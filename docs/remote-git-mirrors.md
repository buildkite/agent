# Remote Git mirrors

Status: **plan**. Nothing described here is implemented on `main` yet.

A *remote Git mirror* is a second, faster URL for the same repository, supplied
by the Buildkite backend as `BUILDKITE_GIT_REMOTE_MIRROR_URL`. The agent may
fetch objects from it, but the canonical repository (`BUILDKITE_REPO`) remains
the source of truth for what to build and stays the checkout's `origin`.

Read [`git-mirror.md`](git-mirror.md) first. It describes the pre-existing
**on-host** mirror (`--git-mirrors-path`), which is a different thing that
happens to share the word "mirror". This document uses:

| Term | Meaning |
| --- | --- |
| **canonical** | `BUILDKITE_REPO` — the pipeline's repository, e.g. GitHub |
| **remote mirror** | `BUILDKITE_GIT_REMOTE_MIRROR_URL` — a Git URL serving the same repository, hosted somewhere faster |
| **on-host mirror** | `--git-mirrors-path/<dir>` — the local bare clone shared between jobs on a host |
| **checkout** | the working directory the job runs in |

§§1–5 and §10 are durable: they describe the feature and the decisions behind
it. §§6–9 and §11 are delivery-time content — once the stack has landed, fold
anything still true into the durable sections and delete the rest rather than
leaving a plan to rot next to the code it produced.

## 1. Why

A pipeline whose repository lives on a slow or unreliable host adds a fast
cloud-hosted mirror URL to the pipeline settings. Every job then pulls the bulk
of its object data from the mirror instead of the canonical host.

The mirror replicates asynchronously, so it can lag. When it does not yet have
the commit the build wants, the agent falls back to canonical. **That fallback
is the only case where enabling a mirror may be slower than not enabling one,
and it must cost at most one bounded round trip.**

Both hosts serve the same content-addressed repository, and the customer
controls both. The mirror is an optimisation, never a policy or security
mechanism.

## 2. Requirements

Numbered so the delivery plan and tests can refer to them.

**R1 — Opt-in and backend-driven.** The mirror URL arrives as job env from the
pipeline's `clone_mirror_url` setting. No agent-side configuration is needed to
benefit from it, and none is needed to avoid it.

**R2 — Never slower on a hit.** On the hit path the mirror must not add work,
including any round trip to canonical the hit made unnecessary. On a miss it may
add at most one bounded, non-retrying round trip, after which the canonical path
proceeds with no discarded work — no re-clone, no wiped checkout directory.

**R3 — Checkout shape is preserved.** Whatever `checkout:` attributes the
pipeline sets (`depth`, clone/fetch flags, sparse paths, submodules, LFS), the
mirror-served path must produce the same checkout shape as the canonical path,
and must not silently upgrade a shallow or blobless transfer into a full one.

**R4 — Tiered caching with the on-host mirror.** When `--git-mirrors-path` is
configured, the on-host mirror is populated and refreshed from the remote mirror
rather than from canonical. Buildkite hosted agents run this way (the mirror
lives on a cache volume populated by earlier jobs), so this is the
single highest-value increment.

**R5 — Existing checkouts are refreshed, not rebuilt.** A long-lived agent that
already has a checkout of the repository must fetch the new delta from the
mirror into that checkout. It must never discard a usable checkout in order to
use the mirror.

**R6 — Fresh checkouts clone from the mirror.** With no on-host mirror and no
existing checkout, the clone itself should come from the mirror.

**R7 — Credentials.** The mirror authenticates through
`buildkite-agent git-credentials-helper` against the provider-neutral repository
credential API (`POST jobs/:id/repository_access_token`), already on `main`. The
agent makes a best-effort separation: credentials minted for canonical should
not be presented to the mirror, and vice versa. Best-effort means "we don't send
them ourselves and we clear the obvious inherited channels", not "we sandbox
Git".

**R8 — The built commit is never chosen by the mirror.** The agent checks out
the SHA the backend named. Mirror-derived *names* (branch refs, tags) may end up
in the checkout and may be stale, but nothing the agent does resolves a name
through the mirror to decide what to build.

**R9 — Observability.** Every checkout reports what the mirror did:
`hit`, `miss`, `timeout`, `error`, or `skipped` with a reason. Without this we
cannot distinguish a working mirror from one that misses every time and silently
falls back, and several of the degradations in §10 present as nothing else.

## 3. Trust model, and what we are deliberately not doing

The pipeline customer chooses the canonical host, chooses the mirror host,
writes the hooks, and sets the env. There is no adversarial relationship between
those parties. The mirror URL is a non-secret pipeline setting.

Consequences, stated up front so review does not relitigate them:

- **We do not isolate the mirror from the canonical in Git configuration.** A
  customer who injects transport configuration by unusual means — ambient
  `GIT_CONFIG_*`, an `init.templateDir`, agent hooks exporting `GIT_*`,
  `--config http.*` in `BUILDKITE_GIT_CLONE_FLAGS` — will have that
  configuration apply to mirror requests too. We clear the two channels that
  cost one flag each (§5.4) and accept the rest.
- **We do not defend against a hostile mirror.** A mirror can only serve objects
  the agent asks for by SHA, or names that the agent never trusts (R8). Beyond
  that, a customer pointing their pipeline at a hostile mirror has already lost.
- **We do not make the checkout byte-identical between hit and miss.** Refs and
  tags a `git clone` copies from the mirror are a snapshot of the mirror, not of
  canonical. §10 records exactly where this shows.
- **This can never be a policy mechanism.** A `checkout` hook or plugin replaces
  the whole checkout phase; `BUILDKITE_REPO` is mutable from within a job.

"Non-secret" is about redaction policy, not about logging hygiene: the mirror
URL does not belong in `--redacted-vars`, but every mirror URL the agent logs or
puts on a span still goes through `redact.URLCredentials`, exactly as
`BUILDKITE_REPO` does today. It costs one call and it is the local convention.

Where we accept something, the acceptance is written as a code comment at the
point a reader would otherwise file a bug. §10 lists each one and where it goes.

## 4. Ground truth

### 4.1 What the checkout does today

`internal/job/checkout.go` → `defaultCheckoutPhase`:

1. `ssh-keyscan` the canonical host; materialise `BUILDKITE_GIT_SSH_KEY`.
2. If `--git-mirrors-path` is set, `getOrUpdateMirrorDir` creates or updates the
   on-host mirror **from canonical**, and exports `BUILDKITE_REPO_MIRROR`.
3. If `.git` exists, reconcile `remote.origin.url`; otherwise
   `git clone <flags> -- <canonical> .`, adding `--reference <on-host mirror>`
   (and `--dissociate` in `dissociate` mode) when there is one.
4. `git clean`, optional `git lfs install --local`.
5. `fetchSource` → `git fetch <flags> -- origin <refspec>`, where the refspec is
   the custom refspec, or `refs/pull/N/{head,merge}`, or the branch (when
   `BUILDKITE_COMMIT=HEAD`), or — the common case — the commit SHA.
6. Commit verification, sparse setup, `git checkout`, submodules, LFS, clean.

Four things are easy to get wrong, and the plan depends on all of them:

- **The checkout never fetches *from* the on-host mirror.** Clone and fetch both
  address canonical. The mirror is only an object store, reached through
  `.git/objects/info/alternates`, plus a negotiation aid.
- **The on-host mirror is refreshed by ref name, not by SHA**
  (`updateGitMirror` fetches the branch / PR ref / custom refspec). It
  short-circuits on `hasGitCommit(mirrorDir, commit)`, so an exact-SHA *presence
  check* exists, but there is no exact-SHA *fetch*.
- **`git fetch origin <sha>` contacts the remote even when the object is already
  local.** So "the mirror had it" does not by itself save the canonical round
  trip; see §5.7.
- **The whole checkout runs under a 6-attempt retrier**, and `gitError.Type`
  decides whether a failure wipes the checkout directory before retrying.
  `gitClone` classifies *every* clone failure as `gitErrorClone`, which is in
  that wipe arm.

### 4.2 Measured Git behaviour

Everything below was measured on git 2.43.0 while writing this document, and
independently reproduced during review. These are the facts the plan rests on.

| # | Behaviour | Result |
| --- | --- | --- |
| G1 | `git fetch <url> <full-sha>` for a SHA reachable from an advertised ref, protocol v2, default server config | succeeds — `uploadpack.allowAnySHA1InWant` is **not** required. Confirmed over local path, `file://`, `http://` and `git://` |
| G2 | Same under protocol v0 | fails with `Server does not allow request for unadvertised object`; **exit 128 over HTTP**, exit 1 over local/`file://`/`git://` |
| G3 | `git fetch <url> <full-sha>` for a SHA the server does not have, protocol v2 | `fatal: remote error: upload-pack: not our ref <sha>`, exit 128 |
| G4 | `git clone --reference <repo>` where the objects are reachable only from a non-standard ref namespace in the reference repo | transfers **0** objects — negotiation enumerates every ref of the alternate, not just `refs/heads/*` |
| G5 | `git fetch <url> "+<sha>:refs/some/ns/name"` into a bare repo | succeeds, writes the ref, pins the objects |
| G6 | `git clone <mirror>` → `git remote set-url origin <canonical>` → `git fetch origin <sha>` → `git checkout <sha>` | works for full, `--depth=2`, `--filter=blob:none`, `--single-branch` and `--no-tags` clones, with `remote.origin.fetch`, `remote.origin.tagOpt`, `.git/shallow` and promisor keys all as a canonical clone leaves them |
| G7 | Partial clone from a mirror, then repoint `origin` at canonical, then delete the mirror entirely | lazy blob fetches succeed — `remote.origin.promisor` is keyed on the *remote name*, so repointing the URL retargets every future lazy fetch |
| G8 | `git -c http.extraHeader= …` with an `http.extraHeader` in `.git/config` or in `GIT_CONFIG_*` | header is **not** sent (verified against a recording HTTP server); an empty value resets the header list. A URL-scoped `http.<url>.extraHeader` is **not** reset |
| G9 | `git -c http.extraHeader= clone --config "http.extraHeader=…"` | header is not sent to the remote, but is still persisted into the new repository's config |
| G10 | `git clone --filter=…` against a server without `uploadpack.allowFilter` | **warns and transfers everything**, exit 0, and still writes `remote.origin.promisor` and `partialclonefilter` locally. `--depth` against a server that cannot serve it fails loudly; `--filter` does not |
| G11 | `fetch.uriProtocols` (packfile-uri / CDN offload) | defaults to empty; the client must opt in. Upstream's server-side implementation only offloads blobs (`uploadpack.blobPackfileUri`) |

G6 and G7 are what let this design stay small: `git clone` from the mirror
followed by repointing `origin` reproduces canonical clone semantics exactly,
because it *is* `git clone`, and the promisor follows for free.

G3 explains why the mirror needs its own error classification: `gitFetch`
currently smelts `fatal: bad object` and `fatal: [Cc]ouldn't find remote ref`,
and an exact-SHA miss matches neither, so it falls through to the generic
exit-128 branch and becomes `gitErrorFetchRetryClean`. Every mirror path in this
plan swallows its own error, so no mirror miss ever reaches the retrier — the
classification is needed for R9's `miss`-versus-`error` distinction, not to
prevent a wipe.

G10 is the plan's nastiest failure mode, because it is silent. See §10-C3.

## 5. Design decisions

### 5.1 One resolved decision per checkout, made before the clone

The mirror is consulted at most once per checkout. Rather than a "first eligible
caller claims it" flag, `defaultCheckoutPhase` resolves the decision **once**,
early, where the two inputs that pick the site are both already in scope: the
on-host `mirrorDir` and `osutil.FileExists(filepath.Join(wd, ".git"))`.

A mutable claim flag was the first design and it is wrong twice over. The three
sites do not run in the order the rule wants — the on-host mirror update is at
the top of `defaultCheckoutPhase`, the clone is in the middle, and `fetchSource`
is at the bottom — so "first caller wins" gives the fresh-clone site priority
over the existing-checkout site. Worse, after a mirror clone `.git` exists, so
the existing-checkout site's own condition becomes true and it would attempt a
second mirror round trip in the same checkout, violating R2. Resolving up front
removes both problems and means landing the fresh-clone work does not silently
change the existing-checkout work's behaviour.

The resolution is a small value, not a boolean:

```
type remoteMirrorPlan struct {
    site       // none | onHostMirror | existingCheckout | freshClone
    url        // the mirror URL, when there is a site
    skipReason // why there is no site, for R9
}
```

R9 forces this shape by itself: a `bool` cannot carry a skip reason, and the
most interesting reason ("a mirror is configured but this job was never
eligible") arises at a point no site's code ever runs. So the outcome is written
unconditionally onto the existing `repo-checkout` span, next to the
`checkout.is_using_git_mirrors` attribute already there, from one place.

The plan value is resolved per checkout *attempt*, not per job. Do not hang it
off `Executor`, which is job-lifetime state; today's "first attempt only" clause
would mask the difference and leave a trap for whoever relaxes it.

### 5.2 Eligibility

```
mirror URL is non-empty and https
&& the mirror URL is permitted by --allowed-repositories, when that is set
&& canonical repository still matches the one the mirror was issued for
&& BUILDKITE_COMMIT is a full 40-hex lowercase SHA
&& BUILDKITE_REFSPEC == "" && BUILDKITE_TAG == "" && BUILDKITE_PULL_REQUEST == "false"
&& this is the first checkout attempt
```

Only the SHA clause is a correctness requirement. Everything the mirror is asked
for is that SHA, and every eligible build ends at `git checkout <commit>`, so
R8 holds on the SHA clause alone — a tag or PR build with a known SHA would be
just as safe. The tag, PR and refspec clauses are pragmatism: they line up
exactly with the conditions `verifyCommit` already skips on, and they avoid
depending on answers we do not have yet (§11 has to ask whether the mirror even
serves `refs/pull/*`). Relax them when those answers arrive.

`https` rather than `http(s)`: the credential helper rejects any other protocol
(`errNotHTTPS` in `clicommand/git_credentials_helper.go`), so an `http` mirror
could only ever work unauthenticated. One scheme, no half-supported case.

**The allowlist clause makes the job mirror-ineligible; it does not refuse the
job.** This is the one place the first draft of this plan had a fleet-breaking
bug, so it is worth being explicit. `validateConfigAllowlists` in
`agent/run_job.go` is not advisory: a miss returns `SignalReasonAgentRefused`
and the agent refuses the job. Operators anchor `--allowed-repositories` at
their canonical host, and a mirror is by definition on a different one — so
checking the mirror URL there would mean that the moment anyone in the
organisation sets `clone_mirror_url`, every job on that fleet stops running,
triggered by a backend setting with no agent-side rollback. Declining the
optimisation gives the operator exactly what the flag promises (the agent never
contacts a host they did not allow) and costs the customer nothing. It also
makes `--allowed-repositories` a precise, already-existing operator-side off
switch, which is why this plan proposes no new agent flag for that job.

Mechanically, the allowlist lives in the job runner and the rest of eligibility
lives in the executor, two different processes. Rather than plumbing a verdict
across, the job runner drops `BUILDKITE_GIT_REMOTE_MIRROR_URL` from the job
environment in `createEnvironment` when the allowlist does not permit it, and
warns. That is the same shape as the `delete(env, "BUILDKITE_AGENT_TOKEN")`
already there, and the executor then sees a job with no mirror and needs to know
nothing about allowlists. The warning is where an operator learns why, since to
the span this is indistinguishable from "no mirror configured".

**Canonical binding** is one field and one clause, not a subsystem. Record the
backend-supplied `BUILDKITE_REPO` when the executor is constructed and require
`e.Repository` to still equal it. Take the snapshot at construction:
`ReadFromEnvironment` refreshes `e.Repository` on every `applyEnvironmentChanges`,
so anything later is already the rewritten value. Give the field a name that says
what it is, and one sentence next to `ExecutorConfig.Repository` explaining why
there are two repository values with opposite refresh semantics.

What this buys is narrow and worth saying so: object content cannot differ,
because every request is by SHA, so R8 survives a hook rewriting
`BUILDKITE_REPO` with no binding at all. The real case is the fresh clone, where
cloning repository A's mirror into a job now building repository B would copy
A's refs and tags into the checkout.

### 5.3 Budget

Mirror commands run with retries disabled, under
`context.WithTimeout(ctx, 30*time.Second)`.

That is the whole rule. `WithTimeout` never extends past a parent deadline, so
when `--git-checkout-timeout` is set the attempt is already capped by whatever
remains — no bespoke "reserve a fraction for canonical" arithmetic, which the
first draft specified and which is not a number an implementer can act on
anyway. The first-attempt-only clause bounds the worst case to one attempt out
of six.

Retries stay with canonical. `gitFetch` with `Retry: true` is 10 attempts over
~2m17s and the checkout sits inside a 6-attempt retrier, so a retrying mirror
attempt would multiply into minutes per job for no benefit: a mirror that does
not have the commit now will not have it 200ms later.

### 5.4 Credentials and transport flags (R7)

Every mirror-addressed Git command gets these, per invocation, never persisted:

```
-c credential.useHttpPath=true
-c credential.helper=              # reset any inherited helper list
-c credential.helper=<the same value configureGitCredentialHelper installs>
-c http.extraHeader=               # reset any inherited extra headers
-c protocol.version=2              # exact-SHA fetches need v2 (G1/G2)
```

- Resetting `credential.helper` first means exactly one helper — the agent's —
  is consulted for the mirror, so a customer's own helper for the canonical host
  (a cloud credential helper, an OS keychain entry, a `store` file) is never
  asked for mirror credentials. Verified: with the reset only the agent's helper
  is invoked, whether the customer's is configured repo-locally, through
  `GIT_CONFIG_*`, or URL-scoped as `credential.<url>.helper`.
- Re-adding the helper is not redundant with the global one
  `configureGitCredentialHelper` installs, because that is installed only when
  the job has managed credentials for *canonical*. The backend deliberately does
  not advertise a global helper when only the mirror needs credentials — a
  global helper would also intercept the canonical checkout — so a
  per-invocation helper is the only mechanism available. Build the value from
  one shared function over `self.Path(ctx)`, used by both call sites; two
  independent `fmt.Sprintf`s of the same helper string will drift.
- `credential.useHttpPath=true` scopes the helper call to the full mirror URL,
  which is what `repository_access_token` keys on. That the endpoint will answer
  for a URL other than the job's repository is the load-bearing assumption
  behind R7, and it is grounded rather than speculative:
  `Job::CodeAccessTokenIssuer` already resolves "an exact repository URL named
  by the pipeline's primary or clone-mirror" and has a dedicated mint path for
  the clone mirror. Today that path is Cursor Origin's, so confirm it for any
  other mirror provider before pointing a pipeline at one.
- `-c http.extraHeader=` resets the **unscoped** header list (G8), so a bearer
  token a customer set for canonical in `.git/config`, in `GIT_CONFIG_*`, or in
  `--config http.extraHeader=…` is not presented to the mirror, while still
  being persisted into the checkout for canonical's use (G9). A URL-scoped
  `http.<url>.extraHeader` whose prefix also matches the mirror survives; that
  requires the two to share a host, which the topology rules out. §10-C5.
- `protocol.version=2` is Git's own default from 2.26 onward, but a customer or
  a distribution can pin `protocol.version=0`, under which an exact-SHA fetch
  fails for anything that is not a ref tip (G2). Pinning v2 turns "the mirror
  never works and nobody notices" into "the mirror works"; a mirror that cannot
  speak v2 simply misses and canonical takes over.

Everything else — `GIT_CONFIG_*` beyond the two keys above, `init.templateDir`,
`http.proxy`, `GIT_SSL_*`, other `http.*` keys, `GIT_SSH_COMMAND` — is
inherited. §10-C6.

### 5.5 Plumbing prerequisite: the git wrappers must take `[]string`

These flags cannot be expressed today, and the shape of the fix matters because
getting it wrong is what produced the worst code in the earlier attempts.

`gitClone` builds `[]string{"clone"}` first, so nothing can precede the
subcommand. `gitFetchArgs.GitFlags` is a `string` that `gitFetch` splits with
`shellwords.Split`. The credential helper value is itself space-separated
(`<agent path> git-credentials-helper`), so pushing it through that string
re-splits it into separate argv tokens on any agent whose binary path contains a
space — `C:\Program Files\Buildkite\buildkite-agent.exe` being the obvious one.
The existing code is safe only because it hands the helper to `git config` as a
single argv element. The same string round trip already mis-splits a
`--git-mirrors-path` containing a space, today, in the one production caller.

So: change `gitFetchArgs.GitFlags` to `[]string` and give `gitClone` a global
flags parameter. One production caller and its tests to update. What not to do
is quote the argv back into a string so it can be re-split on the other side;
[#4153](https://github.com/buildkite/agent/pull/4153) grew a `quoteGitArgs`
helper for exactly that, and Go already has the type for a list of arguments.

### 5.6 `git clone` from the mirror, not `git init` + fetch

For a fresh checkout, clone the mirror and repoint `origin`:

```
git <transport flags> clone <the same clone flags> -- <mirrorURL> .
git remote set-url origin <canonical>
```

Reconstructing a clone as `git init` + `git remote add` + `git fetch` — the
shape of [#4153](https://github.com/buildkite/agent/pull/4153) — means
hand-reproducing everything `git clone` does with its flags: `--config`
(additive, and overridden for clone-owned keys), `--single-branch` narrowing
`remote.origin.fetch`, `--depth` implying `--single-branch`, `--no-tags` writing
`remote.origin.tagOpt`, and promisor configuration. Each of those was a separate
blocking review round on
[#4144](https://github.com/buildkite/agent/pull/4144). `git clone` makes all of
them correct by construction (G6), and G7 shows the promisor case resolves
itself: `remote.origin.promisor` names the remote, so repointing the URL
retargets every future lazy object fetch at canonical.

### 5.7 A hit must also skip the canonical commit fetch

`git fetch origin <sha>` contacts the remote even when the object is already
local. So a mirror hit that still runs the canonical fetch has bought objects
but not the round trip to the slow host, and R2 ("never slower on a hit", which
means no work the hit made unnecessary) is only half met.

On a confirmed hit — the exact commit is present locally *and* the mirror
command exited zero — the existing-checkout and fresh-clone sites skip
`fetchSource`'s commit fetch. This is the same thing
`BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS` does, scoped to a mirror hit instead
of switched on globally. When that option is already enabled the two agree and
nothing changes.

Two consequences, both already caveats: `refs/remotes/origin/*` keep the
mirror's snapshot rather than being refreshed (§10-C1), and `FETCH_HEAD` is not
written by the skipped fetch (§10-C4, and no eligible flow reads it).

"Confirmed" has to mean both halves. A `hasGitCommit` presence check alone is
not enough — §10-C2 has a measured case where the objects arrive but the ref
write fails, leaving them unreferenced and eligible for `gc`.

### 5.8 Namespaced, sanitised refs in the on-host mirror

When the remote mirror fetch targets the on-host mirror, it writes
`+<commit>:refs/buildkite-agent/remote-mirror/<sanitised branch>`, where the
branch is flattened with the same `badCharsRE` substitution `dirForRepository`
uses (every non-alphanumeric becomes `-`). Skip the mirror when the branch is
empty.

Each part of that earns its place:

- **A ref rather than `FETCH_HEAD`**, because `FETCH_HEAD` is not a ref: the
  objects would be unreferenced and therefore `gc` fodder, which is the
  corruption mode `git-mirror.md` documents. G4 also shows the later
  `--reference` clone's negotiation reads the alternate's refs, so an
  unreferenced object store would not even help.
- **A namespace rather than `refs/heads/<branch>`**, because writing the build
  commit to the branch ref would be wrong for a rebuild of an older commit (it
  moves the branch backwards) and would unreference objects that concurrent
  `--reference` checkouts depend on.
- **A sanitised single path component rather than the raw branch name**, because
  a raw name reintroduces directory/file conflicts: with
  `…/heads/<branch>`, a job on `feature` permanently blocks every later job on
  `feature/x`, whose ref write then fails while the objects still land. It also
  avoids needing a ref-name validator — the repository's `gitCheckRefFormat`
  explicitly does not implement the rules that matter here (its own doc comment
  lists rules 1, 2, 5 and 6 as unimplemented), and it passes `''`, `foo/.bar`,
  `x.lock`, `a//b` and `.hidden`, all of which git rejects. Collapsing to
  alphanumerics sidesteps the whole category. Two branches can collide onto one
  ref, which costs churn and never correctness.

## 6. Delivery plan

Four stacked pull requests.

| PR | Idea | Behaviour change | Touches |
| --- | --- | --- | --- |
| 1 | Config, the resolved decision, and R9 telemetry | telemetry only | `config.go`, `env/protected.go`, `job_runner.go`, `git.go`, new decision file |
| 2 | On-host mirror is created and refreshed from the remote mirror | yes | `checkout_mirror.go` |
| 3 | Existing checkout fetches from the remote mirror | yes | `checkout_fetch.go` |
| 4 | Fresh checkout clones from the remote mirror | yes | new `checkout_clone.go` |

PRs 2, 3 and 4 are mutually exclusive per checkout (§5.1) and can land in any
order after PR 1. PR 2 first because it is worth the most.

PRs 2 and 3 are the same operation with different parameters — fetch a commit
from a URL into an existing git directory, retries off, bounded, then ask
whether it landed — differing only in the git dir, the refspec and the fetch
flags. They share one helper, introduced by whichever lands first. If reviewers
would rather have three PRs than four, merging them is the natural cut; they are
separate here because their risk surfaces and test fixtures are different.

There is no PR for CDN pack offload. See §11.

### PR 1 — Config, decision and telemetry

**Goal:** settle the contract, and make the feature observable before it does
anything.

- `ExecutorConfig.GitRemoteMirrorURL`, `--git-remote-mirror-url` on `bootstrap`,
  `BUILDKITE_GIT_REMOTE_MIRROR_URL` in `protectedEnv` with no `env:` struct tag
  so hooks cannot refresh it (`clicommand/config_completeness_test.go` enforces
  most of the plumbing). This value is a new third category for `protectedEnv` —
  backend-supplied *and* agent-immutable, where the map's comment today
  describes only "agent configuration, or in some cases, from within the job" —
  so extend that comment rather than leaving the next reader to infer it.
- The bound canonical repository field, and the `createEnvironment` drop when
  `--allowed-repositories` does not permit the mirror URL (§5.2). Both are in
  this PR because both change what the executor sees, not what it does.
- `remoteMirrorPlan` and its resolution (§5.1, §5.2), called from
  `defaultCheckoutPhase`. No site acts on it yet.
- Unconditional R9 emission onto the `repo-checkout` span, including
  `skipped` with a reason. This is what stops PR 1 being a shape with no caller,
  and it means the first behaviour-changing PR ships into a checkout that can
  already report what happened.
- `gitErrorFetchRefNotOnRemote`, smelting `not our ref` (protocol v2, G3) and
  `Server does not allow request for unadvertised object` (protocol v0, G2).
  Note these are two protocol versions, not two spellings, and test both.

Three call sites branch on `gitError.Type`, and "canonical behaviour is
unchanged" has to hold at each: `gitFetch`'s own retrier must `retrier.Break()`
for the new type (omitting it silently converts an immediate failure into 10
attempts over ~2m17s on the `Retry: true` paths), `gitFetchWithFallback` must
keep returning rather than falling back, and the checkout retrier keeps the new
type in the same wipe arm as `gitErrorFetchBadObject`. Assert attempt counts,
not just the resulting error type.

**Tests:** eligibility table including each skip reason; env protection;
an allowlist miss drops the mirror URL and warns rather than refusing the job;
error classification for
all three fetch failure strings on both protocol versions; span attributes for
`skipped` and for "no mirror configured"; `config_completeness_test`.

### PR 2 — Create and refresh the on-host mirror from the remote mirror (R4)

**Goal:** tiered caching. Buildkite hosted agents keep an on-host mirror on an
attached cache volume, so this alone removes most canonical traffic without
touching the checkout at all.

`updateGitMirror` has two arms and both matter. The first job on a fresh cache
volume is the largest single transfer in the system, and it takes the creation
arm, so covering only the update arm would miss it entirely.

**Creation** (`mirrorDir` does not exist): clone the mirror rather than
canonical, then hand the result to canonical immediately —

```
git <transport flags> clone --mirror <GitCloneMirrorFlags> -- <mirrorURL> <mirrorDir>
git --git-dir=<mirrorDir> remote set-url origin <canonical>
```

Same trick as §5.6, one layer down. Setting the URL at creation time means the
next job's `updateRemoteURL` sees no change, so the `fsck` + `gc` churn a
rotating URL would cause never fires. On failure, remove the mirror directory —
the existing "Removing mirror dir due to failed clone" path — and clone from
canonical as today. There is no canonical-sourced ref to move backwards here, so
the mirror's own refs come across as-is and are reconciled by later canonical
fetches. If the freshly created mirror turns out not to have the commit, no
special handling: the checkout's canonical clone with `--reference` transfers
only the delta, which is the tiered fallback working as designed.

**Update** (`mirrorDir` exists): immediately after the existing
`hasGitCommit` short-circuit and **before** `updateRemoteURL` —

```
git --git-dir=<mirrorDir> <transport flags> fetch --no-tags -- <mirrorURL> \
    "+<commit>:refs/buildkite-agent/remote-mirror/<sanitised branch>"
```

A SHA source with an explicit destination is legal and writes the ref (G5), so
one command both transfers the objects and pins them. Then, only if that exited
zero **and** `hasGitCommit` now passes, take the same
`return e.snapshotMirror(...)` exit the short-circuit above takes. Otherwise
fall through to the existing canonical fetch, unchanged.

Placing it before `updateRemoteURL` rather than after is not cosmetic. After
`updateRemoteURL` has run, `urlChanged` may be set, and its only consumer is the
`fsck` + `gc` block *after* the fetch — so an early return there would silently
skip the maintenance a repository rename needs. Before it, the new exit is
structurally identical to the one it is modelled on. Factor the shared
"present → snapshot and return" tail into one closure so the function has one
exit rather than three.

No user fetch flags are passed, matching the existing mirror-update fetch: the
on-host mirror is always a full mirror regardless of the pipeline's
`checkout.depth`, and the checkout gets its shallowness from its own clone.

**Measured, end to end.** On-host mirror lagging behind canonical, then
`git clone --reference <on-host mirror> -- <canonical> .`:

| On-host mirror state | Objects canonical had to send |
| --- | --- |
| lagging (today's behaviour) | 6–7 |
| warmed by an exact-SHA fetch from the remote mirror | **0** |

with `refs/heads/main` in the on-host mirror provably unmoved, the resulting
checkout fsck-clean at the target SHA, and 0 objects also under `--dissociate`,
`--depth=1` and `--filter=blob:none`.

**Scope:** main repository only, never submodule mirrors (the existing
`isMainRepository` guard). Honour `--git-mirrors-skip-update` — it means
"contact no upstream", and `getOrUpdateMirrorDir` returns before
`updateGitMirror`, so this is automatic. Never
`git remote set-url origin <mirrorURL>` on an existing mirror.

**Tests:** creation from the mirror with canonical unreachable, and the
resulting `remote.origin.url`; creation falls back when the mirror is down;
update hit skips the canonical fetch and canonical serves zero objects to the
later `--reference` clone; miss falls through with `refs/heads/*` correct;
fetch-succeeded-but-ref-write-failed does **not** count as a hit; branch names
that are not valid ref components; timeout; mirror refuses auth;
`--git-mirrors-skip-update`; submodule mirrors untouched; `refs/heads/*` never
written by the mirror path.

### PR 3 — Refresh an existing checkout from the remote mirror (R5)

**Goal:** the long-lived agent that already has the repository and needs today's
delta. Runs only when the on-host mirror site did not.

In `fetchSource`, for the `refspecCommit` case: fetch the commit from the mirror
first, retries off, transport flags of §5.4. On a confirmed hit skip the
canonical fetch (§5.7); otherwise fetch from canonical as today. A miss leaves
the checkout exactly as it was, so R2 holds by construction.

**Use the flag list the canonical fetch would have used, not `e.GitFetchFlags`.**
`fetchSource` receives `addBloblessFilter` and prepends `--filter=blob:none`
itself, so for a sparse pipeline that did not supply its own filter — precisely
the configuration the agent auto-adds the filter for — reading the raw config
field would send an *unfiltered* mirror fetch and, on a hit, leave the checkout
with every blob present. That is R3 violated, not merely slower.

**Then clean up the promisor config the fetch may have written.** A filtered
fetch from a URL makes git record that URL as a promisor remote:

```
remote.<mirrorURL>.promisor true
remote.<mirrorURL>.partialclonefilter blob:none
```

Left behind, a reused checkout consults the mirror URL for every future lazy
object fetch, with none of §5.4's per-invocation credentials or bounding — the
same objection raised against
[#4144](https://github.com/buildkite/agent/pull/4144), reintroduced by a
different route. So unset both keys after the fetch, and repoint
`extensions.partialClone` at `origin` if git set it to the mirror URL. Three
config commands, deterministic, testable. It degrades safely if missed (git
falls back to another promisor), which is exactly why it needs a test rather
than trust.

**Tests:** existing checkout advances via a mirror hit with canonical
unreachable; miss falls back and leaves refs and worktree byte-identical; a
sparse pipeline's mirror fetch carries `--filter=blob:none`; no `remote.<url>.*`
or mirror-valued `extensions.partialClone` survives a hit; a partial clone still
resolves lazily against canonical after the mirror is removed.

### PR 4 — Clone a fresh checkout from the remote mirror (R6)

**Goal:** the cold agent with no on-host mirror and no existing checkout, where
all the bytes are.

**Extract before adding.** `defaultCheckoutPhase` is already 342 lines and
already branches on on-host mirror presence, mirror checkout mode,
existing-versus-fresh, sparse (with two nested sub-branches), partial filter,
LFS and submodules. Adding a fifth cross-cutting axis in place will not compose.
The `.git`-exists reconcile-versus-clone block is already a coherent unit, and
the package has an established convention for exactly this (`checkout_fetch.go`,
`checkout_mirror.go`, `checkout_sparse.go`, `checkout_ssh.go`). Move it to
`checkout_clone.go` in a no-behaviour-change commit, then add the mirror arm
there, where the clone, its failure handling and the `set-url` are local and
testable together.

Behaviour is §5.6, then the existing `git clean` / `fetchSource` / `checkout`
flow, with `fetchSource`'s commit fetch skipped on a confirmed hit (§5.7).

**Failure handling, in order of likelihood:**

| Outcome | Response |
| --- | --- |
| Clone succeeds, commit present | done — the common hit |
| Clone succeeds, commit absent (mirror lagging) | keep the clone, let `fetchSource` fetch the delta from canonical. Strictly better than re-cloning: the mirror already supplied nearly everything |
| Clone fails (mirror down, auth, 404, timeout) | discard what the clone left behind, clone from canonical |

Only the third discards work, and only work the mirror attempt created. Two
things about it:

- **Do not hand-roll the cleanup.** `gitClone` clones into the checkout
  directory itself, so "what the clone left behind" is `.git` plus worktree
  files in the current directory, and removing that is what `removeCheckoutDir`
  does — paired with `createCheckoutDir`, because `removeCheckoutDir` closes and
  nils `e.checkoutRoot`. Reuse the pair or leave `e.checkoutRoot` stale.
- **Do not let the failure escape as a `gitError`.** `gitClone` classifies every
  clone failure as `gitErrorClone`, which is in the retrier's wipe-and-retry arm,
  so a mirror-clone failure that propagates makes the retrier wipe the directory
  and re-run the whole attempt instead of falling back. PR 1 adds a fetch-side
  error type for the mirror; the clone side needs the same care and gets it by
  swallowing rather than by a new type.

Shallow, blobless, sparse and single-branch (R3) need no special handling: the
same `BUILDKITE_GIT_CLONE_FLAGS` go to the mirror clone (G6). The exception is a
mirror that cannot serve a filter, which is silent — §10-C3.

**Tests:** hit with canonical unreachable, asserting canonical served zero
objects rather than "was not contacted"; lagging mirror completed from
canonical; mirror down falls back to a canonical clone without the retrier
wiping; `--depth`, `--filter`, `--single-branch`, `--no-tags` and sparse each
produce the same `.git/config`, `remote.origin.fetch`, `remote.origin.tagOpt`
and shallow state as a canonical clone; a mirror whose fixture lacks
`uploadpack.allowFilter`, asserting on missing-object count rather than config
equality; a mirror-cloned partial clone still resolves lazily after the mirror
is removed.

## 7. How this answers the review threads on #4144 and #4153

Every blocking thread on the two earlier pull requests. "Dissolved" means the
code that caused it no longer exists.

| Thread | Disposition |
| --- | --- |
| `--filter` on the mirror fetch registers the one-shot mirror as a promisor remote | **Dissolved for the fresh clone** (§5.6: the promisor is `origin`, whose URL becomes canonical, G7); **handled explicitly for an existing checkout** (PR 3 unsets the mirror-keyed keys). It is a real defect on that path, not a non-issue |
| Reused partial clone: negotiation hides missing blobs; lazy fetch escapes the mirror scope | **Dissolved.** PR 3 fetches into the existing repository whose promisor is canonical, and cleans up as above |
| Local-commit probe triggers a promisor fetch from `origin` before the bounded context | **Dissolved.** No pre-probe; the sites fetch and then re-check |
| `git init` loses `BUILDKITE_GIT_CLONE_FLAGS` semantics (e.g. `--config core.autocrlf=false`) | **Dissolved** (§5.6) |
| Repeated `--config` values are additive under clone but replaced by `git config` | **Dissolved** (§5.6) |
| `--config remote.origin.url=X` survives as a second push destination | **Dissolved** (§5.6): clone owns `remote.origin.*` |
| `branch.*` config that clone overwrites is preserved by the reconstruction | **Dissolved** (§5.6) |
| `--single-branch` / `--depth` narrowing and `--no-tags` → `remote.origin.tagOpt` not reproduced | **Dissolved** (§5.6), verified in G6 |
| `--tags` / `--prune-tags` imports mutable mirror tags | **Accepted** (§10-C1). A clone from the mirror copies the mirror's tags; the built commit is still the backend's SHA (R8) |
| Clone-time `http.extraHeader` reaches the mirror | **Fixed** by one flag (§5.4, G8/G9), with the URL-scoped residue recorded as §10-C5 |
| `GIT_TEMPLATE_DIR` / `init.templateDir` seeds credentials into the mirror request | **Accepted** (§10-C6). Customer-supplied config applying to a customer-chosen mirror |
| Inherited `GIT_DIR` / `GIT_CONFIG` redirect the mirror sequence | **Largely dissolved:** no `git init` and no bespoke `git config` writes, so there is no separate repository layout to redirect. Residual inheritance is §10-C6 |
| `ls-remote` receives the repository before `--`, allowing option injection | **Dissolved.** No `ls-remote`. Every URL in the new paths is passed after `--`, as the existing `gitClone`/`gitFetch` wrappers already do |
| A hit leaves the checkout without `refs/heads/*`, `refs/remotes/origin/*` or tags, so `git rev-parse origin/main` and `git describe` behave differently | **Dissolved for absence** (§5.6 produces a real clone with all the usual refs), **accepted for staleness** (§10-C1) |
| `.git/FETCH_HEAD` retains the mirror URL after a mirror-served fetch | **Accepted** (§10-C4) |
| A hook rewrites `BUILDKITE_REPO` while the mirror stays bound to the original repository | **Fixed** (§5.2), scoped down to what it actually buys |
| Credential-helper install ordering around `setUp` and environment hooks | Already resolved on `main` by [#4152](https://github.com/buildkite/agent/pull/4152) |

## 8. Testing

- **Unit** (`internal/job`): the eligibility table with a case per skip reason,
  error classification on both protocol versions, transport flag construction.
- **HTTP-backed** (`internal/job` with `internal/job/githttptest`): hit, miss,
  timeout, authenticated mirror through the credential helper, mirror down. The
  package already serves several repositories from one test server, so
  hit/miss/stale scenarios are directly expressible.
- **Differential**: for PR 4, clone the same fixture from canonical and through
  the mirror and assert equal `.git/config`, `remote.origin.fetch`,
  `remote.origin.tagOpt` and shallow state across the flag matrix. Assert the
  mirror path actually ran, so a silent fallback cannot make the test pass
  vacuously.
- **Assert on bytes, not on abstinence.** "Canonical was never contacted" is not
  achievable — the checkout still resolves refs against canonical even on a hit
  (§4.1). Assert objects transferred, or missing-object counts for filtered
  cases. This matters most for G10, where config-shape assertions pass while the
  transfer is wrong.
- **Integration** (`internal/job/integration`): the existing suites assert exact
  git argv through `bintest`'s `ExpectAll`, so any change to command shape
  touches many expectations in `checkout_integration_test.go` and
  `checkout_git_mirrors_integration_test.go`. Budget for that in PRs 2 and 4.
- **Negative**: ineligible targets (HEAD, short SHA, tag, PR, custom refspec,
  retry attempt, hook-rewritten repository, disallowed by
  `--allowed-repositories`) never contact the mirror. A mirror URL pointing at a
  closed port is the cheapest way to say so.

## 9. Rollout

Two controls, both pre-existing:

- **Per pipeline and per organisation**, on the backend: `clone_mirror_url` is
  unset by default, and the `PipelineCloneMirror` feature flag gates who can set
  it at all. Nothing happens to any pipeline until someone opts in.
- **Per fleet**, on the agent: `--allowed-repositories`, which now declines the
  mirror rather than the job (§5.2). An operator who has not allowed the mirror
  host gets canonical behaviour and a `skipped` reason in telemetry.

No new agent flag, and no `EXPERIMENTS.md` entry. An experiment is for behaviour
the backend cannot gate, and this one it can.

What to watch, from the R9 attributes: the `hit`/`miss` ratio, the distribution
of `skipped` reasons (a fleet-wide skip reason is a misconfiguration, not a lag
problem), mirror attempt duration — a slow miss is what hurts — and total
checkout duration against a comparable pipeline without a mirror.

Every PR is revertible on its own: each adds one arm behind a decision that
resolves to `none` for every pipeline without a mirror URL.

## 10. Caveat register

Each entry names where the acceptance comment goes, so a future reader hits the
explanation before they file the bug.

**C1 — Refs from a mirror clone are the mirror's snapshot.** After PR 4,
`refs/remotes/origin/<branch>` and tags come from the mirror and may lag
canonical by the replication delay; skipping the canonical commit fetch on a hit
(§5.7) means nothing refreshes them. The built commit is unaffected (R8), and
commit verification is unaffected because `checkCommitOnBranch` fetches
`+refs/heads/<branch>:refs/buildkite-agent/commit-verification-branch-tip` from
`origin`, which is canonical by then. *Optional cheap fix, decide during PR 4:*
when the canonical fetch does run, add
`+refs/heads/<branch>:refs/remotes/origin/<branch>` to it — no extra round trip.
`checkCommitOnBranch` already documents the ref-name hazards such a refspec must
respect. *Comment: beside the `set-url` in `checkout_clone.go`.*

**C2 — Objects can arrive in the on-host mirror without the ref that pins
them.** A ref write can fail while the fetch succeeds — measured, via a
directory/file conflict in the ref namespace, which §5.8's sanitisation is
designed to prevent but cannot rule out for every future namespace change.
`hasGitCommit` is a presence check and reports the objects as present, so
treating presence as a hit would hand the checkout a `--reference` mirror whose
objects are unreferenced and eligible for `gc` — and the default
`--git-mirror-checkout-mode` is `reference`, the mode with no protection. Hence
§5.7's requirement that a hit means exit zero *and* presence. *Comment: at the
`hasGitCommit` re-check in `checkout_mirror.go`.*

**C3 — A mirror that cannot serve `--filter` silently transfers everything.**
`git clone --filter=…` against a server without `uploadpack.allowFilter` warns,
exits 0, transfers the full object set, and still writes the promisor keys
(G10). A sparse pipeline — where the agent adds `--filter=blob:none` itself — is
both the most likely configuration and the one with the most to lose, and R2 is
violated on a *hit*. `--depth` fails loudly in the same situation; `--filter`
does not. The general rule this instance of: **the mirror must support at least
the capabilities canonical does**, and where it does not, the degradation is
silent. Detection after the fact does not recover the job's bytes, so the
mitigation is telemetry plus §11's question to the mirror provider, and a test
fixture that deliberately lacks the capability. *Comment: beside the mirror
clone in `checkout_clone.go`.*

**C4 — `.git/FETCH_HEAD` records the mirror URL** after a mirror-served fetch on
the existing-checkout path. The mirror URL is a non-secret pipeline setting, and
no mirror-eligible flow reads `FETCH_HEAD` — only the `HEAD`-commit and
PR-refspec flows do, and both are ineligible. `--no-write-fetch-head` would avoid
it but postdates the oldest Git the agent supports. *Comment: at the mirror
fetch in `checkout_fetch.go`.*

**C5 — URL-scoped `http.<url>.extraHeader` is not reset** by §5.4's flag, which
clears only the unscoped list (G8). Reaching the mirror requires the scoped URL
prefix to match the mirror URL, i.e. the two hosts to be the same, which the
topology rules out. *Comment: with the transport flags.*

**C6 — Other ambient and user-supplied transport config applies to mirror
requests**: `init.templateDir`, `http.proxy`, `GIT_SSL_*`, `GIT_SSH_COMMAND`,
`http.*` keys other than `extraHeader`. Deliberate, per §3. *Comment: with the
transport flags.*

**C7 — On-host mirror `gc` can still unreference objects a checkout depends
on.** Pre-existing, documented in `git-mirror.md`, mitigated by `dissociate`
mode and snapshots. The namespaced ref is force-updated, so it does not only
grow the pinned set: a build targeting a commit outside the previous target's
ancestry (a rebuild of an older commit, a force-push) shrinks it. That is the
same churn profile as the `refs/heads/<branch>` the canonical fetch already
force-updates, bounded by branch count, and it never moves a canonical-sourced
ref. *Comment: in `checkout_mirror.go`.*

**C8 — On a mirror-hit steady state the on-host mirror's `refs/heads/*` stop
advancing,** because the canonical fetch is skipped. The namespaced refs keep
the built history reachable, and negotiation stays effective for the branches
being built; other branches' refs age. `git-mirror.md` names disk usage as one
of the mirror's two goals, and this shifts what the mirror retains rather than
growing it without bound. *Comment: in `checkout_mirror.go`.*

**C9 — Submodules never use the remote mirror.** The backend knows only the main
repository; `.gitmodules` URLs are discovered at checkout time. Submodule
on-host mirrors continue to refresh from their canonical URLs.

**C10 — Git LFS is unaffected**, which is the good outcome: `git lfs` resolves
its endpoint from `remote.origin.url`, which is always canonical. An LFS-heavy
repository gets no mirror benefit for its LFS objects.

**C11 — Ineligible builds get no mirror at all**: `BUILDKITE_COMMIT=HEAD`, short
SHAs, tag builds, PR builds, custom refspecs, `http` mirror URLs, and every
checkout attempt after the first. First-attempt-only is cost control, not
correctness — it keeps a failing checkout from re-paying the mirror cost on each
of six retries.

**C12 — Mirror lag is visible as a slower job, not a failure.** Expected and
bounded, and the reason R9 exists: a mirror missing 100% of the time looks
exactly like no mirror, only slightly slower. A corporate proxy that strips the
`Git-Protocol` header would present identically, via G2.

**C13 — Checkout hooks and plugins bypass all of this,** as they bypass the
whole default checkout.

**C14 — Split checkout/command phases** (agent-stack-k8s) need
`BUILDKITE_GIT_REMOTE_MIRROR_URL` to reach the checkout container; the existing
`snapshotMirror` caveat about split phases is unchanged.

**C15 — A job killed between PR 4's clone and its `set-url` leaves a checkout
whose `origin` is the mirror.** The next job's existing-checkout path already
reconciles `remote.origin.url` with `BUILDKITE_REPO`, so it self-heals at the
cost of one confusing "the repository has been renamed" log line. Not worth a
lock or a marker file. *Comment: beside the `set-url` in `checkout_clone.go`.*

## 11. Open questions

For the backend and the mirror provider. The first two would each remove
machinery from this plan.

- **Staleness contract.** Can the mirror URL be advertised only once the backend
  knows the commit is present? That turns a miss from "expected, must be cheap"
  into "a bug worth alerting on", and lets most of the fallback machinery relax.
- **Capability parity.** Does the mirror advertise `uploadpack.allowFilter`, and
  does it allow wants for *unreachable* objects (`uploadpack.allowAnySHA1InWant`,
  needed to rebuild a commit that has been force-pushed away — G1 covers only
  the reachable case)? C3 exists because the answer is currently unknown and the
  failure is silent.
- Can the mirror URL rotate between jobs of the same pipeline? PR 2 keys the
  on-host mirror directory on the canonical URL specifically so rotation does
  not fragment the cache, but it is worth confirming.
- Does the mirror serve `refs/pull/*`? That is the precondition for extending
  eligibility to PR builds.
- Does it proxy Git LFS?
- **CDN pack offload**, deliberately not a PR in §6. Two mechanisms exist and
  each is one client flag: `-c fetch.uriProtocols=https` (packfile-uri, protocol
  v2 only, client must opt in — G11; upstream's server side only offloads blobs
  above a threshold via `uploadpack.blobPackfileUri`), and
  `-c transfer.bundleURI=true` (bundle-uri, git ≥ 2.38, seeds a clone from
  typically CDN-hosted bundle files and is likelier to shift the bulk of a full
  clone). Which to use — if either — is the provider's answer, not ours, and
  neither is worth carrying without a measurement from a landed PR 4.