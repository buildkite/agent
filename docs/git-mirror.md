# How Git Mirrors Works

## The goal

The goal of Git mirrors is to:

- reduce network bandwidth, and
- reduce disk usage

for machines running multiple agents side-by-side.[^1]

[^1]: We also support agents across multiple machines sharing a Git mirror via a
network share.

How do we do this? By:

- Mirroring the repository in a central location (the git mirror directory), and
- Making each checkout use a `--reference` clone of the mirror.

## Mirrors? `--mirror`? 🪞

You're probably familiar with `git clone`: this gives you a copy of a repo you
can use to make changes, with all the git magic hidden in a secret `.git`
directory.

You might also be familiar with `git clone --bare`: this exposes all the
normally-hidden Git magic, at the expense of not checking out the main branch
for you in the root of the directory.

So instead of:

```shell
$ git clone git@github.com:buildkite/agent.git
Cloning into agent...
...
$ ls agent
CHANGELOG.md            docs                    logger
CODE_OF_CONDUCT.md      env                     main.go
...
$ ls agent/.git
HEAD		config		description	hooks		info		objects		packed-refs	refs
```

with `--bare` you get this:

```shell
$ git clone --bare git@github.com:buildkite/agent.git
Cloning into bare repository 'agent.git'...
...
$ ls agent.git
HEAD		config		description	hooks		info		objects		packed-refs	refs
```

`git clone --mirror` is just like `git clone --bare`, except it also clones
all the other stuff you don't normally get with `git clone` or
`git clone --bare`.

For our purposes we are interested in `--mirror` because it gives us _all_ the
branches, which is important because the agent doesn't know in advance which
branch it needs to build.

`git clone --mirror` does what it sounds like: it makes a clone of the remote
repo that is effectively a complete _mirror_ of everything in the remote.

## `--reference`?

The other unusual one is `git clone --reference`. This is the part that saves
network and disk space. Unfortunately it is also a bit dangerous.

When you `git clone`, normally Git fetches the remote repository and copies the
necessary objects locally. `git clone --reference <some path>` changes that part
by using a secret file (`objects/info/alternates`). With `alternates`, git can
reach into the object store of another local repo, and just use those objects
instead of copying them from the remote repo.

## How this can all go horribly wrong

### Parallelism chaos

With multiple agents trying to `git fetch` in the same mirror repo, there is the
question: are these operations safe to run in parallel? The answer might not
surprise you! (The answer is no. No, they are probably not safe).

This is the reason we have a locking system in the mirror to prevent multiple
agents updating the mirror at the same time.

Because the mirror directory can be a network file share, we also need to
ensure the lock works across multiple machines. We do that with file-based
locks.

### Checkout corruption

If you read
[the Git documentation for `git clone --reference`](https://git-scm.com/docs/git-clone),
it has this to say:

> NOTE: see the NOTE for the --shared option, and also the --dissociate option.

OK, let's look at `--shared` then:

> NOTE: this is a possibly dangerous operation; do not use it unless
> you understand what it does. If you clone your repository using
> this option and then delete branches (or use any other Git command
> that makes any existing commit unreferenced) in the source
> repository, some objects may become unreferenced (or dangling).
> These objects may be removed by normal Git operations (such as git
> commit) which automatically call git maintenance run --auto. (See
> git-maintenance(1).) If these objects are removed and were
> referenced by the cloned repository, then the cloned repository
> will become corrupt.

Let's break that down. We have:

- A repo acting as a mirror of the remote. Let's call it `Mirror`.
- A checkout directory which is a `--reference` clone of `Mirror`. Let's call
  it `Checkout`.

To a casual observer, `Mirror` and `Checkout` are both clones of the remote
repo, but they are both unusual clones (`--mirror` and `--reference`). Because
`Checkout` is a reference clone, it has very few objects of its own: they all
mostly belong to `Mirror`. So far so good.

What happens when we fetch new objects into Mirror, with `git fetch`?
You might guess that `git fetch` only ever adds new objects, but unfortunately,
`git fetch` by default runs `git maintenance run --auto`, which sometimes
cleans up old unreferenced objects.

Unreferenced by _what_? By `Mirror` _only_. `Mirror` doesn't know that its
contents are being used by another local repo, so `git maintenance run --auto`
will happily remove objects still in use by `Checkout`!

Which corrupts `Checkout`. It's as though a gremlin went into `Checkout`'s
object storage and randomly started unlinking files.

Even if those objects bear no relation to the commit the job is trying to build.
Git can still try to clean up the reference, see it is missing, and then fatal
out.

This happens more often than it used to, since GitHub got more
aggressive about removing older commits when you force-push.

## How do we recover corrupted checkouts?

By deleting the whole checkout directory, and then re-cloning it. It's blunt but
it works, and if you have git mirrors enabled, it's relatively painless.

## Aren't we supposed to update mirrors with `git remote update`?

That's the usual way. `git remote update` updates mirrors, and it updates
_everything_ in a mirror. But for CI/CD purposes we only need the mirror to have
objects needed for a particular job. Eagerly updating everything with
`git remote update` is too much work. So in PR
[#1112](https://github.com/buildkite/agent/pull/1112) we switched from
`git remote update` to `git fetch origin <branch>`.

`git remote update` also almost certainly runs auto maintenance, clearing out
dangling objects.

## What if we disable auto maintenance when updating the mirror?

We could do that (`git fetch --no-auto-maintenance`). Do we want to? This will
probably need investigating further.

If the mirror only ever grows, then the mirror will become slower and slower as
Git must process ever more objects, and potentially it could fill the disk.

But it might be preferable to repeatedly deleting and recreating checkout
directories.

## Did I see something about `--dissociate`?

`git clone --reference <path> --dissociate` is like `--reference`, but makes
copies of the objects during the clone. This will probably spare us repo
corruption, and continue reducing network usage, but consume hard disk usage
for each clone.

## Worktrees are cool, can we do something with those?

Maybe! Worktrees have been around since at least Git 2.7.0.

In a `--bare` or `--mirror` clone, Git doesn't provide a working copy of the
files in the repo. But you can still get them, make commits, etc, if you need
to. The most convenient way to do that is using a _worktree_.

So what we could do is, instead of `git clone --reference <mirror>`, we could
instead run jobs in a directory inside the mirror. Something like:

```shell
cd <mirror>
git worktree add build-12345
cd build-12345
git checkout <branch>
# Run the job
cd ..
git worktree remove --force build-12345
```

By using worktrees within a single repo, we can run as many maintenance
operations as we like, because we're not in a repo that secretly depends on
objects that might be removed.

One large downside to this approach is that it needs more work to support agents
across different machines sharing the mirror via a network share.

## Why so much code for handling submodules?

Git submodules are a way for one repository to refer to another repository, so
that the contents of that second repository can be used from code in the first.

If we have mirrors enabled, what happens to submodules? Well, submodules
_should be mirrored_. Fortunately, submodule mirrors can be created and updated
just like regular mirrors, because at the end of the day, submodules are
just _other repos_.

But then the main repo---the one that we check out in order to do a job---how
exactly does that learn how to use the submodule mirrors instead of cloning them
directly?

The agent has to update the submodule configuration in the main repo, using
`git submodule update --reference <submodule_mirror>`. It has to do that for
each submodule (there could be many), which means parsing the submodule config,
and then looping over them to update them.

This is the main reason submodule mirrors need special handling in the agent.
