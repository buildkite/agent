# Remote Git mirrors: requirements and plan

Status: proposal. This document captures the requirements for server-provided
remote Git mirror URLs, and proposes a stack of pull requests to implement
them. It supersedes the approach in
[#4153](https://github.com/buildkite/agent/pull/4153) (extracted from
[#4144](https://github.com/buildkite/agent/pull/4144), which also superseded
[#4132](https://github.com/buildkite/agent/pull/4132)). The provider-neutral
repository credentials work ([#4152](https://github.com/buildkite/agent/pull/4152))
is already merged and is a foundation for this.

## The feature in one paragraph

A pipeline whose canonical repository lives on a slower or less reliable host
can configure a faster cloud-hosted **remote mirror** of that repository. The
backend passes the mirror URL to the agent as job env
(`BUILDKITE_GIT_REMOTE_MIRROR_URL`). Wherever the checkout phase would
transfer Git data over the network from the canonical repository, it prefers
the mirror, and falls back to the canonical repository when the mirror doesn't
(yet) have what the job needs. For eligible builds against a correctly
configured mirror, replication lag is the only situation in which using a
mirror may be mildly slower than not configuring one.

## Background: the three bulk transfer points this design changes

The default checkout (`defaultCheckoutPhase` in `internal/job/checkout.go`)
already has a tiered structure. The bulk of Git network transfer from the
canonical repository happens at three points:

1. **On-host mirror refresh** (`updateGitMirror` in
   `internal/job/checkout_mirror.go`): when the agent is configured with
   `--git-mirrors-path`, a bare `--mirror` clone per repository is shared
   across jobs/agents on the host. It is cloned once from the canonical repo
   and then incrementally refreshed (`git fetch origin <refspec>`) per job.
   Checkouts borrow its objects via `git clone --reference` (see
   [git-mirror.md](git-mirror.md)). Buildkite hosted agents use this, with
   the mirror directory on an attached cache volume populated by earlier
   jobs, so this path is high-traffic.
2. **Existing checkout refresh** (`fetchSource` in
   `internal/job/checkout_fetch.go`): long-lived agents reuse the checkout
   directory across jobs for the same pipeline, fetching just the new delta
   from `origin`.
3. **Fresh clone** (`gitClone` from `defaultCheckoutPhase`): no existing
   checkout; a full (or shallow/partial, per `BUILDKITE_GIT_CLONE_FLAGS`)
   clone from the canonical repository, followed by the same `fetchSource`
   delta fetch. (When an on-host mirror exists, the fresh clone uses
   `--reference <mirror>` and most objects come from the mirror, but the
   clone still contacts canonical for ref advertisement and any missing
   objects — that residual traffic is covered by tier A refreshing the
   mirror, not changed directly.)

The design principle of this proposal is: **the remote mirror is an alternate
URL for the same repository data, substituted at these existing transfer
points** — not a separate acquisition pipeline bolted on in front of them.
`origin` always remains the canonical repository URL; the mirror URL is used
per-invocation and never configured as a persistent remote.

## Requirements

- **R1 — Fall back, never fail.** The mirror is an optimization. Any miss
  (replication lag), timeout, authentication failure, or transport failure
  falls back to the canonical repository within the same checkout attempt.
  A configured mirror must never make a build fail that would otherwise
  succeed.
- **R2 — Mirror lag is the only acceptable steady-state regression.** For an
  eligible build against a correctly configured mirror, the only acceptable
  slowdown relative to not configuring a mirror is replication lag: the job
  pays for one wasted attempt (a failed fetch, or on the fresh-clone tier a
  slightly stale clone healed by a canonical delta fetch) before falling
  back. Configurations that would make the mirror miss *every* time (see the
  eligibility rules and caveat register) must disable the mirror attempt up
  front rather than paying the miss per job, wherever we can detect them
  cheaply.
- **R3 — Respect checkout shape.** Pipelines using shallow clones, partial
  (blobless) clones, single-branch, no-tags, sparse checkout, etc. (via
  checkout attributes / `BUILDKITE_GIT_CLONE_FLAGS` / `BUILDKITE_GIT_FETCH_FLAGS`)
  must get the same shape when the transfer comes from the mirror. A mirror
  hit must not silently turn a shallow clone into a full-history transfer, or
  vice versa.
- **R4 — Tiered caching with on-host mirrors.** When the agent uses the
  pre-existing on-host git mirrors feature, the on-host mirror is refreshed
  *from the remote mirror* (falling back to canonical), instead of always
  from canonical. Everything downstream of the on-host mirror
  (`--reference` clones, snapshots, `git-skip-fetch-existing-commits`) is
  unchanged.
- **R5 — Checkout reuse keeps working.** When an agent has an existing
  checkout for the pipeline, the delta fetch comes from the mirror (falling
  back to canonical). The mirror must never cause an existing checkout to be
  discarded and re-cloned.
- **R6 — CDN offload for bulk transfers.** For large transfers (fresh full
  clones, initial on-host mirror clones), support packfile-URI in Git
  protocol v2 (`fetch.uriProtocols=https`) so that mirror providers can
  serve the bulk of pack data from a CDN.
- **R7 — Credentials via the provider-neutral credential API.** Mirror
  transfers authenticate with `buildkite-agent git-credentials-helper`, which
  requests a token for the specific repo URL from the backend's
  `repository_access_token` endpoint (merged in #4152). The backend / mirror
  repository provider handle the OAuth relationship. No new credential
  plumbing in the agent.
- **R8 — Best-effort credential separation.** Make a reasonable effort to
  prevent bearer credentials intended for the canonical repository being sent
  to the mirror, and vice versa. This is *not* a hard isolation boundary
  (see "Trust model" below).
- **R9 — Behavioral parity on hit vs miss.** After checkout, the resulting
  repository state (refs, remotes, persisted config, promisor configuration)
  should be the same whether the data happened to come from the mirror or
  from canonical, modulo mirror replication lag. Jobs should not be able to
  observe "the optimization hit" except by being faster.
- **R10 — Incremental delivery.** Each stage must be independently shippable
  and useful. Shipping only R4 (on-host mirror refresh) is already a major
  win because hosted agents use on-host mirrors.

### Trust model and pragmatism

The canonical repository and the mirror are both under the control of the
same customer, and contain the same repository data. There is no adversarial
relationship between the pipeline customer, the canonical repo host, and the
mirror repo host: the customer opts in, and the customer is the one feeding
the agent config, hooks, and environment. Consequences:

- The mirror URL can be assumed to not contain credentials and to not be a
  sensitive value. It may appear in logs, `FETCH_HEAD`, error messages and
  trace attributes without redaction.
- We do not defend against the customer's own pathological configuration
  (hook-exported `GIT_*` env vars, ambient gitconfig, `url.<x>.insteadOf`
  rewrites, credentials injected via custom mechanisms). Where earlier
  iterations added machinery to sanitize or fail-closed on these, we instead
  keep a **register of caveats** (below) and add targeted, cheap gates only
  where a real bearer-credential-crossing risk exists (R8).
- Fail-open beats fail-closed: an ineligible or unrecognised configuration
  disables the mirror for that operation (with a log line) rather than
  refusing the job or growing an allowlist of every Git flag.

### Out of scope (for this stack)

- **Submodules.** The backend provides one mirror URL for the pipeline's main
  repository. Submodule mirroring keeps its existing canonical behavior.
- **Builds without a resolved commit SHA** (`BUILDKITE_COMMIT=HEAD` or a
  short SHA): the commit-presence test that drives fallback needs an exact
  full SHA, and fetching a mutable ref from a lagged mirror could check out
  an older commit than the canonical ref currently points to. These builds
  use the canonical path. (In practice the backend resolves and pins full
  SHAs for the overwhelming majority of builds.)
- **Tag builds** (`BUILDKITE_TAG` set): today's refresh paths rely on Git's
  DWIM ref matching to resolve a tag name passed as the "branch", which the
  explicit-refspec mirror fetch (tier A) does not reproduce; a lagged mirror
  may also lack the tag. Tag builds use the canonical path (matching #4153's
  `Tag == ""` eligibility gate); mirror-first tag fetches
  (`+refs/tags/<tag>:refs/tags/<tag>`) are a possible follow-up.
- **Custom refspecs and GitHub PR refs** (`refs/pull/N/head|merge`): mirror
  providers may not replicate these ref namespaces, so attempting them
  mirror-first would pay the R2 round-trip cost on every such build. Start
  canonical-only; revisit if mirror providers advertise PR-ref replication.
- **Server-side work** (pipeline settings UI, emitting
  `BUILDKITE_GIT_REMOTE_MIRROR_URL`, the mirror provider integration) is
  tracked elsewhere; this stack only consumes the env var.

## Design

### Eligibility (shared by all tiers)

A mirror-first transfer is attempted when all of:

- `BUILDKITE_GIT_REMOTE_MIRROR_URL` is set, parses as a URL, and has an
  `http`/`https` scheme (note the credential helper hard-rejects non-HTTPS
  URLs, so `http` mirrors are only viable unauthenticated — useful for
  tests, unlikely in production);
- `BUILDKITE_COMMIT` is a full 40-hex SHA (the immutable object whose
  presence defines hit vs miss), and `BUILDKITE_TAG` is empty;
- the fetch target is the build branch or the commit itself (not a custom
  `BUILDKITE_REFSPEC`, not GitHub PR refs — see out of scope). Branch
  targets only arise in the tier A mirror refresh; checkout-level fetches
  of eligible builds always target the commit;
- this is the first checkout attempt (retries after a failure go straight to
  canonical — retrying through an optimization only delays recovery);
- `BUILDKITE_REPO` still equals the repository the backend provided the
  mirror for. If an `environment`/`pre-checkout` hook redirected
  `BUILDKITE_REPO` (a supported pattern for proxies and alternate remotes),
  the mirror no longer corresponds to the repository being checked out, so
  it is skipped. This requires remembering the job's original repo value
  before hooks run.
- if the agent is configured with `allowed-repositories`, the mirror
  URL must also match. Unlike #4153 (which refused the whole job), a
  non-matching mirror URL disables the mirror with a warning and the build
  proceeds via canonical: the operator's policy is enforced (we never fetch
  from a URL outside the allowlist), and a backend-side optimization hint
  cannot fail a job the operator's policy would otherwise allow.

When ineligible, the behavior is exactly today's canonical behavior — no
mirror-specific code runs.

`BUILDKITE_GIT_REMOTE_MIRROR_URL` is protected env (as in #4153): it is
accepted from the backend job env at bootstrap, but hooks cannot override it
mid-job (`env/protected.go`, and no `env:` tag on the `ExecutorConfig` field).
Combined with the `BUILDKITE_REPO`-unchanged gate, hooks can *disable* the
mirror by redirecting the repo, but cannot *redirect* the mirror.

### The universal pattern: fetch from mirror, verify, fall back

Every tier uses the same shape:

```text
try:    git <op> <mirror-url> ... (scoped credentials, no retries)
verify: commit SHA present in the local object store
miss:   fall back to the same <op> against canonical, exactly as today
```

- Mirror-directed operations do not retry (`Retry: false` in `gitFetch`
  terms). A miss or failure immediately proceeds to canonical, which keeps
  its existing retry behavior. `gitFetch` learns to classify
  `not our ref` / `Server does not allow request for unadvertised object`
  as a distinct non-retryable "ref not on remote" error (as in #4153), so
  a lag miss is cheap and well-labelled in traces. This classification is
  enabled only for mirror-directed invocations: canonical fetches keep
  their existing error handling (today these strings fall into
  `gitErrorFetchRetryClean` and trigger the remove-and-reclone recovery),
  so no canonical behavior changes.
- Mirror-directed operations go through the existing `gitFetch`/`gitClone`
  helpers, which already pass the repository URL after a `--` argument
  terminator; any *new* Git invocation added by this work must do the same.
- Fetching an exact SHA requires the mirror server to allow unadvertised
  object requests (`uploadpack.allowAnySHA1InWant` or equivalent). This is a
  documented requirement on mirror providers; a server that refuses simply
  produces a permanent (but cheap and well-labelled) miss and canonical
  fallback.
- Timeouts: mirror operations run under the existing per-attempt checkout
  timeout (`BUILDKITE_GIT_CHECKOUT_TIMEOUT`) and checkout retry loop, with
  the "first attempt only" gate guaranteeing that a retry never touches the
  mirror. Rather than capping the mirror attempt at an arbitrary duration
  (as #4153's 30s / one-third-of-deadline did, which would kill legitimate
  large transfers), stalled-transfer detection is delegated to Git:
  mirror-directed operations set `-c http.lowSpeedLimit=<bytes/s>
  -c http.lowSpeedTime=<seconds>` so a hung or trickling mirror aborts
  quickly while a healthy large transfer is not time-capped. Exact values to
  be settled in the implementing PR. Residual window: these options cover
  the HTTP transfer, not the TCP/TLS connect phase; a black-holed connect is
  bounded by curl/OS connect defaults, and by the checkout timeout when the
  operator sets one.

### Credentials (R7, R8)

Mirror-directed operations pass per-invocation credential configuration:

```text
-c credential.useHttpPath=true -c credential.helper= -c "credential.helper=<agent> git-credentials-helper"
```

The empty `credential.helper=` resets inherited helpers so a helper
configured for canonical (or ambient in the environment) is not consulted
for the mirror URL; the agent helper then requests a token scoped to the
mirror URL from the provider-neutral endpoint. This is the same technique
as #4153 and is retained unchanged.

In the other direction (canonical credentials reaching the mirror), the two
realistic carriers are:

- `BUILDKITE_GIT_CLONE_FLAGS` containing `--config` keys that carry
  credentials: `http.extraHeader` (including URL-scoped
  `http.<url>.extraHeader`), `credential.*`, and `url.*.insteadOf`. On the
  fresh clone tier, these keys are withheld from the mirror-directed clone
  and applied to the repository afterwards with `git config --add`
  (preserving `git clone --config`'s additive semantics), so they cover the
  checkout and all later canonical transport but are not sent to the mirror
  host. All other `--config` keys — including transport-plumbing ones like
  `http.proxy` or `http.sslCAInfo` that a mirror-directed clone may
  genuinely need — pass through to the clone untouched. The list is
  deliberately the bearer-credential carriers only, not all of `http.*`.
- Local config persisted in a **reused checkout** by an earlier clone (the
  same key list). Before a mirror-directed fetch into an existing checkout,
  the agent checks `git config --local` for those keys; if any are present,
  the mirror is skipped for that job (log line, canonical fetch as today).
  This is a cheap targeted gate rather than an attempt to sandbox Git's
  config resolution. Note the interaction with the fresh-clone deferral: a
  pipeline whose clone flags include such a key gets the mirror-sourced
  clone on the first job, and canonical delta fetches on reuse thereafter.

Anything beyond that — hook-exported `GIT_CONFIG_*`, system/global
gitconfig, credentials embedded by custom tooling — is the customer's own
configuration applying to the customer's own mirror, and is accepted (see
caveat register). We deliberately do not reproduce #4144/#4153's
`GIT_CONFIG_NOSYSTEM`/null-config/`GIT_TEMPLATE_DIR` scrubbing or its
`GIT_DIR`-style env skip list.

### Tier A — refresh the on-host mirror from the remote mirror (R4)

In `updateGitMirror`, for the main repository only:

- **Initial mirror clone**: `git clone --mirror` from the remote mirror URL
  instead of canonical, then `git remote set-url origin <canonical>` inside
  the new mirror so all subsequent behavior (including other agents' jobs
  racing on the same host) treats canonical as `origin`, exactly as today.
  Today the initial-clone branch returns straight to snapshotting with no
  commit-presence check (the clone came from the authoritative source);
  with a mirror-sourced clone, new control flow is needed after the clone:
  verify the build's commit, and on a miss (lag) continue into the existing
  update-path code so the delta is fetched from `origin`. If the mirror
  clone fails outright, remove the partial dir (as the canonical clone
  failure path already does) and clone from canonical.
- **Incremental refresh**: where today fetches `git fetch origin <refspec>`
  (branch, or the commit-presence check already short-circuits), first
  `git --git-dir=<mirror> fetch --no-tags -- <mirror-url> +refs/heads/<branch>:refs/heads/<branch>`,
  then verify the commit; on miss, run today's `git fetch origin <refspec>`.
  Two deltas from the canonical form, both deliberate: the refspec is
  explicit because an anonymous URL fetch doesn't map refs into the mirror,
  and `--no-tags` is added because a fetch with an explicit destination
  refspec auto-follows tags where today's destination-less
  `git fetch origin <branch>` does not — without it, a mirror-directed
  refresh would import (possibly lagged) tags that the canonical refresh
  wouldn't have.
- The mirror-directed fetch runs after the existing `updateRemoteURL`
  rename handling, so `origin` is already correct for the fallback path.
- Locking, snapshotting, `--git-mirrors-skip-update`,
  `git-clone-mirror-flags`, submodule mirrors and everything downstream
  (`--reference` checkout, dissociate/reference modes) are untouched. In
  particular the mirror's `origin` remains canonical at all times, so mixed
  fleets (some agents with newer agent versions than others) behave
  correctly against the same shared mirror directory. (One R8 note:
  `--git-clone-mirror-flags` is operator-supplied agent config; if an
  operator puts canonical credentials in it, the tier A initial clone will
  send them to the mirror. Operator config is trust-model territory — see
  caveat 3.)

This tier alone delivers the hosted-agents use case: cache volumes are
populated and refreshed from the fast mirror, and checkouts already source
their objects from the cache volume.

### Tier B — refresh an existing checkout from the remote mirror (R5)

Applies when there is an existing `.git` in the checkout dir and no on-host
mirror is configured (when one is configured, tier A already moved the bulk
transfer to the mirror, and the checkout-level fetch keeps status quo; see
"Possible follow-ups").

In `fetchSource`, for the eligible refspec kinds (commit SHA; the branch
kind implies `Commit == "HEAD"` which is ineligible): fetch the SHA from the
mirror URL first (`git fetch -- <mirror-url> <sha>`), verify, fall back to
today's `git fetch origin <sha>` (with its fallback-to-all-heads-and-tags
behavior). Notes:

- Fetching a bare SHA from a URL updates `FETCH_HEAD` and the object store
  but no remote-tracking refs and no auto-followed tags — identical ref and
  object effects to today's `git fetch origin <sha>`, so R9 parity holds
  with no compensation logic. (Only the URL recorded in `FETCH_HEAD`
  differs; caveat 2.)
- The existing `GitSkipFetchExistingCommits` short-circuit runs first, as
  today. On a partial (blobless) clone, the commit-presence probes must not
  trigger a lazy fetch from origin (set `GIT_NO_LAZY_FETCH=1`, Git ≥ 2.45;
  a review finding on #4144 worth keeping). Older Gits ignore the variable
  and may lazy-fetch during the probe — accepted, since that is today's
  behavior for those probes anyway.
- The credential-bearing-local-config gate from the credentials section
  applies.
- A mirror miss or failure must leave the checkout exactly as it found it —
  a failed fetch adds objects at worst — and never triggers the
  remove-and-reclone recovery path.

### Tier C — fresh clone from the remote mirror (R3, R9)

Applies when there is no existing checkout and no on-host mirror.

**Clone from the mirror with the user's own clone flags, then re-point
origin at canonical:**

```text
git clone <user flags, minus deferred --config keys> [-c scoped credentials] -- <mirror-url> .
git remote set-url origin <canonical>
git config --add <deferred --config keys>   # extraHeader / credential.* / insteadOf
# then the normal flow: fetchSource (tier B mirror-first logic), verify, checkout…
```

This is the key simplification over #4153, which reproduced `git clone`
semantics via `git init` + config surgery + fetch, and consequently needed a
fail-closed allowlist for every clone/fetch flag, plus special handling for
`--single-branch`/`--depth`/`--no-tags`/`--filter`/`branch.*`/
`remote.origin.*`/repeated `--config` (each one a review round on #4144).
Letting `git clone` do the cloning gives us, for free:

- **R3 shape parity**: `--depth`, `--shallow-since`, `--filter=blob:none`,
  `--sparse`, `--single-branch`, `--no-tags` and any future flag behave on
  the mirror exactly as they would on canonical, including their persistent
  effects on `remote.origin.fetch`, `tagOpt`, and the shallow file.
- **Promisor retargeting for partial clones**: `git clone --filter` records
  `remote.origin.promisor=true` / `remote.origin.partialclonefilter` keyed
  by remote *name*; after `set-url`, `origin` is canonical, so later lazy
  blob fetches go to canonical with canonical credentials — the exact
  behavior #4153 implemented manually with `retargetRemoteMirrorPromisor`.
- **R9 ref parity**: the checkout has the normal refs a clone produces
  (`refs/remotes/origin/*`, tags, HEAD), rather than #4153's
  "no mutable refs" state that made `git rev-parse origin/main` and
  `git describe` behave differently on hit vs miss. The refs are the
  mirror's snapshot, i.e. canonical's refs modulo replication lag —
  see the caveat register.
- After the clone, the standard `fetchSource` runs (with tier B
  mirror-first for the SHA), so a lagged mirror clone self-heals by
  fetching the missing delta from canonical: R2's "mild regression at
  worst".

If the mirror clone fails, remove the created checkout dir and re-run the
clone against canonical within the same attempt (reusing the existing
remove/recreate helpers) — using the original, unmodified flag set,
including the `--config` keys the mirror attempt deferred, since a
canonical clone may depend on them (e.g. `http.extraHeader` to
authenticate) before its fetch.

The clone-vs-fetch decision, `--reference`/`--dissociate` handling, sparse
checkout, LFS, submodules, commit signing verification and everything after
source acquisition are unchanged.

### Tier D — packfile-URI support (R6)

Small and mostly orthogonal: mirror-directed clone/fetch operations add
`-c fetch.uriProtocols=https`, which lets a protocol-v2 mirror server
advertise `packfile-uris` and offload bulk pack data to a CDN. Git falls
back transparently when the server doesn't offer it. Canonical-directed
operations are left alone (canonical hosts generally don't support it, and
we don't want to change canonical behavior at all). The CDN URI is fetched
by Git over HTTPS using the same connection settings as the fetch; no agent
code handles the packfile bytes.

## Proposed PR stack

Stacked in order; each is independently revertible and useful. Sizes are
deliberately kept small enough for a single-sitting human review.

### PR 1 — foundations (no behavior change)

- `BUILDKITE_GIT_REMOTE_MIRROR_URL`: bootstrap flag/env plumbing into
  `ExecutorConfig` (no `env:` tag), protected-env entry + tests,
  allowlist handling (skip-with-warning semantics) in `run_job.go` + agent
  integration test.
- Shared eligibility helper (URL scheme, full-SHA check, refspec kind,
  original-repo-unchanged, first-attempt) with unit tests.
- `gitFetch` support for what the later tiers need: per-invocation extra
  `-c` config, and the `gitErrorFetchRefNotOnRemote` classification
  (`not our ref` / `Server does not allow request for unadvertised object`)
  with no-retry semantics, opt-in per invocation so canonical fetch error
  handling is untouched.
- `gitCredentialHelperFlags` helper (scoped credential config) extracted
  beside `configureGitCredentialHelper`.
- Trace attribute scaffolding (`git.remote_mirror.result` =
  hit/miss/timeout/error, `…fallback`, `…duration`).

Nothing consumes the value yet, so behavior is identical; this keeps each
behavioral PR focused on one transfer point.

### PR 2 — refresh the on-host mirror from the remote mirror (tier A)

The highest-value slice (hosted agents). Contains the `updateGitMirror`
changes, the initial-mirror-clone-from-mirror path with `set-url origin`,
commit-verification fallback, and integration tests against `githttptest`
(hit, lag-miss falls back to canonical fetch, mirror unavailable, mirror
requiring credential-helper auth, assert mirror dir's `origin` is always
canonical).

### PR 3 — refresh an existing checkout from the remote mirror (tier B)

`fetchSource` mirror-first SHA fetch with fallback; the
credential-bearing-local-config gate; `GIT_NO_LAZY_FETCH` on presence
probes. Tests: reused checkout hit/miss/failure (checkout preserved in all
cases), partial-clone reuse doesn't lazy-fetch during the probe, local
`http.extraHeader` disables the mirror, no `remote-tracking`-ref divergence
vs the canonical path.

### PR 4 — fresh clone from the remote mirror (tier C)

Clone-from-mirror + `set-url` + deferred credentialish `--config` keys +
failure cleanup/fallback. Tests: hit with canonical unreachable
(proves independence), shape parity matrix (depth / filter / single-branch /
no-tags: compare resulting `.git/config` + refs against a canonical clone
with the same flags — reuse the differential-test idea from #4153),
promisor points at canonical after a `--filter` hit, proxy-based assertion
that a canonical `--config http.extraHeader` bearer token is not sent to
the mirror (port of `TestRemoteMirrorFetchDoesNotSendCanonicalCloneHeaders`),
lag-miss self-heals via canonical delta fetch.

### PR 5 — packfile-URI (tier D)

`fetch.uriProtocols=https` on mirror-directed operations; a `githttptest`
(or fixture-server) test exercising a packfile-uris advertisement if
practical, otherwise verifying the config is applied and that a
non-supporting server is unaffected. Folded into PR 4 if it turns out to be
a handful of lines.

Documentation (agent docs + buildkite/docs) ships alongside PR 2 (the first
user-visible behavior) and is updated per tier.

## Anticipated review feedback, and how it's handled

Drawn from the actual review history of #4144 and #4153 (buildsworth, Codex,
and human reviewers). Each item is either **built into the design** or
**explicitly accepted** with a rationale that should also appear as a code
comment near the relevant code.

| # | Feedback (source) | Disposition |
|---|---|---|
| 1 | Repository URLs must follow a `--` terminator to prevent option injection (buildsworth on #4153's `ls-remote`, unresolved there) | Already satisfied: the shared `gitFetch`/`gitClone` helpers on `main` emit `--` before the URL, and the un-hardened `ls-remote` doesn't exist in this design (no canonical-HEAD probe is needed when `git clone` runs for real). New Git invocations must keep the discipline. |
| 2 | Mirror URL persists in `.git/FETCH_HEAD` (Codex P1 on #4153) | Accepted: the mirror URL is by definition non-sensitive (no embedded credentials); scrubbing `FETCH_HEAD` would be surgery on Git-owned state for no security benefit. Code comment at the mirror fetch site. |
| 3 | Hooks can change `BUILDKITE_REPO`, leaving the mirror bound to the wrong repository (Codex P2 on #4153) | Built in: eligibility requires the repo to be unchanged from the value the backend issued the mirror for; otherwise skip. |
| 4 | Mirror hit produces different refs than a canonical checkout (`git describe`, `origin/main`) (buildsworth on #4153, unresolved) | Designed out: tier C uses a real `git clone` so refs exist as normal; tiers A/B change only the transfer source of operations whose ref effects are identical either way. Residual: refs reflect the mirror's replication point; see caveat register. |
| 5 | Partial-clone promisor config must not leave lazy fetches pointing at the one-shot mirror URL (buildsworth, multiple rounds on #4144) | Designed out: promisor config is keyed by remote name (`origin`), and `origin`'s URL is set to canonical immediately after the clone. Covered by a PR 4 test. |
| 6 | Canonical clone-config credentials (`http.extraHeader`) must not be sent to the mirror (buildsworth on #4144) | Built in: credentialish `--config` keys are deferred to post-clone application with `--add` (preserving repeated-key semantics, another #4144 round); proxy test asserts no leakage. |
| 7 | Reused checkouts may carry persisted canonical transport config that a mirror fetch would read (the reason #4153 excluded them entirely) | Built in, narrowly: tier B skips the mirror when local config contains bearer-credential carriers (`http.extraHeader` incl. URL-scoped, `credential.*`, `url.*.insteadOf`), instead of excluding all reused checkouts. |
| 8 | Commit-presence probes on partial clones can trigger lazy fetches from origin, escaping the mirror attempt's bounds (buildsworth on #4144) | Built in: `GIT_NO_LAZY_FETCH=1` on probes (tier B). |
| 9 | `--tags`/tag-following can import mutable refs from a lagged mirror (buildsworth on #4144) | Handled per tier: tier B's bare-SHA fetch auto-follows no tags from either source (parity by construction); tier A's explicit-refspec fetch *would* auto-follow tags where today's destination-less fetch does not, so it adds `--no-tags`; tier C's clone takes tags from the mirror exactly as a canonical clone takes them from canonical, modulo lag (caveat register). Tag *builds* are ineligible. No flag allowlisting. |
| 10 | Hook-set `GIT_DIR`-style env can redirect Git operations (buildsworth on #4144; #4153 fail-closed on 10 env vars) | Accepted: such env redirects canonical operations equally; mirror operations inherit whatever environment the customer configured. Caveat register, not code. |
| 11 | Unknown/unvetted clone/fetch flags could change mirror transport behavior (the driver of #4153's `planRemoteMirrorCheckout` fail-closed allowlist) | Designed out: flags are passed to real `git clone`/`git fetch` invocations against a customer-controlled host, so there is nothing to vet. The only flag inspection remaining is the credentialish `--config` deferral. |
| 12 | Mirror attempt must not consume the whole checkout budget when the mirror hangs (buildsworth on #4144) | Built in: `http.lowSpeedLimit`/`http.lowSpeedTime` stall detection + no-retry + first-attempt-only, rather than a wall-clock cap that would kill legitimately large transfers. |
| 13 | Agent repo allowlists must cover the mirror URL (#4153 behavior) | Changed deliberately: a non-matching mirror URL disables the mirror instead of refusing the job. Flagged for explicit reviewer sign-off since it differs from #4153's shipped-in-PR semantics. |
| 14 | Bot reviewers will push toward fail-closed handling of every conceivable configuration | This document is the pushback: the trust model section and caveat register exist so that review can converge on "is this caveat acceptable?" instead of "add another guard". PRs should link here from code comments at each accepted-caveat site. |

## Register of caveats (accepted, documented, not defended)

1. **Ref staleness on mirror-sourced clones.** A tier C checkout's
   branch/tag refs reflect the mirror's replication point, which may trail
   canonical by the replication lag. The build's own commit is always
   verified present (else fallback), so the built commit is never stale;
   only auxiliary refs (`origin/main`, tags for `git describe`) can be.
   Customers with lag-sensitive jobs shouldn't opt in, or should pin what
   they need to the build commit.
2. **The mirror URL appears in Git-owned files and output.** `FETCH_HEAD`,
   reflogs, error messages, trace spans. Non-sensitive by definition (see
   trust model).
3. **Ambient credentials follow ambient rules.** Credentials injected via
   hook-exported env (`GIT_CONFIG_*`, `GIT_ASKPASS`, …), system/global
   gitconfig, or OS keychains are consulted by Git for mirror URLs the same
   way they are for any URL. The agent separates only the credentials it
   manages (R8).
4. **`GIT_DIR`-style env redirection.** A hook that points Git at a
   different repository affects mirror and canonical operations alike; the
   agent does not detect or defend against it.
5. **Mirror servers must support SHA fetches.** Without
   `uploadpack.allowAnySHA1InWant` (or reachable-SHA equivalent), tier B
   misses every time and the feature degrades to canonical behavior plus
   one wasted round trip per job. A provider requirement, not an agent
   guard. (Tier A's refresh fetches a branch ref, not a bare SHA, so it is
   unaffected.)
6. **Checkouts carrying canonical transport config skip tier B.** The
   credential-bearing-local-config gate means a pipeline using
   `--config http.extraHeader=…`-style clone flags gets mirror-sourced
   fresh clones (tier C) but canonical delta fetches on reuse — a
   deliberate trade of R8 over tier B coverage.
7. **Canonical still sees regular traffic.** Fallback traffic (lag windows,
   ineligible builds, retries) keeps flowing to the canonical host; the
   mirror reduces canonical load, it does not eliminate it. This is inherent
   to fail-open design.
8. **Shared on-host mirrors and mixed agent versions.** Tier A keeps the
   shared mirror's `origin` canonical precisely so agents without
   remote-mirror support (or jobs without the env var) interoperate on the
   same mirror directory. A mirror directory never encodes whether it was
   last refreshed from the remote mirror.

## Possible follow-ups (explicitly not in this stack)

- Routing the checkout-level delta fetch through the remote mirror even when
  an on-host mirror is configured (today that fetch is usually skipped via
  `git-skip-fetch-existing-commits` or cheap thanks to `--reference`).
- Mirror-first for GitHub PR refs / custom refspecs, if mirror providers
  replicate those namespaces.
- Submodule remote mirrors (would need per-submodule mirror URLs from the
  backend).
- Using the remote mirror for `git lfs` transfers.
- An agent-level kill switch (`--no-git-remote-mirrors`) if operators ask
  for one; until then, absence of the env var is the off switch.
