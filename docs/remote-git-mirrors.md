# Remote Git mirrors: requirements and delivery plan

Status: discussion document. Nothing in this document is implemented.

This document proposes how the agent should use a server-provided remote Git
mirror as a preferred source for repository data, with the pipeline's canonical
repository as the fallback and durable `origin`.

It incorporates the implementation and review history from
[#4144](https://github.com/buildkite/agent/pull/4144) and
[#4153](https://github.com/buildkite/agent/pull/4153), but deliberately starts
again from the checkout behavior we want. In particular, it does not preserve
the fresh-checkout-only, exact-SHA-only, isolated `git init` plus `git fetch`
design from #4153.

Read [How Git Mirrors Works](git-mirror.md) and the
[public Git mirrors documentation](https://buildkite.com/docs/agent/self-hosted/configure/git-mirrors)
for the existing on-host mirror behavior.

## Summary

A remote mirror is an optional, customer-configured replica of the canonical
repository. The agent should prefer it anywhere the default checkout would
otherwise transfer Git data from the canonical repository:

1. Refresh an existing on-host Git mirror, or populate a new one, from the
   remote mirror.
2. Route any network refresh still required by a reused checkout to the remote
   mirror, whether or not an on-host mirror is configured.
3. Clone a new checkout from the remote mirror, retaining the on-host mirror as
   a `--reference` object store when one is configured.
4. Enable Git protocol v2 packfile URI negotiation on mirror commands so the
   mirror provider can serve bulk pack data from a CDN.

If the remote mirror cannot satisfy the job, the agent falls back to the
canonical repository. A lagging mirror should cost one failed or incomplete
mirror operation plus the canonical delta; it should not cause the checkout to
be deleted and cloned again.

The implementation should route the agent's existing clone and fetch operations
to a preferred source. It should not build a second clone implementation by
translating arbitrary clone flags into `git init`, config writes, and fetch
flags.

## Terminology

- **Canonical repository**: the pipeline repository in `BUILDKITE_REPO`. It is
  the final authority and the durable URL for `origin`.
- **Remote mirror**: the server-provided Git URL for a customer-configured
  replica of the canonical repository.
- **On-host mirror** or **local mirror**: the existing bare repository under
  `--git-mirrors-path`, shared by checkouts on an agent host.
- **Existing checkout**: a checkout directory with a reusable `.git`
  directory, commonly used by long-lived agents.
- **Fresh checkout**: a checkout directory without a `.git` directory.
- **Mirror attempt**: one network operation, or one bounded group of operations,
  using the remote mirror before canonical fallback.

The on-host mirror and the remote mirror are different layers. When both are
present, the intended data path is:

```text
remote mirror -> on-host mirror -> checkout
       |                 ^
       +-- miss ----------+-- canonical repository
```

This is tiered caching, not two competing checkout implementations.

## Product assumptions

These assumptions are part of the feature contract:

1. The pipeline customer opts in and controls both the canonical repository and
   its mirror.
2. The mirror is a replica of the same logical repository. Replication lag is
   expected; adversarially different repository contents are not.
3. The mirror URL contains no credentials and is not sensitive. It may appear
   in debug logs, traces, or transient Git files such as `FETCH_HEAD`.
4. The mirror uses HTTP or HTTPS and authenticates through
   `buildkite-agent git-credential-helper` and the provider-neutral repository
   credential API. HTTPS is the production default.
5. The customer remains responsible for broad or unusual Git configuration
   supplied through hooks, environment variables, templates, or checkout
   flags.

These assumptions are intentionally less defensive than #4153. This is an
opt-in performance feature between cooperating systems, not a security boundary
between mutually distrusting repository hosts.

## Required behavior

### Correct checkout

- Mirror eligibility requires an immutable expected commit: a full object ID
  for the repository's hash format, or an equivalent immutable value attested
  by the backend. Abbreviated hashes and symbolic revisions can resolve
  differently on a lagging replica and are not sufficient.
- When `BUILDKITE_COMMIT` identifies that immutable commit, the final `HEAD`
  must resolve to it regardless of whether the mirror hits or falls back.
- A mirror must not be used as the sole authority when the job only asks for a
  mutable `HEAD` and provides no expected commit. A lagging but otherwise
  successful branch fetch is indistinguishable from the current branch in that
  case. Use canonical, or require the backend to provide an immutable expected
  commit.
- Custom refspec, pull request, tag, and branch behavior should use the same
  refspec selection that the default checkout uses today. If the mirror does
  not serve a provider-specific ref such as `refs/pull/*`, fall back.
- Commit verification remains authoritative. If strict verification needs the
  canonical branch tip, it must use canonical rather than accepting a stale
  mirror ref.

### Preserve existing checkout semantics

- The clone attempt receives the existing `BUILDKITE_GIT_CLONE_FLAGS`
  unchanged, including depth, filters, sparse options, `--single-branch`,
  `--no-tags`, and repeated `--config` values.
- Fetch attempts receive the existing `BUILDKITE_GIT_FETCH_FLAGS` unchanged.
- Shallow, partial, blobless, and sparse checkouts use the mirror with the same
  options they use against canonical.
- Local mirror locking, snapshots, `reference` versus `dissociate` mode,
  split-phase behavior, and `--git-mirrors-skip-update` keep their current
  semantics.
- Custom checkout hooks continue to replace the default checkout. The agent
  does not try to impose remote mirror behavior on custom checkout code.
- Submodules continue using their own URLs. A mirror URL for the main repository
  must never be applied to a submodule.
- Git LFS continues using the canonical repository unless the product later
  defines an explicit LFS mirror contract.

### Preserve the canonical repository at stable boundaries

At the start of user code and post-checkout hooks:

- `remote.origin.url` is the canonical repository.
- The remote mirror is not retained as a second push destination.
- A mirror URL from an earlier job does not determine the source for a later
  job.

Prefer a command-scoped URL rewrite:

```text
-c url.<remote-mirror>.insteadOf=<canonical>
```

Git commands continue to receive the canonical URL, so clone persists canonical
`origin`, while transport for that command goes to the mirror. This also lets a
partial clone's lazy fetch use the mirror during checkout without mutating
durable repository state. The rewrite must be command-scoped and must not
persist in checkout or global Git configuration.

### Fail open without multiplying retries

- A mirror miss, authentication failure, transport failure, unavailable
  packfile URI, or missing promised object falls back to canonical.
- A mirror attempt does not use the canonical path's nested retry budget. Try
  the mirror once, then let the existing canonical retries apply.
- Once a mirror attempt fails for a job, outer checkout retries go directly to
  canonical. Persist this outcome in executor state rather than treating each
  outer attempt as a new opportunity to pay the mirror penalty.
- Give the mirror a short, mirror-specific sub-deadline that leaves time for
  canonical fallback. This cap applies even when the overall checkout has no
  configured timeout. Parent cancellation still stops both sources.
- Cancellation and the checkout deadline stop work; they do not trigger a new
  canonical attempt after the job has been cancelled.
- A mirror miss in an existing checkout or on-host mirror must not delete that
  repository.
- If a mirror-backed clone produced a usable repository but the requested
  commit is missing, keep the objects already downloaded and fetch the missing
  delta from canonical.
- If the clone itself failed before creating a usable repository, clean up only
  the partial clone state before performing the normal canonical clone.

Start with no mirror retries and a conservative cap such as 30 seconds when
there is no parent deadline. When a parent deadline exists, use at most half of
its remaining time so canonical fallback retains a budget. Tune both policies
from hit, miss, fallback, and latency data.

### Configuration and binding

- Use a new protected value such as `BUILDKITE_GIT_REMOTE_MIRROR_URL`.
  `BUILDKITE_REPO_MIRROR` already means the on-host mirror directory.
- The backend may provide the URL, but hooks, plugins, secrets, and the Job API
  must not replace it.
- Capture the canonical repository and mirror URL together as one immutable
  job-level binding before hook-mutable checkout configuration is applied. One
  eligibility method should compare the effective post-hook repository with
  that binding, require an immutable expected commit, and apply rollout and
  allowlist policy for every path.
- If a hook changes `BUILDKITE_REPO`, skip the mirror rather than applying the
  old repository's mirror to the new repository.
- Check the mirror URL against the agent's repository allowlist. If it does not
  match, do not contact it; log why it was skipped and continue with the
  already-allowed canonical repository. An optional optimization must not make
  an otherwise-valid job fail validation.
- Keep an agent-side kill switch for rollout and incident response.
- Do not key an on-host mirror directory by the remote mirror URL. Continue to
  key it by the canonical repository so mirror URL rotation does not fragment
  or discard the cache.

## Trust and credential model

The agent should make a best effort to keep credentials scoped to their
intended host:

- The remote mirror URL has no embedded credentials.
- Agent-managed credentials come from the provider-neutral credential helper,
  which receives the URL Git is accessing.
- The agent must not copy credentials parsed from the canonical URL into the
  mirror URL.
- Any Git URL passed as an argument must follow `--` so it cannot be interpreted
  as an option.
- The agent should not persist a generated bearer token in checkout or mirror
  Git config.

The agent does not need to sanitize or reinterpret every customer-supplied Git
setting. For example, a customer can configure a global `http.extraHeader`, a
credential helper that ignores its input URL, a Git template containing
credentials, or a hook that rewrites Git configuration. Those settings may
affect both canonical and mirror requests. Attempting to make such
configurations safe led #4144 and #4153 toward an increasingly incomplete clone
emulator.

This limitation should be documented at the public opt-in point:

> Remote mirrors use the job's normal Git environment and checkout flags.
> Host-wide or unscoped credentials may therefore be sent to the mirror. Use
> URL-scoped credentials or Buildkite's repository credential helper.

## Source selection by checkout state

| On-host mirror | Existing checkout | Preferred behavior |
| --- | --- | --- |
| configured | yes | Refresh the on-host mirror from the remote mirror; if the reused checkout still needs a network fetch, route it to the remote mirror too |
| configured | no | Populate or refresh the on-host mirror from the remote mirror; clone through the remote mirror with the existing `--reference` behavior |
| absent | yes | Fetch and materialize the requested checkout through a command-scoped mirror rewrite |
| absent | no | Clone the canonical URL through a command-scoped mirror rewrite with the normal clone flags |
| any | custom checkout hook | No agent-managed behavior; expose configuration for the hook only if that is an explicit product decision |

Populate or refresh the on-host mirror first because it benefits all checkouts
on the host. This does not suppress remote source selection for a subsequent
checkout operation. `--git-skip-fetch-existing-commits` may make that operation
unnecessary when the expected commit is already available through the local
mirror; otherwise the checkout also prefers the remote mirror.

### One source-attempt boundary

One checkout-owned module should expose explicit existing-repository and
fresh-clone entry points. Both use the same attempt policy:

1. Confirm the immutable mirror binding is eligible.
2. Apply the mirror URL rewrite only to commands in one bounded source attempt.
3. If the mirror succeeds, return success without contacting canonical.
4. If the mirror failed and the parent context is still active, run the
   canonical operation.
5. Record a failed mirror outcome so outer retries do not attempt it again.
6. Consume the mirror error inside the boundary; only a canonical fallback
   error reaches the existing outer retry and checkout-cleanup logic.

The existing-repository entry point applies the rewrite to fetch and checkout
materialization commands, including lazy partial-clone fetches. The fresh-clone
entry point applies it to the real clone and initial materialization commands.
Neither entry point mutates or restores `origin`.

The existing fetch dispatcher combines refspec selection, retries, and
execution against `origin`. Before PR 2 adds source selection, extract the
existing behavior into a pure request selection and an execution step that
takes a source and retry policy. This is one fetch model used by both sources,
not a mirror-specific planner. Canonical retains its current retries and broad
ref fallback; the mirror gets one attempt.

## Path 1: refresh the on-host mirror from the remote mirror

This should be the first behavior shipped. It has high value for Buildkite
hosted agents, where attached cache volumes preserve on-host mirrors populated
by earlier jobs, and it leaves the checkout flow itself almost unchanged.

### Existing local mirror

1. Acquire the existing update lock.
2. Keep `remote.origin.url` set to canonical.
3. If the expected commit is already present, preserve the current fast path.
4. Otherwise fetch the current job's normal mirror refspec from `origin` with
   the command-scoped canonical-to-mirror rewrite.
5. Verify that the expected commit is now present when the job provides one.
6. On a miss or mirror transport error, fetch the same refspec from canonical
   `origin`.
7. Create the existing snapshot, if applicable, and continue unchanged.

The command-scoped rewrite avoids changing the shared mirror's durable
`origin`. This matters because `updateRemoteURL` treats an origin change as a
repository rename and may run `fsck` and `gc`; a rotating server-provided URL
must not trigger that work on every job.

### New local mirror

1. Under the clone lock, attempt the existing `git clone --mirror` operation
   using the canonical URL plus the command-scoped mirror rewrite, with
   `BUILDKITE_GIT_CLONE_MIRROR_FLAGS`.
2. Confirm that the clone persisted canonical `origin`.
3. Check or fetch the job's required ref/commit from the remote mirror.
4. If the mirror clone fails, remove the partial mirror directory and perform
   the existing canonical mirror clone.
5. If the mirror clone succeeds but is behind, keep its downloaded objects and
   fetch the missing delta from canonical. Do not discard and re-clone it.

`--git-mirrors-skip-update` keeps its current scope: contact neither source
while refreshing the on-host mirror and use the existing local mirror if
present. The normal checkout clone and fetch may still contact canonical,
especially when the local mirror is missing or incomplete.

### Scope

The main repository gets the server-provided URL. Submodule mirrors continue to
refresh from each submodule's canonical URL because the backend has not supplied
a mirror mapping for them.

## Path 2: refresh an existing checkout from the remote mirror

This path matters for long-lived agents that run many jobs for the same
pipeline. It must not replace an incremental fetch with a fresh clone.

For any reused checkout, including one using an on-host mirror in `reference`
or `dissociate` mode:

1. Reconcile the checkout's durable `origin` with canonical as today.
2. Apply the command-scoped canonical-to-mirror rewrite to the source
   acquisition portion of checkout.
3. Run the existing fetch selection with the existing fetch flags.
4. Apply the same rewrite to checkout materialization so shallow and partial
   clones request required objects from the mirror, not canonical.
5. If the fetch or materialization shows that the mirror is behind, rerun the
   source-dependent operation without the rewrite, against canonical, without
   deleting the checkout.

This should be a small routing layer around `fetchSource` and checkout
materialization, not a separate fetch planner.

Commit verification may deliberately contact canonical even after a mirror hit.
Its purpose is to establish canonical branch membership, so a stale mirror ref
must not cause a strict verification failure or success.

## Path 3: clone a fresh checkout from the remote mirror

For any fresh checkout, with the existing local `--reference` flags included
when an on-host mirror is configured:

1. Invoke the real `git clone` with the canonical URL, the command-scoped mirror
   rewrite, and the same computed clone flags the canonical path would use.
2. Apply the rewrite while the existing fetch and checkout logic obtains and
   materializes the requested commit.
3. If the mirror clone fails, remove partial checkout state and run the normal
   canonical clone.
4. If the clone produces a usable repository but a later fetch or lazy object
   request misses, retain the cloned objects and fetch the delta without the
   rewrite.
5. Verify that `origin` is canonical before canonical-only operations and user
   code.

Using the real clone operation is the key simplification. Git owns:

- depth and shallow boundaries;
- partial-clone and promisor configuration;
- sparse and blobless behavior;
- `--single-branch`, tags, and remote refspec configuration;
- repeated `--config` values;
- repository templates and platform-specific checkout behavior.

The agent should not parse an allowlist of "safe" clone flags and synthesize
their durable effects. Unknown or future Git flags should work against a mirror
for the same reason they work against canonical.

Some clone flags make the initial clone itself depend on replica freshness. For
example, `--branch new-branch` fails before leaving a usable repository when
that branch has not replicated. In that case canonical fallback must clone
again. Preserving real Git semantics is more important than claiming that every
possible lag fallback can reuse partial clone work.

### Refs and tags from a lagging mirror

A mirror-backed clone can leave remote-tracking refs and tags at the mirror's
replication point rather than canonical's current point. This is accepted for
the opt-in feature:

- The expected build commit remains the checkout invariant.
- The mirror and canonical are the same customer-controlled repository.
- Incidental refs can be stale until a later canonical fetch, just as reads
  from other asynchronous replicas can be stale.

Trying to prevent every mirror-derived mutable ref caused #4153 to create a
checkout with no normal clone refs or tags, which in turn changed commands such
as `git rev-parse origin/main` and `git describe --tags`. A normal mirror-backed
clone is the more useful and predictable result.

If a customer requires every incidental ref to be current at job start, remote
mirrors are not a suitable optimization: proving that property requires a
canonical ref advertisement or fetch and removes much of the benefit.

Clone-owned configuration can also reflect the replica's state.
`--single-branch`, including its depth-implied form, derives
`remote.origin.fetch` from the clone source's symbolic `HEAD`. A default-branch
rename that has not replicated can therefore leave a durable refspec for the
old branch even though durable `origin` is canonical. This rare case is an
accepted lag caveat; jobs still fetch and check out their expected commit
explicitly. A later implementation may reconcile this metadata when canonical
fallback is already required, but should not add a canonical preflight to
every shallow clone.

## Path 4: Git protocol v2 packfile URIs

Git's
[packfile URI protocol](https://git-scm.com/docs/packfile-uri) lets a protocol
v2 server replace part of a fetch response with packfiles served from HTTP or
HTTPS URIs. The client opts in with `fetch.uriprotocols`; the server decides
whether to advertise and use the capability.

For operations against the remote mirror:

- Run Git with command-scoped `protocol.version=2`.
- Allow only `https` in `fetch.uriprotocols` for production.
- Let Git download, verify, index, and retain the packs. Do not implement a
  Buildkite-specific pack downloader.
- Apply the same command-scoped configuration when a new on-host mirror is
  populated from the remote mirror.
- If a packfile URI cannot be downloaded or verified, treat the mirror
  operation as failed and use the normal canonical fallback.

Do not add agent-side eligibility parsing for full, shallow, or partial clones.
Git already sends the client capability, the server decides whether to
advertise and use it, and unsupported combinations are a no-op. Existing depth
and filter flags remain unchanged.

This support should be a separate pull request because the Git feature remains
documented as experimental, needs a provider contract, and deserves an
end-to-end test against a server that actually advertises packfile URIs.

## Fallback details

Fallback is a source decision, not a second checkout attempt.

Errors such as `not our ref`, `couldn't find remote ref`, refusal of an
unadvertised object, missing promisor objects, HTTP failures, and credential
failures are consumed by the mirror source-attempt boundary. The canonical
operation then runs once without the command-scoped rewrite. Only its result
reaches the existing "checkout may be corrupt, remove it" recovery.

The boundary should also check whether the expected commit is present after a
nominally successful mirror command. This avoids expanding global `gitError`
classification or parsing every server's wording for a miss.

Errors that are clearly local—invalid flags, an unwritable filesystem, a
corrupt existing checkout, or cancellation—retain the existing handling.
Where Git's exit status is ambiguous, canonical may be attempted once and its
result becomes authoritative; it is better to pay one redundant command than
to make mirror availability fail a job.

## Observability

Every behavior-changing pull request should add enough telemetry to answer:

- Was a remote mirror configured and eligible?
- Which path used it: local mirror population, local mirror refresh, existing
  checkout fetch, or fresh clone?
- Did it hit, miss because of lag, fail because of transport/authentication, or
  get skipped?
- Did canonical fallback succeed?
- How much time was spent on the mirror and fallback?
- Was packfile URI support enabled for the mirror command?

Use bounded, low-cardinality values in trace attributes. Do not put repository
or mirror URLs in metric labels.

The job log should make fallback understandable without presenting a healthy
lagging mirror as a checkout failure. For example:

```text
Fetching repository data from remote Git mirror
Remote Git mirror does not yet contain the requested commit; fetching the delta from origin
```

## Proposed pull request stack

The stack is ordered by value and by how much of the checkout lifecycle each
change touches. Each pull request is independently useful and revertible.

### PR 1: refresh on-host mirrors from the remote mirror

- Add protected remote mirror URL plumbing, canonical binding, allowlist
  validation, rollout kill switch, and baseline tracing.
- Populate and refresh the main repository's on-host mirror from the remote
  mirror first, with canonical fallback.
- Preserve canonical keying, locking, snapshots, skip-update, checkout modes,
  and submodule behavior.

This is the first production milestone. It directly benefits hosted agents with
attached cache volumes and has the smallest checkout compatibility surface.

### PR 2: fetch reused checkouts from the remote mirror

- Wrap the existing fetch and checkout materialization with command-scoped
  mirror source selection, whether or not an on-host mirror is configured.
- Separate the current fetch request selection from execution so the same
  request can run once against the mirror and with existing retry behavior
  against canonical.
- Preserve fetch flags and shallow/partial checkout behavior.
- Restore canonical `origin` and fall back without deleting the checkout.
- Keep commit verification canonical where required.

This milestone covers the long-lived agent case and should include a test that
runs successive jobs against the same checkout.

### PR 3: clone fresh checkouts from the remote mirror

- Use the existing `git clone` call with unchanged computed clone flags.
- Preserve `--reference` and `--dissociate` when an on-host mirror is
  configured; the remote mirror remains the transport source.
- Apply the command-scoped URL rewrite through initial fetch/materialization;
  keep canonical `origin` throughout.
- Reuse successful mirror-clone objects on a lag fallback.
- Cover shallow, blobless, sparse, single-branch, no-tags, custom config, pull
  request, and custom-refspec cases through the normal checkout code.

This replaces the `git init` plus clone-flag emulation approach from #4153.

### PR 4: enable packfile URI negotiation

- Enable Git protocol v2 HTTPS packfile URI negotiation on remote-mirror
  commands, including new on-host mirrors.
- Add provider-level end-to-end coverage, fallback tests, and telemetry.
- Let Git and the server negotiate support; keep shallow and partial clone
  flags unchanged.

A foundations-only pull request is not required. The immutable binding belongs
with its first use in PR 1, and the fetch selection/execution seam belongs with
its first use in PR 2.

## Review strategy

The existing review history shows that reviewers and review models struggle
when one pull request introduces a new checkout state machine and then attempts
to prove parity with every Git option. The proposed stack should instead make
the source change visible in the existing state machine.

For each pull request:

1. Start the description with the one checkout path that changes.
2. Include a before/after command sequence for hit and lag fallback.
3. Keep the canonical path structurally unchanged.
4. Use the existing Git wrappers and error types rather than adding a parallel
   command framework.
5. Put accepted caveats in code comments next to the source-selection boundary,
   not as scattered special-case checks.
6. Prefer real Git integration tests with a canonical repository and a lagging
   replica over tests of private flag parsers.
7. Keep tests for each path in focused files. Avoid another single
   thousand-line remote-mirror test file.
8. Ask reviewers whether an issue can violate the stated product assumptions
   before adding defensive code for it.

### Test matrix

Each applicable path should cover:

| Case | Expected result |
| --- | --- |
| mirror has expected commit | canonical object transfer is not used |
| mirror lags expected commit | canonical supplies the delta; checkout is not re-cloned |
| mirror transport/auth failure | one mirror attempt, then canonical |
| outer checkout retries after mirror failure | subsequent attempts use canonical only |
| cancelled checkout | no fallback after cancellation |
| hook changes `BUILDKITE_REPO` | bound mirror is skipped |
| commit is abbreviated, symbolic, or `HEAD` | mirror is skipped |
| mirror is not allowlisted | mirror is skipped; allowed canonical checkout continues |
| URL begins with option-like text | passed after `--`, never parsed as an option |
| shallow checkout | same depth flags and shallow behavior from mirror |
| partial/blobless checkout | same filters; required checkout objects come from mirror during the attempt |
| repeated clone config | real clone preserves Git behavior |
| reused checkout across jobs | second job fetches from mirror without cloning |
| local mirror cache across jobs | cache remains keyed by canonical and refreshes from mirror |
| local mirror skip-update | no local-mirror refresh; normal checkout behavior is unchanged |
| strict commit verification | canonical branch authority is preserved |
| custom checkout hook | default remote mirror logic does not run |

Fresh-clone tests should compare important observable state with a canonical
clone—`HEAD`, shallow state, promisor config, origin fetch refspec, tags, and
working tree—but should not require byte-for-byte identical `.git` directories.

## Prior review concerns and proposed disposition

| Concern from #4144 / #4153 | Disposition |
| --- | --- |
| Clone flags and repeated `--config` values are difficult to emulate | Use the real clone with unchanged flags |
| `--depth`, `--single-branch`, and `--no-tags` persist config | Let the real clone persist it |
| Partial clones can lazy-fetch outside a one-shot mirror fetch | Apply the command-scoped URL rewrite through checkout materialization; fall back on a mirror-side miss |
| Existing partial checkouts may already have a canonical promisor | Apply the rewrite to the promisor remote's canonical URL during materialization; test this explicitly |
| A SHA-only `git init` checkout lacks normal refs and tags | Do not synthesize a SHA-only checkout; accept replica-lagged incidental refs |
| Mirror-derived tags or refs can lag | Accepted opt-in caveat; the expected build commit remains invariant |
| Canonical `http.extraHeader` could be sent to the mirror | Do not add canonical credentials; document that customer-supplied unscoped Git config applies to both |
| Git templates and `GIT_*` variables can alter the mirror operation | Preserve normal Git semantics; do not build a sanitized clone environment |
| Mirror URL remains in `FETCH_HEAD` | Accepted: the URL is explicitly non-sensitive and credential-free |
| Hook changes canonical repository after mirror assignment | Skip the mirror because its canonical binding no longer matches |
| Symbolic or abbreviated revisions can resolve differently on a replica | Require a full immutable object ID or backend-attested equivalent |
| Repository value can be parsed as a Git option | Always use `--`; this is a correctness/security requirement |
| Mirror failure can multiply checkout retries | One non-retrying mirror attempt, then existing canonical retry behavior |
| Mirror miss can wipe a reusable checkout | Consume mirror errors inside one source-attempt boundary; expose only canonical failure to existing cleanup |
| Provider-neutral and legacy credential rewrite behavior | Preserve the already-separated credential implementation; do not mix it into mirror routing |

## Caveat register

These are accepted limitations unless production evidence justifies more work:

1. Remote-tracking refs and tags obtained from the mirror may be behind
   canonical.
2. The credential-free mirror URL may remain in `FETCH_HEAD`, reflogs, debug
   output, or traces.
3. Broad customer-provided Git credentials or config may apply to both hosts.
4. Custom checkout hooks own their Git behavior.
5. Main-repository mirror configuration does not accelerate submodules or Git
   LFS.
6. A failed mirror operation adds work before canonical fallback. A successful
   clone followed by lag fallback may download mirror data plus a canonical
   delta.
7. Clone options that require a not-yet-replicated ref may make the mirror clone
   fail before it leaves reusable objects, requiring a canonical re-clone.
8. Single-branch configuration may reflect a lagging mirror's symbolic `HEAD`
   even though `origin` remains canonical.
9. Packfile URI behavior depends on the installed Git version and mirror
   provider implementation.
10. Changing or removing the configured mirror URL does not purge objects
   already present in a checkout or on-host mirror; Git objects from the
   customer-controlled replica remain valid cache contents.

The register should be maintained as behavior ships. A caveat can become a
requirement when it is observed in ordinary customer configurations; it should
not become implementation complexity solely because a pathological
configuration can be constructed.

## Rollout

1. Ship behind an agent-side experiment or kill switch.
2. Enable on hosted agents that already use attached on-host mirror caches.
3. Measure hit, lag fallback, transport failure, and latency by checkout path.
4. Enable reused-checkout and fresh-clone paths separately.
5. Add packfile URI support only after the mirror provider contract and
   end-to-end fixture exist.
6. Remove the experiment after fallback rates and checkout latency are
   understood.

The backend advertises an optimization; it does not make the mirror mandatory.
Canonical remains the final authority and fallback throughout the rollout.
