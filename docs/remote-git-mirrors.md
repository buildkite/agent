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
benefit from it; agents get a fleet-wide off switch.

**R2 — Never slower on a hit.** On the hit path the mirror must not add work.
On a miss it may add at most one bounded, non-retrying round trip, after which
the canonical path proceeds with no discarded work — no re-clone, no wiped
checkout directory.

**R3 — Checkout shape is preserved.** Whatever `checkout:` attributes the
pipeline sets (`depth`, clone/fetch flags, sparse paths, submodules, LFS), the
mirror-served path must produce the same checkout shape as the canonical path,
and must not silently upgrade a shallow or blobless transfer into a full one.

**R4 — Tiered caching with the on-host mirror.** When `--git-mirrors-path` is
configured, the on-host mirror is refreshed from the remote mirror rather than
from canonical. Buildkite hosted agents run this way (the mirror lives on a
cache volume populated by earlier jobs), so this is the single highest-value
increment.

**R5 — Existing checkouts are refreshed, not rebuilt.** A long-lived agent that
already has a checkout of the repository must fetch the new delta from the
mirror into that checkout. It must never discard a usable checkout in order to
use the mirror.

**R6 — Fresh checkouts clone from the mirror.** With no on-host mirror and no
existing checkout, the clone itself should come from the mirror, including CDN
pack offload where the mirror provider supports it.

**R7 — Credentials.** The mirror is assumed to authenticate through
`buildkite-agent git-credentials-helper` against the provider-neutral
repository credential API (`POST jobs/:id/repository_access_token`), already on
`main`. The agent makes a best-effort separation: credentials minted for
canonical should not be presented to the mirror, and vice versa. Best-effort
means "we don't send them ourselves and we clear the obvious inherited
channels", not "we sandbox Git".

**R8 — The built commit is never chosen by the mirror.** The agent checks out
the SHA the backend named. Mirror-derived *names* (branch refs, tags) may end
up in the checkout and may be stale, but nothing the agent does resolves a name
through the mirror to decide what to build. Where the build target is itself a
name (`BUILDKITE_COMMIT=HEAD`, a custom refspec, a PR merge ref), the mirror is
not used at all.

**R9 — Observability.** Every checkout reports whether the mirror was used and
what happened (`hit` / `miss` / `timeout` / `error` / `skipped` plus a skip
reason). Without hit-rate telemetry we cannot distinguish a working mirror from
one that misses every time and silently falls back.

## 3. Trust model, and what we are deliberately not doing

The pipeline customer chooses the canonical host, chooses the mirror host,
writes the hooks, and sets the env. There is no adversarial relationship
between those parties. The mirror URL is a non-secret pipeline setting; it
carries no credentials and does not need redacting.

Consequences, stated up front so review does not relitigate them:

- **We do not isolate the mirror from the canonical in Git configuration.** A
  customer who injects transport configuration by unusual means — ambient
  `GIT_CONFIG_*`, an `init.templateDir`, agent hooks exporting `GIT_*`,
  `--config http.*` in `BUILDKITE_GIT_CLONE_FLAGS` — will have that
  configuration apply to mirror requests too. We clear the two channels that
  cost one flag each (§6.4) and accept the rest.
- **We do not defend against a hostile mirror.** A mirror can only serve objects
  the agent asks for by SHA, or names that the agent never trusts (R8). Beyond
  that, a customer pointing their pipeline at a hostile mirror has already lost.
- **We do not make the checkout byte-identical between hit and miss.** Refs and
  tags a `git clone` copies from the mirror are a snapshot of the mirror, not of
  canonical. §9 records exactly where this shows.
- **This can never be a policy mechanism.** A `checkout` hook or plugin replaces
  the whole checkout phase; `BUILDKITE_REPO` is mutable from within a job.

Where we accept something, the acceptance is written as a code comment at the
point a reader would otherwise file a bug. §9 lists each one and where it goes.

## 4. Ground truth

### 4.1 What the checkout does today

`internal/job/checkout.go` → `defaultCheckoutPhase`:

1. `ssh-keyscan` the canonical host; materialise `BUILDKITE_GIT_SSH_KEY`.
2. If `--git-mirrors-path` is set, `getOrUpdateMirrorDir` clones or updates the
   on-host mirror **from canonical**, and exports `BUILDKITE_REPO_MIRROR`.
3. If `.git` exists, reconcile `remote.origin.url`; otherwise
   `git clone <flags> -- <canonical> .`, adding `--reference <on-host mirror>`
   (and `--dissociate` in `dissociate` mode) when there is one.
4. `git clean`, optional `git lfs install --local`.
5. `fetchSource` → `git fetch <flags> -- origin <refspec>`, where the refspec is
   the custom refspec, or `refs/pull/N/{head,merge}`, or the branch (when
   `BUILDKITE_COMMIT=HEAD`), or — the common case — the commit SHA.
6. Commit verification, sparse setup, `git checkout`, submodules, LFS, clean.

Two things about the on-host mirror are easy to get wrong:

- **The checkout never fetches *from* the on-host mirror.** Clone and fetch both
  address canonical. The mirror is only an object store, reached through
  `.git/objects/info/alternates`, plus a negotiation aid.
- **The on-host mirror is refreshed by ref name, not by SHA**
  (`updateGitMirror` fetches the branch / PR ref / custom refspec). It
  short-circuits on `hasGitCommit(mirrorDir, commit)`, so an exact-SHA
  *presence check* exists, but there is no exact-SHA *fetch*.

The whole checkout runs under a 6-attempt retrier, and `gitError.Type` decides
whether a failure wipes the checkout directory before retrying.

### 4.2 Measured Git behaviour

Everything below was measured on git 2.43.0 while writing this document. These
are the facts the plan rests on.

| # | Behaviour | Result |
| --- | --- | --- |
| G1 | `git fetch <url> <full-sha>` for a SHA reachable from an advertised ref, protocol v2, default server config | succeeds — `uploadpack.allowAnySHA1InWant` is **not** required |
| G2 | Same under protocol v0 | fails: `Server does not allow request for unadvertised object`, exit 1 |
| G3 | `git fetch <url> <full-sha>` for a SHA the server does not have | `fatal: remote error: upload-pack: not our ref <sha>`, exit 128 |
| G4 | `git clone --reference <repo>` where the objects are reachable only from a non-standard ref namespace in the reference repo | transfers **0** objects — negotiation uses every ref of the alternate, not just `refs/heads/*` |
| G5 | `git fetch <url> "+<sha>:refs/some/ns/name"` into a bare repo | succeeds, writes the ref, pins the objects |
| G6 | `git clone <mirror>` → `git remote set-url origin <canonical>` → `git fetch origin <sha>` → `git checkout <sha>` | works for full, `--depth=2`, `--filter=blob:none` and `--single-branch` clones |
| G7 | Partial clone from a mirror, then repoint `origin` at canonical, then delete the mirror entirely | lazy blob fetches succeed — `remote.origin.promisor` is keyed on the *remote name*, so repointing the URL retargets every future lazy fetch |
| G8 | `git -c http.extraHeader= …` with an `http.extraHeader` in `.git/config` | header is **not** sent (verified against a recording HTTP server); an empty value resets the header list |
| G9 | `git -c http.extraHeader= clone --config "http.extraHeader=…"` | header is not sent to the remote, but is still persisted into the new repository's config |
| G10 | `fetch.uriProtocols` (packfile-uri / CDN offload) | defaults to empty; the client must opt in. Upstream's server-side implementation only offloads blobs (`uploadpack.blobPackfileUri`) |

The G6/G7 results are what let this design stay small: `git clone` from the
mirror followed by repointing `origin` reproduces canonical clone semantics
exactly — refspec narrowing, `tagOpt`, shallow boundary, promisor
configuration — because it *is* `git clone`.

G3 matters for a different reason: the agent's current error classification in
`gitFetch` matches `fatal: bad object` and `fatal: [Cc]ouldn't find remote ref`,
and **neither matches an exact-SHA miss**. Today that falls through to the
generic exit-128 branch, becomes `gitErrorFetchRetryClean`, and makes the
retrier delete the whole checkout directory. The single most likely mirror
outcome is currently handled in the most expensive way possible, so
classification has to land before anything else.

## 5. Delivery plan

Five stacked pull requests. Each is independently shippable, independently
revertible, and has a single reviewable idea.

| PR | Idea | Behaviour change | Touches |
| --- | --- | --- | --- |
| 1 | Foundations | none | config, env protection, allowlist, `gitError` |
| 2 | On-host mirror is warmed from the remote mirror | yes | `checkout_mirror.go` |
| 3 | Existing checkout fetches from the remote mirror | yes | `checkout_fetch.go` |
| 4 | Fresh checkout clones from the remote mirror | yes | `checkout.go` |
| 5 | CDN pack offload on mirror transfers | yes | one `-c` flag + config |

**At most one of PRs 2–4 acts per checkout**, whichever comes first: PR 2 when
the on-host mirror is being updated, else PR 3 when a checkout already exists,
else PR 4. §6.1 has the reasoning and the one edge case.

PRs 2, 3 and 4 are independent of each other and can land in any order after
PR 1; the order above is by value. If reviewers would rather have a shorter
stack, PR 1 folds into PR 2 without much loss — it is separate because it lets
the contract (eligibility, env protection, error classification) be settled
without any behaviour change to argue about. PR 5 is optional and should not
be written before PR 4 has produced a measurement.

### PR 1 — Foundations (no behaviour change)

**Goal:** agree the contract before arguing about checkout behaviour.

Contents:

- `ExecutorConfig.GitRemoteMirrorURL`, `--git-remote-mirror-url` on
  `bootstrap`, `BUILDKITE_GIT_REMOTE_MIRROR_URL` in `protectedEnv`. No `env:`
  struct tag: the value must not be refreshable by hooks
  (`clicommand/config_completeness_test.go` enforces most of the plumbing).
- `--git-remote-mirror-enabled` (default `true`, protected) as the fleet-wide
  agent off switch, so an operator can disable the feature without a backend
  change.
- `validateConfigAllowlists` checks the mirror URL against
  `--allowed-repositories` as well as `BUILDKITE_REPO`. Without this the
  allowlist is bypassable by a mirror URL.
- **Canonical binding.** Record the backend-supplied `BUILDKITE_REPO` alongside
  the mirror URL at bootstrap time. Eligibility later requires
  `e.Repository == <bound canonical>`, so a hook that rewrites `BUILDKITE_REPO`
  disables the mirror rather than pointing a mirror issued for repository A at
  a job now building repository B.
- `gitErrorFetchRefNotOnRemote`, smelting `not our ref` and
  `Server does not allow request for unadvertised object`. Add it to the same
  wipe-and-retry arm as `gitErrorFetchBadObject` so **canonical behaviour is
  unchanged**; the mirror paths will branch on the type instead of wiping.
- A `remoteMirror` helper carrying eligibility, the bounded context, the
  per-invocation transport flags (§6.4) and the span attributes. Nothing calls
  it yet.

**Budget.** Every mirror command runs with retries disabled and under a context
that takes a fixed default (30s is a reasonable start) or a minority share of
the remaining checkout deadline when `--git-checkout-timeout` is set, whichever
is smaller. The majority of the deadline stays reserved for the canonical
fallback: a mirror that hangs must not consume the budget the real checkout
needs. Retries belong to canonical only — `gitFetch` with `Retry: true` is 10
attempts over ~2m17s, and the checkout itself already sits inside a 6-attempt
retrier, so a retrying mirror attempt would multiply into minutes per job.

**Eligibility predicate** (one function, one place):

```
mirror URL is non-empty, http(s), and agent-enabled
&& canonical repository still matches the one the mirror was issued for
&& BUILDKITE_COMMIT is a full 40-hex lowercase SHA
&& BUILDKITE_REFSPEC == "" && BUILDKITE_TAG == "" && BUILDKITE_PULL_REQUEST == "false"
&& this is the first checkout attempt
```

**Tests:** eligibility table; env protection; allowlist accept/refuse; error
classification for the three fetch failure strings; `config_completeness_test`.

**Reviewer questions this PR should pre-answer, in comments:** why the config
field has no `env:` tag; why `gitErrorFetchRefNotOnRemote` still wipes on the
canonical path; why the mirror URL is not redacted.

### PR 2 — Warm the on-host mirror from the remote mirror (R4)

**Goal:** tiered caching. Highest value: Buildkite hosted agents keep an on-host
mirror on an attached cache volume, so this alone removes most canonical
traffic without touching the checkout at all.

**Behaviour.** Inside `updateGitMirror`, under the existing update lock, after
the `hasGitCommit(mirrorDir, commit)` short-circuit and before the canonical
refspec fetch:

```
git --git-dir=<mirror> <mirror transport flags, §6.4> \
    fetch --no-tags -- <mirrorURL> "+<commit>:refs/buildkite-agent/remote-mirror/heads/<branch>"
```

Then re-check `hasGitCommit`. Present → return, skipping the canonical fetch
entirely. Absent, or the fetch failed or timed out → fall through to the
existing canonical fetch, unchanged.

No user fetch flags are passed, matching the existing mirror-update fetch: the
on-host mirror is always a full mirror regardless of what the pipeline's
`checkout.depth` says, and the checkout gets its shallowness from its own clone.

**Why an exact SHA into a namespaced ref:**

- Fetching the *SHA* rather than the branch means the mirror cannot be wrong
  (R8), and a lagging mirror produces the clean `not our ref` miss of G3.
- Writing a ref rather than relying on `FETCH_HEAD` keeps the objects reachable,
  which matters twice: unreferenced objects are `gc` fodder, and G4 shows that
  negotiation for the later `--reference` clone reads the alternate's refs.
- The namespace keeps it out of `refs/heads/*`. Writing the build commit to
  `refs/heads/<branch>` would be wrong for a rebuild of an older commit (it
  would move the branch backwards) and would unreference objects that concurrent
  `--reference` checkouts depend on — the corruption mode `git-mirror.md`
  describes. The namespaced ref is force-updated per branch, so its churn
  profile matches `refs/heads/<branch>` without ever touching it.
- One ref per branch, not per commit: a commit's whole ancestry is reachable
  from it, so a single rolling ref pins the history that matters.

Validate the branch name with the existing `gitCheckRefFormat` and skip the
mirror if it is not a usable ref component.

**Measured, end to end.** On-host mirror lagging by two commits behind
canonical, then `git clone --reference <on-host mirror> -- <canonical> .`:

| On-host mirror state | Objects canonical had to send |
| --- | --- |
| lagging (today's behaviour) | 7 |
| warmed by an exact-SHA fetch from the remote mirror | **0** |

with `refs/heads/main` in the on-host mirror untouched throughout.

**Scope:** main repository only, never submodule mirrors. Honour
`--git-mirrors-skip-update` (it means "contact no upstream"). Do **not**
`git remote set-url origin <mirrorURL>` — `updateRemoteURL` would see the URL
change and run `fsck` + `gc` on the shared mirror on every job.

**Tests:** hit (canonical never contacted), miss (falls through, canonical
fetch happens, `refs/heads/*` correct), timeout, mirror-refuses-auth,
`skip-update` interaction, submodule mirrors untouched, `refs/heads/*` never
written by the mirror path.

### PR 3 — Refresh an existing checkout from the remote mirror (R5)

**Goal:** the long-lived agent that already has the repository and just needs
today's delta. Applies only when there is no on-host mirror.

**Behaviour.** In `fetchSource`, for the `refspecCommit` case only, when the
checkout already exists and the job is eligible: fetch the commit from the
mirror first, with retries off and the transport flags of §6.4. If the commit
is then present locally, skip the canonical fetch. Otherwise fetch from
canonical as today. A miss leaves the checkout exactly as it was — nothing is
discarded, so R2 holds by construction.

The mirror fetch carries the pipeline's `BUILDKITE_GIT_FETCH_FLAGS` unchanged,
which is what keeps R3: a pipeline with `checkout.depth` gets `--depth=N` on the
mirror fetch too, so the mirror serves the same shallow transfer canonical
would have.

This is deliberately the plain `git fetch -- <mirrorURL> <sha>`, not an isolated
sub-repository. The checkout's `.git/config` may already hold canonical
transport configuration; §6.4 clears the two channels worth clearing, and §9
records the rest as accepted.

**Tests:** existing checkout advances via a mirror hit with canonical
unreachable; miss falls back and leaves local refs intact; a checkout that is a
partial clone still resolves lazily against canonical afterwards.

### PR 4 — Clone a fresh checkout from the remote mirror (R6)

**Goal:** the cold agent. No on-host mirror, no existing checkout, so the clone
is where all the bytes are.

**Behaviour.** Replace the canonical clone with:

```
git -c <mirror transport flags> clone <the same clone flags> -- <mirrorURL> .
git remote set-url origin <canonical>
```

then continue into the existing `git clean` / `fetchSource` / `checkout` flow
untouched. `fetchSource`'s `git fetch origin <sha>` transfers nothing when the
mirror had the commit and transfers the delta when it did not.

**This is the design decision that keeps the PR small.** Reconstructing a clone
as `git init` + `git remote add` + `git fetch` — the shape of
[#4153](https://github.com/buildkite/agent/pull/4153) — means hand-reproducing
everything `git clone` does with its flags: `--config` (additive, and
overridden for clone-owned keys), `--single-branch` narrowing
`remote.origin.fetch`, `--depth` implying `--single-branch`, `--no-tags`
writing `remote.origin.tagOpt`, and promisor configuration. Each of those was a
separate blocking review round on #4144. Using `git clone` makes all of them
correct by construction (G6), and G7 shows the promisor case resolves itself:
`remote.origin.promisor` names the *remote*, so repointing the URL retargets
every future lazy object fetch at canonical, verified even with the mirror
deleted.

**Failure handling, in order of how likely it is:**

| Outcome | Response |
| --- | --- |
| Clone succeeds, commit present | done — the common hit |
| Clone succeeds, commit absent (mirror lagging) | keep the clone, let `fetchSource` fetch the delta from canonical. Strictly better than re-cloning: the mirror already supplied nearly everything |
| Clone fails (mirror down, auth, 404, timeout) | remove whatever the clone left behind, clone from canonical |

Only the third case discards work, and only work the mirror attempt itself
created.

Shallow, blobless, sparse and single-branch (R3) need no special handling: the
same `BUILDKITE_GIT_CLONE_FLAGS` are passed to the mirror clone, verified in G6.
`--sparse` and the auto-added `--filter=blob:none` are unchanged.

**Tests:** hit with canonical unreachable; lagging mirror completed from
canonical; mirror down falls back to a canonical clone; `--depth`, `--filter`,
`--single-branch`, `--no-tags`, sparse each produce the same `.git/config`,
`remote.origin.fetch`, `remote.origin.tagOpt` and shallow state as a canonical
clone; a mirror-cloned partial clone still resolves lazily after the mirror is
removed.

### PR 5 — CDN pack offload (optional, R6)

For a cold full clone, the remaining win is having the mirror hand back CDN
URIs instead of streaming the pack. Two mechanisms exist and both are one
client flag:

- **packfile-uri**: `-c fetch.uriProtocols=https`. Protocol v2 only; the client
  must opt in (G10). Upstream's server side only offloads blobs above a
  threshold (`uploadpack.blobPackfileUri`), so how much this moves depends
  entirely on the mirror provider's implementation.
- **bundle-uri**: `-c transfer.bundleURI=true` (git ≥ 2.38). Seeds the clone
  from bundle files, typically CDN-hosted, and is the mechanism most likely to
  shift the *bulk* of a full clone.

Apply either only to mirror-addressed commands, restricted to `https` URIs.
Ship this after PR 4 with a measurement, not before: which of the two to use is
a question for the mirror provider (§10), and neither is worth carrying without
a number showing it helps.

## 6. Design decisions

### 6.1 The mirror is consulted at most once per checkout

Two mirror round trips in one checkout would violate R2, and "which code path
ran?" is the first thing a reviewer or an on-call engineer asks. So the executor
carries a single "mirror already attempted" flag and the first eligible owner
claims it, in this order: PR 2 (on-host mirror update), then PR 3 (existing
checkout), then PR 4 (fresh clone).

Ordering rather than a static rule matters for one real case:
`--git-mirrors-skip-update` with no mirror present on disk makes
`getOrUpdateMirrorDir` return an empty directory and the checkout proceeds as if
on-host mirrors were off. PR 2 never ran, so PR 3 or PR 4 should still get their
turn.

With a warm on-host mirror there is no additional win from also cloning the
checkout from the remote mirror: the canonical clone already transfers zero
objects (PR 2's table). Only the ref advertisement round trip remains.

### 6.2 `git clone` from the mirror, not `git init` + fetch

See PR 4. Summarised: `git clone` is the only thing that reproduces `git clone`
semantics, and `git remote set-url` is the only line needed to hand the result
back to canonical.

### 6.3 Namespaced refs in the on-host mirror

See PR 2. The on-host mirror is shared between jobs and sometimes between
hosts; it must never have a canonical-sourced ref moved backwards by
mirror-sourced data.

### 6.4 Credentials and transport flags (R7)

Every mirror-addressed Git command gets exactly these, per invocation, never
persisted:

```
-c credential.useHttpPath=true
-c credential.helper=              # reset any inherited helper list
-c credential.helper=<self> git-credentials-helper
-c http.extraHeader=               # reset any inherited extra headers
-c protocol.version=2              # exact-SHA fetches need v2 (G1/G2)
```

- Clearing `credential.helper` first means exactly one helper — the agent's — is
  consulted for the mirror, so a customer-configured helper for the canonical
  host (a cloud credential helper, an OS keychain entry, a `store` file) is
  never asked for mirror credentials. The backend deliberately does not
  advertise a global helper when only the mirror needs credentials, precisely
  because a global helper would also intercept the canonical checkout, so a
  per-invocation helper is the only mechanism available.
- `credential.useHttpPath=true` scopes the helper call to the full mirror URL,
  which is what `repository_access_token` keys on.
- `-c http.extraHeader=` resets the header list (G8), so a bearer token a
  customer set for canonical in `.git/config` or in
  `--config http.extraHeader=…` is not presented to the mirror — while still
  being persisted into the checkout for canonical's use (G9). This is one flag
  and it closes the single most-raised concern on #4144.
- `protocol.version=2` is Git's own default from 2.26 onward, but a customer or
  a distribution can pin `protocol.version=0`, under which an exact-SHA fetch
  fails for anything that is not a ref tip (G2). Pinning v2 for mirror commands
  turns that from "the mirror never works and nobody notices" into "the mirror
  works"; if the mirror host cannot speak v2 the attempt simply misses and
  canonical takes over.

Everything else — `GIT_CONFIG_*`, `init.templateDir`, `http.proxy`,
`GIT_SSL_*`, other `http.*` keys, `GIT_SSH_COMMAND` — is inherited. §9-C3.

### 6.5 How this plan answers the review threads on #4144 and #4153

Every blocking thread on the two earlier pull requests, and what happens to it
here. "Dissolved" means the code that caused it no longer exists.

| Thread | Disposition |
| --- | --- |
| `--filter` on the mirror fetch registers the one-shot mirror as a promisor remote | **Dissolved** (PR 4). The promisor is `origin`, whose URL becomes canonical. G7. |
| Reused partial clone: negotiation hides missing blobs; lazy fetch escapes the mirror scope | **Dissolved.** PR 3 fetches into the existing repository whose promisor already is canonical. |
| Local-commit probe triggers a promisor fetch from `origin` before the bounded context | **Dissolved.** No pre-probe; PR 3 fetches, PR 2 re-checks after fetching. |
| `git init` loses `BUILDKITE_GIT_CLONE_FLAGS` semantics (e.g. `--config core.autocrlf=false`) | **Dissolved** (PR 4). |
| Repeated `--config` values are additive under clone but replaced by `git config` | **Dissolved** (PR 4). |
| `--config remote.origin.url=X` survives as a second push destination | **Dissolved** (PR 4): clone owns `remote.origin.*`. |
| `branch.*` config that clone overwrites is preserved by the reconstruction | **Dissolved** (PR 4). |
| `--single-branch` / `--depth` narrowing and `--no-tags` → `remote.origin.tagOpt` not reproduced | **Dissolved** (PR 4). Verified in G6. |
| `--tags` / `--prune-tags` imports mutable mirror tags | **Accepted** (§9-C1). A clone from the mirror copies the mirror's tags; the built commit is still the backend's SHA (R8). |
| Clone-time `http.extraHeader` reaches the mirror | **Fixed** by one flag (§6.4, G8/G9). |
| `GIT_TEMPLATE_DIR` / `init.templateDir` seeds credentials into the mirror request | **Accepted** (§9-C3). Customer-supplied config applying to a customer-chosen mirror. |
| Inherited `GIT_DIR` / `GIT_CONFIG` redirect the mirror sequence | **Largely dissolved:** no `git init` and no bespoke `git config` writes, so there is no separate repository layout to redirect. Residual inheritance is §9-C3. |
| `ls-remote` receives the repository before `--`, allowing option injection | **Dissolved.** No `ls-remote`. Every URL in the new paths is passed after `--`, matching the existing `gitClone`/`gitFetch` wrappers. |
| A hit leaves the checkout without `refs/heads/*`, `refs/remotes/origin/*` or tags, so `git rev-parse origin/main` and `git describe` behave differently | **Dissolved for absence** (PR 4 produces a real clone with all the usual refs), **accepted for staleness** (§9-C1). |
| `.git/FETCH_HEAD` retains the mirror URL after a mirror-served fetch | **Accepted** (§9-C2). |
| A hook rewrites `BUILDKITE_REPO` while the mirror stays bound to the original repository | **Fixed** (PR 1, canonical binding). |
| Credential-helper install ordering around `setUp` and environment hooks | Already resolved on `main` by [#4152](https://github.com/buildkite/agent/pull/4152). |

## 7. Testing

- **Unit** (`internal/job`): eligibility table, error classification, mirror
  transport flag construction.
- **HTTP-backed** (`internal/job` with `internal/job/githttptest`): hit, miss,
  timeout, authenticated mirror via the credential helper, mirror down. This
  package already serves several repositories from one test server, so
  hit/miss/stale scenarios are directly expressible.
- **Differential**: for PR 4, clone the same fixture from canonical and through
  the mirror and assert equal `.git/config`, `remote.origin.fetch`,
  `remote.origin.tagOpt` and shallow state for `--depth=N`,
  `--depth=N --no-single-branch`, `--single-branch`, `--no-tags` and
  `--filter=blob:none`. Assert the mirror path actually ran, so a silent
  fallback cannot make the test pass vacuously.
- **Integration** (`internal/job/integration`): the existing suites assert exact
  git argv through `bintest`'s `ExpectAll`, so any change to command shape
  touches many expectations in `checkout_integration_test.go` and
  `checkout_git_mirrors_integration_test.go`. Budget for that in PRs 2 and 4.
- **Negative**: assert that ineligible targets (HEAD, short SHA, tag, PR,
  custom refspec, retry attempt, hook-rewritten repository) never contact the
  mirror — a mirror URL pointing at a closed port is the cheapest way to say so.

## 8. Rollout

There are three independent controls, which is the right number:

- **Per pipeline**: `clone_mirror_url` is unset by default, so no pipeline is
  affected until someone sets it.
- **Per organisation**: the backend's `PipelineCloneMirror` feature flag gates
  who can set it at all.
- **Per fleet**: `--git-remote-mirror-enabled` (PR 1) lets an agent operator opt
  out without a backend change, which matters for self-hosted agents behind a
  proxy or an egress allowlist that has never seen the mirror host.

No agent experiment flag is proposed on top of that. `EXPERIMENTS.md` is for
behaviour the backend cannot gate, and this one it can.

What to watch after each PR ships, from the R9 span attributes: the ratio of
`hit` to `miss`, the mirror attempt duration distribution (a slow miss is the
thing that hurts), and total checkout duration for pipelines with a mirror
against a comparable pipeline without one. A mirror that misses most of the time
is a backend replication problem, not an agent problem, but the agent is where
it becomes visible.

Every PR is revertible on its own: PRs 2–4 each add one branch on an eligibility
predicate that is false for every pipeline without a mirror URL.

## 9. Caveat register

Each entry names where the acceptance comment goes, so a future reader hits the
explanation before they file the bug.

**C1 — Refs from a mirror clone are the mirror's snapshot.** After PR 4,
`refs/remotes/origin/<branch>` and tags come from the mirror and may lag
canonical by the replication delay. A commit-only fetch from canonical does not
refresh them (measured). The built commit is unaffected (R8) and commit
verification fetches the branch tip from canonical explicitly, so verification
is unaffected too. *Optional cheap fix, decide during PR 4:* add
`+refs/heads/<branch>:refs/remotes/origin/<branch>` to the `fetchSource` fetch
that already runs, costing no extra round trip. `checkCommitOnBranch` already
documents the ref-name hazards that refspec has to respect. *Comment: beside the
mirror clone in `checkout.go`.*

**C2 — `.git/FETCH_HEAD` records the mirror URL** after a mirror-served fetch
(PR 3). The mirror URL is a non-secret pipeline setting, and no
mirror-eligible flow reads `FETCH_HEAD` — only the `HEAD`-commit and PR-refspec
flows do, and both are ineligible. `--no-write-fetch-head` would avoid it but
postdates the oldest Git the agent supports. *Comment: at the PR 3 fetch call.*

**C3 — Ambient and user-supplied transport config applies to mirror requests.**
`GIT_CONFIG_*`, `init.templateDir`, `http.proxy`, `GIT_SSL_*`, `GIT_SSH_COMMAND`
and `http.*` keys other than `extraHeader` are inherited. Deliberate: the
customer configured both hosts (§3). *Comment: next to the transport flags in
§6.4's helper.*

**C4 — On-host mirror `gc` can still unreference objects a checkout depends
on.** Pre-existing, documented in `git-mirror.md`, mitigated by `dissociate`
mode and snapshots. PR 2 adds a namespaced ref that only ever grows the pinned
set, so it does not make this worse. *Comment: in `checkout_mirror.go`.*

**C5 — On a mirror-hit steady state the on-host mirror's `refs/heads/*` stop
advancing,** because the canonical fetch is skipped. The namespaced ref keeps
the built history reachable and negotiation stays effective for the branches
being built; other branches' refs age. Acceptable for a cache. *Comment: in
`checkout_mirror.go`.*

**C6 — Submodules never use the remote mirror.** The backend knows only the
main repository; `.gitmodules` URLs are discovered at checkout time. Submodule
on-host mirrors continue to refresh from their canonical URLs.

**C7 — Git LFS is unaffected**, which is the good outcome: `git lfs` resolves
its endpoint from `remote.origin.url`, which is always canonical. An
LFS-heavy repository simply gets no mirror benefit for its LFS objects.

**C8 — Ineligible builds get no mirror at all**: `BUILDKITE_COMMIT=HEAD`, short
SHAs, tag builds, PR builds, custom refspecs, and every checkout attempt after
the first. Restricting to the first attempt keeps a failing checkout from
re-paying the mirror cost on each retry.

**C9 — Mirror lag is visible as a slower job, not as a failure.** Expected and
bounded, but it is why R9 exists: a mirror missing 100% of the time looks
exactly like no mirror, only slightly slower.

**C10 — Checkout hooks and plugins bypass all of this,** as they bypass the
whole default checkout.

**C11 — Split checkout/command phases** (agent-stack-k8s) need
`BUILDKITE_GIT_REMOTE_MIRROR_URL` to reach the checkout container; the existing
`snapshotMirror` caveat about split phases is unchanged.

**C12 — A job killed between PR 4's clone and its `remote set-url` leaves a
checkout whose `origin` is the mirror.** The next job's existing-checkout path
already reconciles `remote.origin.url` with `BUILDKITE_REPO`, so it self-heals,
at the cost of one confusing "the repository has been renamed" log line. Not
worth a lock or a marker file. *Comment: beside the `remote set-url` in
`checkout.go`.*

## 10. Questions for the backend

- **Staleness contract.** Can the mirror URL be advertised only once the backend
  knows the commit is present? That single guarantee would turn a miss from
  "expected, must be cheap" into "a bug worth alerting on", and would let most
  of the fallback machinery relax.
- Can the mirror URL rotate between jobs of the same pipeline? PR 2 keys the
  on-host mirror directory on the canonical URL specifically so that rotation
  does not fragment the cache, but it is worth confirming.
- Does the mirror serve `refs/pull/*`? That is the precondition for extending
  eligibility to PR builds.
- Does it allow wants for *unreachable* objects
  (`uploadpack.allowAnySHA1InWant`)? Needed for rebuilding a commit that has
  been force-pushed away. G1 covers only the reachable case.
- Does it support partial-clone filters, and does it proxy Git LFS?
- For PR 5: packfile-uri, bundle-uri, both, or neither?
