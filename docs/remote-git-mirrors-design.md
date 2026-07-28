# Remote Git mirrors: design notes

Status: **discussion document**. Nothing here is implemented. This records what
the agent does today, what would have to change to fetch from a Buildkite-supplied
remote mirror with the canonical repository as a fallback, and the hazards we
would hit on the way.

Read [`git-mirror.md`](git-mirror.md) first — it covers the existing on-host
mirror and why `--reference` clones are dangerous.

## 1. What the agent actually does today

All of this lives in `internal/job/`: `checkout.go` (`defaultCheckoutPhase`),
`checkout_fetch.go` (`fetchSource`), `checkout_mirror.go` (`getOrUpdateMirrorDir`),
and `git.go` (the `git*` command wrappers and error classification).

### 1.1 No mirror

1. Optionally `ssh-keyscan` the host of `BUILDKITE_REPO`.
2. Materialise `BUILDKITE_GIT_SSH_KEY` into a temp dir and point `GIT_SSH_COMMAND` at it.
3. If `.git` exists, reconcile `remote.origin.url` with `BUILDKITE_REPO`
   (`updateRemoteURL`). Otherwise `git clone <clone flags> -- <repo> .`.
4. `git clean`, optionally `git lfs install --local`.
5. `git fetch <fetch flags> -- origin <refspec>`, where the refspec is chosen by
   `fetchSource`: a custom `BUILDKITE_REFSPEC`, or `refs/pull/N/{head,merge}`
   (plus the commit SHA when known), or the branch name when `BUILDKITE_COMMIT`
   is `HEAD`, or — the common case — the commit SHA.
6. Commit verification, sparse checkout, `git checkout <commit|FETCH_HEAD>`,
   submodules, LFS fetch, `git clean` again.

### 1.2 With the on-host ("agent") mirror

Before step 3, `getOrUpdateMirrorDir` clones or updates
`$BUILDKITE_GIT_MIRRORS_PATH/<mangled repo URL>` and returns either that
directory or a hardlink snapshot of it. The checkout clone then gets
`--reference <dir>`, plus `--dissociate` unless
`BUILDKITE_GIT_MIRROR_CHECKOUT_MODE=reference`.

Two details in the problem statement need correcting:

- **The checkout never fetches *from* the local mirror.** Both the `git clone`
  and the subsequent `git fetch` still address the canonical URL. The mirror is
  only an object store, handed to git through `.git/objects/info/alternates`.
  So the flow is "refresh local mirror from canonical → clone from canonical
  using the mirror's objects → fetch from canonical (usually transferring
  almost nothing, because the objects are already local)".
- **The mirror is refreshed by ref name, not by commit SHA.** `updateGitMirror`
  fetches the custom refspec, or the PR ref, or the branch — never the commit.
  It does short-circuit on `hasGitCommit(mirrorDir, e.Commit)` before fetching,
  so an exact-SHA *presence check* exists, but there is no exact-SHA *fetch*
  into the mirror. A branch that moved between scheduling and checkout can
  therefore leave the mirror without the commit; today the checkout's own fetch
  against canonical silently covers for that.

The only existing way to avoid the canonical round trip on the checkout's fetch
is `BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS`, which skips the fetch entirely
when the commit is already reachable locally.

Submodules get the same treatment, mirrored per submodule URL from `.gitmodules`.

## 2. Existing mechanisms worth knowing about

Several of these are close relatives of what is being proposed:

- **Mirror on a network share.** `git-mirror.md` documents agents on different
  machines sharing one mirror directory over a network file share. That is
  already a "remote mirror", just at the filesystem layer rather than the git
  transport layer, and it is why the mirror locks are file-based.
- **`--git-mirrors-skip-update`.** Uses a pre-populated mirror without
  contacting any upstream, and — notably — **already implements the
  mirror-with-canonical-fallback shape**: if the expected mirror directory is
  absent it sets `mirrorDir = ""` and the checkout degrades to a plain clone
  from canonical instead of failing.
- **`--git-skip-fetch-existing-commits`.** Skips the fetch when the commit is
  already present, which is what makes a pre-warmed mirror actually avoid
  network traffic.
- **Mirror snapshots.** When clean checkout is on and the command phase is
  included, the checkout references a hardlink `--mirror` clone of the mirror
  rather than the mirror itself, so mirror maintenance can't corrupt the
  checkout. Snapshots are deleted at teardown.
- **GitHub App code-access credential helper.** `BUILDKITE_USE_GITHUB_APP_GIT_CREDENTIALS=true`
  makes the executor register `buildkite-agent git-credentials-helper` as a
  **global** `credential.helper` with `credential.useHttpPath=true`, and rewrite
  `git@github.com:` to `https://github.com/` via a global `insteadOf`. The
  helper calls `GenerateGithubCodeAccessToken(repoURL, jobID)`.
- **`--allowed-repositories`.** An agent-side regex allowlist applied to
  `job.Env["BUILDKITE_REPO"]` in `validateConfigAllowlists`.
- **Signed pipelines.** `verifyJob` verifies the step against
  `Job.Env["BUILDKITE_REPO"]`; the `repository_url` signed field is exactly that
  value. Any signed field the agent doesn't recognise is a hard failure
  ("mystery signed field").
- **Checkout override modes.** `env/protected.go` splits checkout-related env
  into `protectedEnv` (agent-authoritative; the mirror-infra vars live here) and
  `checkoutOverrideScope` (governed by `--checkout-override-mode`). Any new
  mirror var has to be placed deliberately in this scheme.
- **Checkout retry and error classification.** `defaultCheckoutPhase` runs under
  a 6-attempt retrier; `gitError.Type` decides whether a failure wipes the
  checkout directory. `git.*` trace spans already exist for clone, fetch, mirror
  update and snapshot.
- **`BUILDKITE_REPO_MIRROR`.** Already exported to hooks, and already means "the
  local mirror directory". Do not reuse this name for a remote mirror URL.
- **Custom `checkout` hooks and plugins** replace all of the above wholesale.

## 3. Is there an alternate repository URL from the Agent API today?

No. `api.Job` carries `Env map[string]string`, `Step`, tokens and timing fields —
there is no repository field at all, let alone a second one. The repository URL
reaches the executor purely as the backend-supplied `BUILDKITE_REPO` env var
(`ExecutorConfig.Repository`), and `createEnvironment` copies the job env
verbatim, so the backend can already set arbitrary `BUILDKITE_*` vars without an
agent change.

`BUILDKITE_PULL_REQUEST_REPO` is set by the backend for fork PRs, but the agent
never reads it (it appears only in the agent's own `.buildkite/pipeline.yml`).

`updateRemoteURL` contains a branch for repos with multiple `remote.origin.url`
values, but that is defensive handling of user-configured remotes, not a
fallback mechanism — git only ever fetches from the first URL.

So: no alternate-URL support, and no place to put one without new config.

## 4. What would need to change

### 4.1 Transport of the new attributes

Nothing is strictly required in `api/jobs.go` if the mirror is delivered as job
env. If we want structured attributes instead, add fields to `api.Job`; older
agents ignore unknown JSON fields, which is the compatibility behaviour we want.

What must *not* happen is signing a new step field for this: older agents reject
jobs carrying signed fields they don't know about.

### 4.2 Config plumbing

New `ExecutorConfig` fields in `internal/job/config.go`, e.g. a mirror URL and a
mode selector (off / fetch-only / mirror-update / both). Each one needs, per the
comment at the top of that file: a `cli` field and flag in
`clicommand/bootstrap.go`, a flag definition in `clicommand/global.go`, an entry
in `AgentStartConfig` and `agent.AgentConfiguration` if it is also
agent-configurable, wiring in `createEnvironment`, and a decision in
`env/protected.go`. `clicommand/config_completeness_test.go` enforces most of
this.

Recommended protection: put the mirror URL in `protectedEnv` **without**
`mutableFromWithinJob`. Because `protectedEnv`'s job-env enforcement is implicit
(it happens by `createEnvironment` overriding values the agent sets), a
backend-supplied value still lands, while hooks, plugins, the Job API and
secrets are blocked from injecting one.

### 4.3 The fetch path — the core change

`fetchSource` in `checkout_fetch.go` currently hardcodes `Repository: "origin"`
at four call sites, and `gitFetchWithFallback` reads `remote.origin.fetch`.
`gitFetchArgs.Repository` is passed through after `--`, so a bare URL already
works as a fetch source with no change to `gitFetch` itself.

The shape of the change: a helper that attempts the fetch against the mirror URL
with retries disabled, classifies the failure, and on a "mirror doesn't have it"
classification repeats the fetch against `origin`. Initially scope it to the
`refspecCommit` case (and the commit half of the PR case) — see §5.4 for why
that is a correctness requirement rather than a simplification.

`gitFetchWithFallback`'s "fetch all heads and tags" recovery must stay pointed at
canonical only.

### 4.4 Error classification — required, and not the obvious string

We measured this on git 2.43 against both a local path and `git://`:

| situation | stderr | exit |
| --- | --- | --- |
| remote lacks a full 40-hex SHA | `fatal: remote error: upload-pack: not our ref <sha>` | 128 |
| remote lacks a named ref | `fatal: couldn't find remote ref <ref>` | 128 |
| short SHA | `fatal: couldn't find remote ref <short>` | 128 |

The agent's existing smelt list in `gitFetch` covers `fatal: bad object` and
`fatal: [Cc]ouldn't find remote ref` — **neither matches the exact-SHA miss**.
Today that case falls through to the generic exit-128 branch, becomes
`gitErrorFetchRetryClean`, and makes the retrier delete the entire checkout
directory and start over. In other words, the single most likely mirror outcome
is currently handled in the most expensive way possible.

So we need a new smelt string (`not our ref`, plus
`Server does not allow request for unadvertised object` for servers that refuse
unadvertised wants) and a new `gitError` type that means "this remote doesn't
have it, try the other one" and specifically does *not* wipe the checkout.

Worth noting for the backend conversation: under protocol v2, fetching a SHA
that is *reachable* from an advertised ref succeeds regardless of
`uploadpack.allowAnySHA1InWant` / `allowReachableSHA1InWant` (verified). Those
knobs only matter for unreachable objects.

### 4.5 The clone path

A fresh checkout is where most of the bytes move, so a mirror that only serves
`git fetch` leaves the biggest win on the table. Three options:

1. **Keep cloning from canonical, route only fetches to the mirror.** Smallest
   change, no new failure modes in the clone, but only helps incremental
   checkouts (and when local mirrors are enabled, `--reference` already handles
   those).
2. **Clone from the mirror, then rewrite `origin` to canonical.** Gets the bulk
   transfer, but see the `updateRemoteURL` churn and stale-`origin/<branch>`
   hazards in §5.
3. **Replace clone with `git init` + configured remotes + fetch.** Most control,
   but it discards user-supplied `BUILDKITE_GIT_CLONE_FLAGS` semantics
   (`--depth`, `--filter`, `--sparse`, `--reference`), which is a compatibility
   break we should not take casually.

Option 1 for a first cut; revisit 2 once mirror hit rates are measurable.

### 4.6 The "both mirrors" flow

For "refresh local mirror from remote mirror, fall back to canonical", the fetch
inside `updateGitMirror` takes the mirror URL as `Repository` with an `origin`
fallback. Three constraints:

- Keep keying the mirror directory on `dirForRepository(<canonical URL>)`. Keying
  on the mirror URL would fragment the shared cache and churn it whenever the
  mirror URL changes.
- Do **not** `git remote set-url origin <mirrorURL>` in the mirror. `updateRemoteURL`
  compares `remote.origin.url` against `e.Repository` and, on a mismatch, logs
  "the repository has been renamed" and runs `git fsck` + `git gc` on the shared
  mirror. With a per-job or rotating mirror URL that happens on **every job**.
- Fetch the commit SHA into the mirror in addition to the branch, so that a
  mirror hit is complete and detectable rather than depending on branch-ref
  freshness.

### 4.7 Auth, keyscan, allowlists, observability

- `addRepositoryHostToSSHKnownHosts` is called for the canonical repo (and
  submodules) only; an SSH mirror host needs the same treatment.
- The GitHub App credential helper is registered globally. Left as-is, git will
  invoke it for the mirror host too and ask the Buildkite API to mint a GitHub
  token for a non-GitHub URL. It needs host-scoping (`credential.<url>.helper`).
- `validateConfigAllowlists` must cover the mirror URL, or `--allowed-repositories`
  is trivially bypassed by a mirror URL.
- Add mirror source / hit / miss attributes to the `git.fetch` and
  `git.mirror.*` spans. Without hit-rate telemetry we cannot tell a working
  mirror from one that misses 100% of the time and silently falls back.
- Gate the whole thing behind an entry in `EXPERIMENTS.md` for rollout.

### 4.8 Tests

`internal/job/githttptest` already serves multiple repositories from one HTTP
test server, so mirror-hit, mirror-miss and mirror-stale are all directly
expressible. Be aware that the integration tests assert exact git argv via
`ExpectAll`, so any change to the fetch command shape touches most expectations
in `checkout_integration_test.go` and `checkout_git_mirrors_integration_test.go`.

## 5. Challenges and hazards

### 5.1 Retry budgets multiply

`gitFetch` with `Retry: true` is 10 attempts over ~2m17s, and the whole checkout
sits inside a 6-attempt retrier. Mirror attempts must be non-retrying and
fail-fast, otherwise a lagging mirror adds minutes per attempt. Conversely the
PR-head retry (GitHub creates `refs/pull/N/head` asynchronously) only makes sense
against canonical.

### 5.2 A mirror miss must not wipe the checkout

See §4.4. Until the new error type exists, every mirror miss costs a full
re-clone.

### 5.3 Credentials persisted on disk

Both the checkout directory and the mirror directory are reused across jobs, and
the mirror is shared between agents on the host (sometimes across hosts). A
credentialed mirror URL written into `remote.*.url` leaks into later, unrelated
jobs. Pass mirror credentials via the credential helper or per-invocation `-c` /
`GIT_CONFIG_*` env instead of persisted remote config. Also check the value is
covered by redaction — the default `--redacted-vars` globs match names like
`*_TOKEN`, so a var named for the mirror URL would not be redacted from logs.

### 5.4 Stale refs are the real correctness risk

A mirror cannot lie about the *content* of a SHA — git is content-addressed. It
can absolutely lie about where a *name* points. So:

- Fetching an exact SHA from a mirror either succeeds correctly or 404s.
- Fetching a branch, tag glob, or PR ref from a mirror can silently return the
  wrong commit, and the job proceeds and passes against the wrong code.

This should be written down as an invariant, not left as "perhaps": **only exact
40-hex commit SHAs may be fetched from the mirror.** Two concrete traps that
follow from it:

- `gitFetchWithFallback`'s "fetch all heads and tags" recovery would *succeed*
  against a stale mirror, then `git checkout <sha>` fails, then the retrier wipes
  the checkout — a confusing failure a long way from its cause.
- `checkCommitOnBranch` (used by `BUILDKITE_GIT_COMMIT_VERIFICATION`) runs
  `git merge-base --is-ancestor <commit> <branch>`, where `<branch>` resolves to
  `refs/remotes/origin/<branch>` populated by the clone. If the clone came from a
  stale mirror, that ref is behind, the ancestry check legitimately fails, and in
  `strict` mode **the job fails** with a security-flavoured error. Its
  `git fetch --deepen=50` / `--unshallow` recovery also uses the implicit
  `origin`.

### 5.5 Trust boundary

The mirror URL would arrive as an unsigned env var, outside the signed-step
guarantee (`repository_url` covers `BUILDKITE_REPO` only). SHA-only fetching
keeps this safe by construction. Anything beyond that makes the mirror URL
trusted input that can change what code a signed pipeline runs.

### 5.6 Partial clone and promisor remotes

`git clone --filter=...` writes `remote.origin.promisor=true` and
`remote.origin.partialclonefilter`, and all later lazy object fetches go to that
promisor remote — git has no promisor fallback. Since sparse checkout
auto-adds `--filter=blob:none`, a sparse checkout cloned from a mirror will send
every lazy blob fetch to the mirror forever, and a missing blob surfaces as an
error deep inside some unrelated later command. Sparse checkout plus remote
mirror is the riskiest combination in the matrix.

### 5.7 Shallow clones

`--depth` in `BUILDKITE_GIT_CLONE_FLAGS` computes a `.git/shallow` boundary
against whichever source served the clone. Mixing sources across a shallow
boundary is asking for "object not found" during deepen/unshallow.

### 5.8 Submodules

The backend knows the main repository; `.gitmodules` URLs are discovered at
checkout time. Either scope remote mirroring to the main repo, or define a
URL-rewriting scheme (effectively `url.<mirror>.insteadOf <canonical-prefix>`) so
submodule URLs can be mapped too. The rewrite approach is more general but
removes per-URL fallback control.

### 5.9 Git LFS

`git lfs fetch` resolves its endpoint from the remote URL, not from where objects
came from. A mirror that doesn't proxy LFS simply doesn't help LFS-heavy repos;
worse, if `origin` ends up pointing at a mirror without an LFS endpoint, LFS
breaks outright.

### 5.10 Everything else that can bypass it

Custom `checkout` hooks and plugins replace the default checkout entirely, so
this can never be a policy mechanism — only an optimisation. `BUILDKITE_REPO`
itself is `mutableFromWithinJob`, so a hook can change the canonical URL after
the mirror URL was chosen; the two can disagree.

### 5.11 Interaction matrix to define explicitly

`--git-mirrors-skip-update` (don't touch any upstream) combined with a remote
mirror; `--git-skip-fetch-existing-commits`; `reference` vs `dissociate` checkout
modes; split checkout/command phases in agent-stack-k8s (env must reach the
checkout container, and the snapshot-teardown caveat in `snapshotMirror` already
applies).

## 6. Strawman implementation plan

This is deliberately a strawman: the slicing and the defaults are there to be
argued with. It is ordered so that each slice is independently shippable and
independently revertible, and so that the risky parts come last, after we can
measure whether they are worth doing.

### 6.0 Invariants the plan is built on

These are the load-bearing decisions. If any of them changes, most of the plan
changes with it.

1. **The mirror is only ever asked for exact 40-hex commit SHAs.** Everything
   name-based — branches, tags, `refs/pull/*`, custom refspecs, the
   "all heads and tags" recovery — goes to canonical. See §5.4.
2. **Canonical stays `origin`, forever.** The mirror is never a named remote and
   never persisted into any `.git/config`. It is a URL passed as an argument to a
   single `git fetch`, with any credentials supplied per-invocation.
3. **Fail open, always.** Any mirror failure — miss, timeout, DNS, auth, 500 —
   falls back to canonical. No job ever fails because a mirror is unhealthy.
   There is no "required" mode in v1.
4. **Off unless the backend opts the job in**, and additionally gated by an
   agent-side experiment for the first releases.
5. **Telemetry ships with the behaviour, not after it.** A mirror that misses
   100% of the time and silently falls back looks identical to a working one
   from the outside.

### 6.1 Slice 1 — fetch error classification (no new config, ships alone)

Prerequisite for everything else, and worth shipping on its own merits: today an
exact-SHA fetch miss against *canonical* (a force-push race, say) is classified
`gitErrorFetchRetryClean` and deletes the whole checkout directory, six times
over. Wiping the checkout cannot possibly help when the remote doesn't have the
object.

- `internal/job/git.go`: add smelt strings for `not our ref` and
  `Server does not allow request for unadvertised object`; add
  `gitErrorFetchRefNotOnRemote`.
- `internal/job/checkout.go`: add that type to the retry switch so it does *not*
  wipe the checkout directory.
- Tests in `git_test.go`, plus an integration test using `githttptest` with a
  repo that lacks the requested SHA.

Exit criteria: a canonical-side SHA miss no longer re-clones, and the failure
message names the actual problem.

### 6.2 Slice 2 — inert config plumbing

- Env: `BUILDKITE_GIT_REMOTE_MIRROR_URL` and `BUILDKITE_GIT_REMOTE_MIRROR_MODE`
  (`off` | `fetch` | `mirror-update` | `both`, default `off`). Note
  `BUILDKITE_REPO_MIRROR` is already taken — it is the local mirror *directory*.
- `internal/job/config.go`: `RemoteMirrorURL`, `RemoteMirrorMode`.
- Flags in `clicommand/global.go`; fields in `clicommand/bootstrap.go`,
  `clicommand/agent_start.go`, `agent.AgentConfiguration`; wiring in
  `createEnvironment`. `clicommand/config_completeness_test.go` keeps this honest.
- `env/protected.go`: both vars into `protectedEnv` **without**
  `mutableFromWithinJob`. Because that map's job-env enforcement is implicit (it
  works by `createEnvironment` overriding what the agent sets), a
  backend-supplied value still lands, while hooks, plugins, the Job API and
  secrets are all blocked from injecting one. Update the map's doc comment and
  the disjointness test.
- Agent-side kill switch: an `--no-remote-git-mirror` style flag that forces
  mode `off` regardless of what the backend sends.
- Validation, at setup rather than at fetch time: URL parses, scheme is in an
  allowlist, mode is a known value, and the mirror URL is included in
  `validateConfigAllowlists` so `--allowed-repositories` can't be sidestepped.
  Reject the mirror outright when `BUILDKITE_REPO` is empty.
- Redaction: if the URL carries userinfo, register the credential with
  `e.redactors.Add(...)` (as `startJobAPI` does for the agent token) and log the
  URL only through `redact.URLCredentials`. The default `--redacted-vars` globs
  match names like `*_TOKEN`, so a `*_URL` name gets no automatic protection.
- `EXPERIMENTS.md`: add `remote-git-mirror`.

Exit criteria: the agent accepts, validates, redacts and traces the config
(`git.remote_mirror.configured`, `git.remote_mirror.mode`) and changes no
behaviour.

### 6.3 Slice 3 — mirror-assisted fetch (the core)

New file `internal/job/checkout_remote_mirror.go`, holding the whole policy in
one place so the decision is auditable:

```go
// remoteMirrorFetchURL returns the mirror URL to try for this fetch, or "" to
// go straight to canonical. This is where invariant 1 is enforced.
func (e *Executor) remoteMirrorFetchURL(kind refspecKind) string

// fetchViaRemoteMirror fetches refspec from the mirror, and on a
// mirror-doesn't-have-it or mirror-is-unhealthy classification retries against
// origin. Returns which source served the fetch, for tracing.
func (e *Executor) fetchViaRemoteMirror(ctx context.Context, gitFetchFlags, refspec string) (source string, err error)
```

- `checkout_fetch.go`: route the `refspecCommit` case through the helper. Leave
  `refspecCustom`, `refspecBranch` and the PR-ref fetches on canonical. The
  commit component of the PR case can come later, once the simple case is proven.
- Mirror attempts use `Retry: false` and a short per-attempt deadline; only the
  canonical fallback keeps today's retry budget. Otherwise a lagging mirror adds
  minutes to every attempt, multiplied by the outer 6-attempt checkout retrier.
- **Commit verification interaction:** a mirror fetch of a bare SHA gives us the
  object but does not advance `refs/remotes/origin/<branch>`, which
  `checkCommitOnBranch` compares against. When
  `BUILDKITE_GIT_COMMIT_VERIFICATION` is set, either keep fetching the branch
  from canonical as well (cheap, since the objects are already local) or skip the
  mirror for that job. Pick one explicitly rather than discovering it in
  production.
- Spans: `git.remote_mirror.attempted`, `.result` (`hit` / `miss` / `error`),
  `.duration_ms`, and the canonical fallback duration alongside.

Exit criteria: with the experiment on and a healthy mirror, the common
"build this SHA" checkout fetches from the mirror; with a stale, broken or
absent mirror, the job behaves exactly as it does today, only slower by one
failed round trip.

### 6.4 Slice 4 — feed the on-host mirror from the remote mirror

The `both` mode, and the one that actually saves bulk transfer on hosts that
have a local mirror, because the expensive `git clone --mirror` is the thing
being amortised.

- `checkout_mirror.go`: `updateGitMirror`'s fetch takes the mirror URL with an
  `origin` fallback; the initial `git clone --mirror` may source from the mirror.
- Keep keying the mirror directory on `dirForRepository(<canonical URL>)`.
- Never `git remote set-url origin <mirrorURL>` in the mirror — `updateRemoteURL`
  would then see a changed URL and run `git fsck` + `git gc` on the *shared*
  mirror on every job (§4.6).
- While here: fetch the commit SHA into the mirror in addition to the branch, so
  a hit is complete and detectable rather than dependent on branch-ref freshness.

Exit criteria: on a host with local mirrors enabled, a cold mirror populates
from the remote mirror and a job whose commit the mirror lacks still succeeds.

### 6.5 Slice 5 — clone from the mirror (deferred, decide with data)

This is the biggest win for ephemeral agents with no local mirror, and the
riskiest change. Do not start it before slices 3 and 4 have produced hit-rate
numbers. When we do:

- Prefer "clone from mirror, then `git remote set-url origin <canonical>`" over
  rewriting clone as `git init` + fetch, because the latter discards user
  `BUILDKITE_GIT_CLONE_FLAGS` semantics (`--depth`, `--filter`, `--sparse`,
  `--reference`).
- Treat every remote-tracking ref created by a mirror-sourced clone as
  untrusted: nothing may rely on `refs/remotes/origin/*` unless canonical
  populated it. That specifically means commit verification must fetch the branch
  from canonical (§5.4).
- Refuse to clone from the mirror when a partial-clone filter is in play,
  including the `--filter=blob:none` that sparse checkout adds automatically —
  the clone source becomes the promisor remote for every later lazy blob fetch,
  with no fallback (§5.6). Same for `--depth` (§5.7).

### 6.6 Cross-cutting: the auth seam

Auth details are TBA, so slice 2 should land the *seam* and nothing more: a
single function that runs immediately before a mirror fetch and returns
per-invocation git config (`-c credential.<mirror-url>.helper=...`, or
`GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_n` env), so no credential is ever written to
disk in a directory reused by later jobs (§5.3).

Two things must be fixed whatever the scheme turns out to be:

- Host-scope the GitHub App credential helper. It is registered as a global
  `credential.helper` with `credential.useHttpPath=true`, so as things stand git
  will invoke it for the mirror host and ask the Buildkite API to mint a GitHub
  token for a non-GitHub URL.
- Keyscan the mirror host in `addRepositoryHostToSSHKnownHosts` if SSH mirror
  URLs are in scope.

If the mirror needs a Buildkite-minted credential, the natural shape is a
sibling of `GenerateGithubCodeAccessToken` plus a `git-credentials-helper`
action, reusing the existing plumbing.

### 6.7 Test plan

`internal/job/githttptest` already serves several repositories from one HTTP test
server, which covers most of what we need:

- mirror has the SHA → mirror serves it, canonical is never contacted;
- mirror lacks the SHA → one failed fetch, then canonical, job succeeds;
- mirror's branch tip is *behind* canonical → still correct, because we only ask
  for SHAs (this is the regression test for invariant 1);
- mirror returns 500 / connection refused / hangs → fallback;
- mirror configured but mode `off`, and mode set with no URL;
- commit verification enabled with a mirror-served fetch.

Expect churn in `checkout_integration_test.go` and
`checkout_git_mirrors_integration_test.go`: they assert exact git argv via
`ExpectAll`, so any change to the fetch command shape touches many expectations.

### 6.8 Rollout

Experiment flag → internal pipelines → a small number of opted-in orgs with
mirror hit rate and fallback rate on a dashboard → default on for jobs where the
backend advertises a mirror → remove the experiment. The gate to advance at each
step is the fallback rate, split by classification: a high `miss` rate means the
staleness contract (§7) isn't holding, whereas a high `error` rate means an
infrastructure problem.

### 6.9 Open decisions in this plan

Strawman answers, offered to be overridden:

| Decision | Strawman |
| --- | --- |
| Mirror used for name-based refspecs? | No, ever. SHA-only. |
| Mirror used for the clone? | Not in v1 (slice 5). |
| One URL or a list? | One in v1; the helper signature takes a list so it can grow. |
| Behaviour when the mirror is unhealthy? | Fail open, always. No required mode. |
| Where does the config live? | `protectedEnv`, backend + agent only. |
| Who owns the fallback decision? | The agent. The backend advertises; it does not command. |
| Local and remote mirror together? | Yes — remote feeds local (slice 4), local feeds the checkout as today. |
| Submodules? | Out of scope in v1; needs URL rewriting (§5.8). |

## 7. Questions for the backend side

- One mirror URL or an ordered list? Per job, per pipeline, per queue, per region?
- Rotation / TTL: can the URL change between jobs, and can it carry a credential?
  (If it can, §4.6 and §5.3 become load-bearing.)
- Auth: does the mirror accept the agent access token, a scoped token minted by
  an agent-API endpoint (as `GenerateGithubCodeAccessToken` does today), or
  ambient cloud identity?
- Does the mirror serve `refs/pull/*`? Does it allow wants for *unreachable*
  objects (`uploadpack.allowAnySHA1InWant`)?
- Does it proxy Git LFS? Does it support partial-clone filters?
- **Staleness contract:** can the backend advertise the mirror only when it has
  confirmed the commit is already present? That single guarantee removes most of
  the fallback machinery above, and turns a mirror miss from "expected, must be
  cheap" into "a bug worth alerting on".
