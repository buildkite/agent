# Native CloneKit support

Status: **implementation plan**. No CloneKit behavior is implemented yet.

This plan is grounded in `origin/main` at
`39626b41d656baebf1e819cfc5ec7fb4b8087135`.

CloneKit is an Origin-specific cold-clone accelerator. It supplies a verified
bare Git object database which the agent can use to negotiate a much smaller
transfer from the repository that remains authoritative. It is not a working
tree archive and it is not a Git transport.

Reported on Cursor's Everysphere repository, the current shell prototype
prepares the initial local clone cache in about 12 seconds, compared with about
570 seconds for a normal clone and 22 seconds for the previous Buildkite
tarball seed. Those numbers are promising, but the exact 12-second measurement
boundary is an open rollout question (§13).

Read [`git-mirror.md`](git-mirror.md) and
[`remote-git-mirrors.md`](remote-git-mirrors.md) first. This document uses:

| Term | Meaning |
| --- | --- |
| **canonical repository** | `BUILDKITE_REPO`; the only source that decides refs and the requested build commit |
| **remote Git mirror** | `BUILDKITE_GIT_REMOTE_MIRROR_URL`; an existing optional Git transport |
| **on-host mirror** | `--git-mirrors-path/<dir>`; a local bare repository shared by jobs |
| **CloneKit repository** | the HTTPS repository URL whose `/clone-kit` endpoint serves the manifest; it is either canonical or the configured Origin remote-mirror repository |
| **seed** | the agent-owned temporary bare repository built from a verified CloneKit pack triplet |
| **checkout** | the job's working tree and its Git directory |

## 1. Goals and requirements

Numbered requirements let the delivery slices and tests point at stable
behavior rather than restating it.

**R1 — Optional, backend-issued, and immutable.** The backend advertises a
CloneKit capability only for an Origin repository known to support it. The
agent never probes `<every-repository>/clone-kit`. Hooks, plugins, secrets,
pipeline env, and ambient process env cannot add or rewrite the capability.

**R2 — Canonical Git remains authoritative.** CloneKit supplies objects only.
The ordinary canonical clone, `fetchSource`, commit verification, checkout,
sparse setup, LFS, submodule, hook, and double-clean behavior remain in charge.
The manifest's `tip_commit` is only a negotiation anchor; it never selects what
the job builds.

**R3 — Cold paths only.** CloneKit may create a missing on-host mirror or seed a
fresh checkout. An existing on-host mirror and a reused checkout perform their
ordinary targeted canonical updates. `BUILDKITE_CLEAN_CHECKOUT=true` removes
the old checkout before site selection and is therefore a fresh-checkout case.

**R4 — One accelerator and one attempt.** Resolve CloneKit once at the start of
the first checkout attempt. If both CloneKit and a remote Git mirror are
present, CloneKit has deterministic precedence and the remote Git mirror is not
contacted. A CloneKit miss or failure falls directly to canonical, never to the
second accelerator. Later outer checkout attempts are canonical-only.

**R5 — Fail open, except cancellation.** A missing manifest, malformed
manifest, auth failure, timeout, stalled transfer, range failure, digest
mismatch, invalid seed, or unsupported local filesystem cleans only state the
CloneKit attempt owns and uses canonical Git in the same first attempt. Parent
context completion—cancellation or the checkout-attempt deadline—stops
CloneKit, performs bounded local repair/cleanup, and returns the parent error
without starting canonical network work.

**R6 — Git never consumes unverified artifacts.** Downloads remain in private
staging. Validate the complete manifest, verify every artifact, and prove the
anchor's reachable object closure before creating a reference that any clone
can see. The prototype's verification/fetch overlap is deliberately not
preserved.

**R7 — No temporary dependency escapes checkout.** A successful checkout or
new on-host mirror contains its own links to the verified pack, index, and
reverse index and has no CloneKit alternate. Split checkout/command phases must
not depend on an executor teardown callback or a seed path from the checkout
container.

**R8 — Process-crash recovery is deterministic.** At every adoption boundary,
objects are available through the intact seed, through the target repository,
or both. The next checkout can finish an interrupted adoption or discard an
incomplete checkout and retry canonically. It never guesses that an arbitrary
alternate or temporary directory belongs to the agent.

**R9 — Cross-platform Go.** No Bash, Python, GNU utilities, `truncate`,
`sha256sum`, or `nproc`. Linux, macOS, and Windows use the same Go state machine.
Filesystems without same-volume hard-link support skip CloneKit before paying
the download cost and retain canonical behavior.

**R10 — Bounded network behavior.** Manifest requests have a total deadline.
All requests have dial, TLS-handshake, response-header, and no-progress bounds.
The multi-GB pack has no fixed total timeout while bytes continue to arrive;
the parent checkout deadline still caps the whole attempt when configured.
CloneKit network operations do not retry or switch delivery URLs.

**R11 — Credentials stay scoped.** Use the existing
`buildkite-agent git-credentials-helper` for the base CloneKit repository URL,
not its `/clone-kit` path. Only the manifest request receives repository
authorization. Pack, index, and reverse-index requests receive no repository
authorization header. No repository credential or presigned URL is logged,
attached to a span, or retained in recovery state.

**R12 — Existing checkout semantics win over coverage.** The first release
skips direct shallow, partial/blobless, and sparse clones, user-supplied
alternates/dissociation/hard-link controls, and separate Git directories with
named telemetry reasons. A missing on-host mirror remains eligible for those
checkout shapes because that mirror is already a full object store in today's
design; its downstream clone still owns the requested shape. These are explicit
compatibility boundaries, not silent fallbacks. §8 explains why passing the same
clone flags is not enough for the direct site.

**R13 — Observable and low-cardinality.** Every default checkout emits one
CloneKit outcome and site, plus a bounded skip/failure-stage vocabulary and
durations. A hit means a verified seed actually accelerated canonical Git and
was adopted successfully, not merely that a manifest returned 200.

**R14 — No shared-state regression.** Creating a missing on-host mirror uses
its existing clone lock, staging directory, canonical-origin rewrite, and
atomic publication. Existing mirrors, submodule mirrors, mirror snapshots, and
mixed-version agents retain their current behavior.

## 2. Current ownership and insertion points

The current default checkout path is:

1. `internal/job/executor.go` installs the repository-provider credential
   helper before setup and refreshes provider-dependent configuration after
   hooks.
2. `internal/job/checkout.go` → `CheckoutPhase` runs pre-checkout hooks, refreshes
   managed credentials, applies clean checkout, creates the checkout directory,
   and enters the outer checkout retrier.
3. `internal/job/checkout.go` → `defaultCheckoutPhase` prepares the SSH key,
   updates an optional on-host mirror, resolves sparse and clone flags, and
   delegates existing-versus-fresh handling to `prepareCheckoutWorkdir`.
4. `internal/job/checkout_workdir.go` → `prepareCheckoutWorkdir` reconciles an
   existing checkout or runs `git clone` with canonical origin and optional
   on-host `--reference`/`--dissociate` flags.
5. `internal/job/checkout_fetch.go` → `fetchSource` owns custom refspecs, GitHub
   PR head/merge refs, branch `HEAD`, and exact-commit fetch behavior.
6. `internal/job/checkout.go` continues with pre-clean, LFS install,
   `fetchSource`, commit verification, sparse setup, exact checkout,
   submodules, LFS materialization, and post-clean.

Relevant supporting ownership:

- `internal/job/checkout_mirror.go` → `getOrUpdateMirrorDir` and
  `updateGitMirror` own on-host locks, missing-mirror creation, targeted update,
  snapshots, canonical-origin repair, dissociation, and alternates.
- `internal/job/checkout_remote_mirror.go` owns the existing remote-mirror
  attempt decision, first-attempt gate, allowlist-compatible behavior,
  timeout conventions, redaction, and telemetry vocabulary.
- `internal/job/checkout_ssh.go` and
  `clicommand/git_credentials_helper.go` own managed Git credential-helper
  setup and the provider-neutral repository access-token request.
- `clicommand/bootstrap.go` maps bootstrap env/flags to
  `job.ExecutorConfig`; `internal/job/config.go` controls which values hooks can
  refresh.
- `agent/job_runner.go` → `createEnvironment` is the boundary that masks
  ambient backend-only values and applies `--allowed-repositories` without
  refusing an otherwise valid job. `agent/run_job.go` emits the resulting
  job-facing warning after `jobLogs` exists.
- `env/protected.go` blocks protected values from hooks, plugins, secrets, and
  the Job API.

CloneKit belongs at two cold acquisition sites, not in `fetchSource`:

```text
                                     ┌────────────────────────────┐
CloneKit manifest + artifacts ──────▶│ verified temporary bare seed│
                                     └─────────────┬──────────────┘
                                                   │ --reference
                     ┌─────────────────────────────┴─────────────────────────┐
                     │                                                       │
                     ▼                                                       ▼
       missing on-host mirror staging                         fresh working-tree clone
       git clone --mirror canonical                            git clone canonical .
                     │                                                       │
                     └──────────── hard-link pack adoption ──────────────────┘
                                                   │
                                                   ▼
                            existing canonical fetch/verify/checkout tail
```

The same seed builder and adoption protocol serve both sites. Site-specific
repository lifecycle remains in `checkout_mirror.go` and
`checkout_workdir.go`.

## 3. Selected architecture and rejected alternatives

### 3.1 Canonical clone with a verified reference seed

For a fresh checkout, prepare the seed before touching the checkout, then run:

```text
git clone <existing clone flags> --reference <seed> -- <canonical> .
```

The seed contains `refs/clone-kit/anchor` at the manifest's `tip_commit`.
Measured packet traces show `git clone --reference <seed>` advertises that
private ref as a protocol `have`. Canonical upload-pack therefore sends only
objects not covered by the verified anchor, while `git clone` still owns normal
worktree, index, ref, tag, shallow, promisor, template, and origin setup.

After adoption, continue through the existing `fetchSource` unmodified. Do not
turn a CloneKit hit into `GitSkipFetchExistingCommits`: the seed tip need not be
the build commit, a normal clone does not request fork PR refs, and custom
refspec/branch/commit behavior belongs to `fetchSource`.

A CloneKit hit still requires canonical to be reachable. Unlike a remote Git
mirror, CloneKit cannot complete a checkout while canonical is offline. Tests
must assert reduced canonical pack bytes, not zero canonical requests.

### 3.2 Hard-link adoption, not `--dissociate` or triplet moves

Do not use unconditional `--dissociate`. Git implements it by repacking all
objects reachable through alternates. Rewriting a multi-GB seed after a
12-second download can erase the intended speed-up and doubles peak disk IO.
Keep it only as a rare recovery path after an adoption failure.

Do not implement the prototype suggestion as three `os.Rename` calls. A Git
pack is usable as a set: moving any one of `pack`, `idx`, or `rev` first leaves
neither the source nor destination with a complete triplet during that crash
window.

Instead, create the seed on the same filesystem as its target and use
`os.Link`:

1. Keep the verified source triplet intact.
2. Hard-link each file into the target's `objects/pack`. A newly linked file is
   validated with `os.SameFile` and size, avoiding another multi-GB read. If a
   destination name already exists and is not the same file, accept it only
   after size and SHA-256 verification.
3. Verify that all three destination files are present and are either the same
   file as the verified source or match recovery metadata.
4. Remove the one agent-owned CloneKit entry from `objects/info/alternates`.
5. Flush metadata where the platform supports it, then remove the seed. Unlinking
   the seed names leaves the target links intact.

The initial eligibility gate excludes user `--reference`,
`--reference-if-able`, `--dissociate`, `--shared`, `--local`, and
`--no-hardlinks` flags, and excludes an active on-host reference at the
direct-checkout site. Apply the same alternate/hard-link checks to
`BUILDKITE_GIT_CLONE_MIRROR_FLAGS` at the on-host site. Therefore the alternates
file created by this path must contain exactly one normalized line: the
agent-owned seed object directory. Adoption removes the file rather than
attempting a cross-platform atomic rewrite that could damage user alternates.

Linux experimentation confirmed that hard-link adoption followed by seed
removal passes `git fsck --full --no-dangling`. It also confirmed an important
shallow-clone caveat: `.git/shallow` continues to enforce a one-commit logical
history, but parent objects from the full seed remain physically available.
That is why the first release skips shallow and partial shapes rather than
claiming that forwarding clone flags preserves their transfer/storage
semantics (§8).

### 3.3 Missing on-host mirror creation

When `BUILDKITE_GIT_MIRRORS_PATH` is active, use CloneKit only inside the
missing-mirror creation arm, under the existing clone lock:

```text
prepare verified seed on mirror filesystem
git clone --mirror <existing mirror flags> --reference <seed> \
    -- <canonical> <existing staging dir>
adopt seed triplet into staging; remove CloneKit alternate
git --git-dir=<staging> remote set-url origin <canonical>
rename <staging> <mirrorDir>
```

This preserves the existing atomic publication and mixed-agent invariant: a
published shared mirror always has canonical `origin` and no temporary
alternate. A successful but lagging seed is still useful because the canonical
mirror clone obtains the missing delta. Existing on-host mirrors keep today's
targeted canonical fetch; downloading a full kit to refresh them would be a
regression.

### 3.4 Alternatives rejected

**Populate the checkout directly from the bare seed — rejected.** Recreating
`git clone` would require hand-implementing worktree/index setup, clone flags,
shallow and partial configuration, tags, templates, separate Git directories,
submodules, and origin refspecs. The agent has already avoided this class of bug
for remote Git mirrors by keeping a real clone.

**Fetch broadly into the seed before cloning — rejected.** The shell prototype's
`+refs/heads/*:refs/heads/*` fetch duplicates and can contradict the agent's
custom refspec, PR, tag, and exact-commit semantics. The anchor only aids
negotiation; existing canonical clone/fetch code decides refs.

**Use only the on-host mirror path — rejected.** It is the highest-value hosted
path but does not help direct pipelines or agents without
`--git-mirrors-path`.

**Use only an ephemeral seed — rejected.** On hosted agents, bypassing the
existing shared mirror would repeatedly download the same full kit and discard
the cache lifecycle already designed for that fleet.

**Verify while canonical Git reads the pack — rejected.** Installing or
referencing unverified bytes makes a digest failure a repository-repair problem
instead of a private staging cleanup. Inline chunk hashing recovers most of the
prototype's concurrency without crossing that safety boundary.

**Try the remote Git mirror after CloneKit fails — rejected.** Two speculative
accelerators make the miss path unbounded and telemetry ambiguous. The backend
should issue one; the agent gives CloneKit deterministic precedence if a bad
backend rollout issues both.

## 4. Backend, bootstrap, and environment contract

### 4.1 Proposed backend fields

The backend should model one structured CloneKit capability and flatten it to
three bootstrap values:

| Environment value | Meaning |
| --- | --- |
| `BUILDKITE_GIT_CLONE_KIT_URL` | HTTPS manifest URL, exactly `<CloneKit repository URL>/clone-kit`; no query, fragment, or userinfo |
| `BUILDKITE_GIT_CLONE_KIT_REPO_ID` | expected stable UUID, compared with manifest `repo_id` |
| `BUILDKITE_GIT_CLONE_KIT_DELIVERY` | immutable `cdn` or `s3`; the agent does not infer cloud topology or race both |

A URL alone is insufficient. It cannot detect a stale/wrong repository manifest
because `repo_id` would be syntax-checked but not bound to an expected identity,
and it leaves CDN/S3 policy to per-agent guesswork. Keeping these as one backend
capability also lets the backend omit all three atomically.

The backend emission gate is the feature gate and kill switch. It must be off
by default, selectable per organization/pipeline during canarying, and removable
without an agent redeploy. No `EXPERIMENTS.md` entry or user-facing agent flag
is proposed unless the backend cannot provide an emission-side kill switch.

### 4.2 Direct and Origin-mirror topology

Strip the literal `/clone-kit` suffix to obtain the base CloneKit repository
URL. After URL normalization it must equal one of:

1. the original backend-supplied canonical HTTPS repository (direct Origin
   pipeline); or
2. the original immutable `BUILDKITE_GIT_REMOTE_MIRROR_URL` (Origin-backed
   mirror pipeline).

No third repository URL is accepted. The current candidate statement “obtain
credentials for canonical” is correct for direct pipelines but cannot also
describe an Origin-backed mirror on another origin. In the mirror case the
credential helper must be filled for the configured Origin repository base,
which the existing backend token issuer already recognizes as a pipeline clone
mirror. It must still never be filled for `/clone-kit` because
`credential.useHttpPath=true` makes that a different credential identity.

This topology and token-mint behavior require provider/backend confirmation
before implementation (§13 Q1–Q3). Do not weaken the URL check to arbitrary
same-origin or cross-origin credentials to make an unconfirmed topology work.

If pre-checkout hooks change `BUILDKITE_REPO`, CloneKit is skipped with
`canonical-changed`. The existing `canonicalRepository` snapshot in
`internal/job/executor.go` provides this binding.

### 4.3 Agent plumbing and protection

Follow the remote-mirror pattern:

- add three internal bootstrap flags/env sources and fields to
  `BootstrapConfig` in `clicommand/bootstrap.go`;
- add fields to `ExecutorConfig` in `internal/job/config.go` **without `env`
  tags**, so `ReadFromEnvironment` cannot refresh them after hooks;
- pass them through `job.New` in `clicommand/bootstrap.go`;
- add all three names to `protectedEnv` in `env/protected.go`;
- in `agent/job_runner.go` → `createEnvironment`, first snapshot then explicitly
  set all three values to empty, preventing ambient process env from enabling
  CloneKit when the backend omitted or rejected it;
- validate the base CloneKit repository URL with the existing
  `validateJobValue(...AllowedRepositories...)`. A miss declines CloneKit and
  does not refuse the job. Record only the safely redacted base for a warning
  emitted from `agent/run_job.go` after `jobLogs` exists;
- extend `formatDebugEnvironmentVariable` in `internal/job/executor.go` so the
  manifest URL is credential-redacted even though validation forbids userinfo;
  never print the other capability values as a URL or secret.

`--allowed-repositories` applies to the CloneKit repository, not each presigned
artifact host. Artifact URLs are short-lived capabilities selected by an
authenticated, allowed provider. Treating CDN/S3 hosts as Git repositories
would make normal allowlists reject every kit. Fleet network policy remains the
operator control for artifact egress; this boundary needs security sign-off.

Custom checkout hooks and checkout plugins replace the default checkout and do
not use CloneKit. They should not report a misleading CloneKit outcome.

## 5. Attempt resolution and precedence

Introduce a per-attempt `cloneKitAttempt`, resolved once in
`defaultCheckoutPhase` beside `remoteMirrorAttempt`. It is not executor-lifetime
state.

Conceptual fields:

```text
site          none | on-host-mirror | fresh-checkout
outcome       notReached | hit | miss | timeout | error | skipped
skipReason    bounded enum
failureStage  none | credential | manifest | download | verify | seed | clone | adopt | cleanup
delivery      cdn | s3
duration      total optional-work duration
```

`notReached` remains the zero outcome so an early checkout error cannot report a
false hit.

Common eligibility requires:

- all three backend values are present and internally consistent;
- manifest and base URLs pass §4's HTTPS, identity, and path rules;
- the expected repo ID is a canonical UUID and delivery is `cdn` or `s3`;
- `e.Repository == e.canonicalRepository`;
- this is checkout attempt one;
- manifest v1 supplies strict SHA-1 object IDs and pack names, with the backend
  responsible for emitting it only for a compatible repository; and
- a same-filesystem hard-link probe succeeds before network work.

The direct site additionally requires a full checkout shape: no shallow/depth
flags, partial filter, or sparse checkout configuration. Its clone flags may not
contain positive `--reference*`, `--dissociate`, `--shared`, `--local`,
`--no-hardlinks`, or `--separate-git-dir` spellings. The on-host site may serve
shallow/partial/sparse downstream checkouts because the on-host mirror is
already full by design, but applies the same incompatible-flag check to
`BUILDKITE_GIT_CLONE_MIRROR_FLAGS` and skips if those mirror flags request a
shallow or filtered mirror. Recognize Git-accepted long-option abbreviations
and split `--option value` forms conservatively; an ambiguous or invalid
spelling falls back to the unchanged canonical command, which reports its
ordinary error.

Do **not** gate on build commit, branch, tag, PR shape, or custom fetch refspec.
CloneKit does not answer those questions. The unchanged canonical clone and
`fetchSource` do.

Site selection:

1. Active on-host mirror updates select `on-host-mirror`; the site is reached
   only if the mirror is still missing after acquiring its clone lock. If
   another process populated it, report `notReached` and use that mirror.
2. Otherwise an existing `.git` selects `skipped/existing-checkout` after any
   interrupted CloneKit adoption is repaired.
3. Otherwise select `fresh-checkout`.

`GitMirrorsSkipUpdate` retains its current meaning for the shared mirror. If it
finds an existing mirror, CloneKit is not used. If no mirror exists and the
checkout itself is fresh, the direct ephemeral site may run because it does not
write the shared mirror.

When CloneKit is configured, `resolveRemoteMirrorAttempt` returns
`skipped/clone-kit-configured` before any remote Git mirror site can run. A
CloneKit skip or error goes to canonical, not back into remote-mirror
resolution.

## 6. Manifest, credentials, HTTP, and verification

### 6.1 Manifest v1 validation

Fetch the manifest with a bounded body (recommended initial cap: 1 MiB) and a
strict JSON decoder that rejects duplicate keys, trailing values, and unknown
fields for version 1. A new contract requires a new `version`; an agent that
does not support it fails open with `unsupported-version` rather than guessing
that additive fields are harmless.

Validate before creating artifact requests:

- `version == 1`;
- `repo_id` is a canonical, non-nil UUID equal to
  `BUILDKITE_GIT_CLONE_KIT_REPO_ID`;
- `tip_commit` is exactly 40 lowercase hexadecimal characters;
- `files` has exactly three entries: one each named `pack`, `idx`, and `rev`;
- every path is exactly
  `objects/pack/pack-<40-lowercase-hex>.<matching extension>`, uses `/`, has no
  empty, dot, dot-dot, absolute, escaped, or backslash component, and all three
  entries share the same pack basename;
- each `size` is positive, fits `int64`, and the sum cannot overflow. Final
  absolute pack/sidecar limits must be agreed with Origin (§13 Q7); do not ship
  guessed limits that reject the target repository;
- each `sha256` is exactly 64 lowercase hexadecimal characters;
- every selected artifact URL is absolute HTTPS with a host, no userinfo, and
  no fragment. Query strings are allowed because they carry presigned
  capabilities;
- `cdn` uses each required `url`; `s3` requires `s3_url` on all three entries.
  Do not mix delivery modes within one seed;
- when `pack_chunks` is present, `chunk_size` is positive and bounded, the hash
  count is exactly `ceil(pack.size/chunk_size)`, every hash has strict SHA-256
  syntax, and the final chunk covers exactly the remaining bytes. Empty or
  partial chunk metadata is invalid.

Never derive a local path by joining the manifest's `path`. Map the three
validated logical names to agent-computed staging filenames. This turns path
validation into contract checking rather than a traversal defense that later
code must still get right.

Manifest `404` or `410` is `miss`. Auth failures, other statuses, malformed
responses, and unsupported versions are `error` with a failure stage. Do not
include response bodies in errors.

### 6.2 Credential flow

Run a prompt-hidden:

```text
git -c credential.useHttpPath=true \
    -c credential.helper= \
    -c credential.helper=<buildkite-agent helper> \
    credential fill
```

Feed `url=<base CloneKit repository URL>` on stdin. Resetting the helper list
ensures only the managed repository-provider helper is consulted; ambient OS
keychains and customer helpers are not asked for credentials the agent will
forward itself. Parse `username` and `password` from captured stdout without
ever writing the captured value or a wrapping error containing it to shell
output. The native v1 design has no explicit-token environment escape hatch.

Set Basic Authorization only on the manifest request. Use a separate artifact
client/request constructor that has no auth state. Reject manifest redirects so
Go cannot forward authorization under its same-domain redirect rules. Reject
artifact redirects in v1 as well: the presigned URL itself is a bearer
capability and should not be copied into a redirect `Referer` or a second host
without a provider contract.

Do not use `internal/agenthttp.Do` or the generic artifact downloader for these
requests: both include URLs in debug/error logs, while CloneKit artifact URLs
are secrets. Reuse `agenthttp` transport construction only if it can be done
without the URL-logging wrapper; otherwise build a private `http.Client` from a
clone of `http.DefaultTransport`.

### 6.3 Timeouts and low-speed behavior

Recommended starting bounds, pending Q8:

- TCP dial: 10 seconds;
- TLS handshake: 10 seconds;
- response headers / first byte: 30 seconds;
- manifest total: 30 seconds, including body;
- artifact no-progress window: 60 seconds per response;
- no fixed total timeout for a progressing artifact request;
- no CloneKit-level retries.

Set `Accept-Encoding: identity`. Use a per-request watchdog that cancels the
request context if no successful body read occurs for the no-progress window;
Go's `http.Client.Timeout` is unsuitable for a multi-GB progressing pack. The
watchdog must stop on completion and cannot leak a goroutine. Parent context
deadlines always win.

No retry means a failed S3 request does not fall back to CDN, and vice versa.
Canonical Git is the one fallback budget.

Classify timeout against both contexts: an internal CloneKit deadline or idle
watchdog with a still-live parent records `timeout` and falls back, while
`ctx.Err() != nil` returns the parent cancellation/deadline and forbids new
canonical work.

### 6.4 Parallel range download

Create and preallocate the pack with `File.Truncate(pack.size)`. Download `idx`
and `rev` concurrently as exact-length streams while a fixed worker pool of at
most 32 requests fills the pack with `WriteAt`/`io.NewOffsetWriter`.

Each sidecar requires `200 OK`, identity/no content encoding, the declared
`Content-Length` when present, and exactly its manifest byte count. A redirect
or generic 2xx response is not treated as equivalent.

When chunk hashes are present, ranges align exactly with manifest chunks.
Otherwise divide the pack into at most 32 deterministic, non-overlapping
ranges. For every range require:

- request `Range: bytes=<start>-<end>`;
- status `206 Partial Content` (a `200` is not accepted for a ranged pack);
- exact `Content-Range` start, end, and total;
- exact `Content-Length` when supplied;
- no content encoding;
- exactly the expected body byte count, rejecting both truncation and extra
  data.

Hash `idx` and `rev` while streaming. When chunk hashes exist, hash each pack
range while writing, so download and cryptographic verification overlap safely
inside private staging. If chunk hashes are absent, wait for every range then
read the preallocated pack sequentially once for its whole-file SHA-256. SHA-256
itself is serial; pretending 32 independent hashes combine into the manifest's
whole-file digest would be incorrect.

Close and `Sync` files before the seed becomes ready. Any worker failure cancels
the sibling workers, waits for them to exit, closes files, and removes staging.

### 6.5 Git-level seed validation

Cryptographic verification proves the bytes match the authenticated manifest,
but the negotiation anchor also claims that the client has the anchor's entire
reachable object closure. An incomplete seed can make upload-pack omit required
objects even when every file hash is correct.

After artifact verification:

1. `git init --bare` a private destination with no inherited alternates.
2. Install the verified triplet into its `objects/pack`.
3. Clear `GIT_OBJECT_DIRECTORY`, `GIT_ALTERNATE_OBJECT_DIRECTORIES`, and lazy
   fetch behavior for validation commands.
4. Confirm `tip_commit^{commit}` exists.
5. Run `git fsck --connectivity-only --no-dangling <tip_commit>` (or the exact
   tested equivalent) to prove the complete reachable closure before the
   anchor is advertised.
6. Write `refs/clone-kit/anchor` with `git update-ref` and atomically publish a
   URL-free recovery record containing only version, target identity digest,
   repo ID, tip, filenames, sizes, and SHA-256 values.

The connectivity scan is a correctness requirement unless Origin provides a
different verifiable closure contract. Its cost belongs in the performance
benchmark; it must not be silently removed to hit the 12-second number.

### 6.6 URL-safe errors

Presigned URLs can appear in `*url.Error`, redirect errors, request dumps, and
wrapped transport errors. At the HTTP boundary, convert failures to a private
error carrying only operation, safe network cause, status class, and failure
stage. Strip `url.Error.URL` before wrapping. Never log request objects,
response headers, response bodies, manifest artifact paths, or underlying
errors known to contain a URL.

Unit tests inject unique credentials in every URL and assert they are absent
from returned errors, shell output, debug output, span events, and recovery
files.

## 7. Repository lifecycle, recovery, and fallback

### 7.1 Owned state

For direct checkout, place a deterministic hidden seed directory and lock beside
the checkout, not under it and not in the system temp directory. This ensures
the seed and target are on one filesystem and lets a later executor find state
after process death. The name is a fixed agent prefix plus a digest of the
absolute checkout path; it contains no repository URL. Acquire the seed lock
before deleting stale state or starting a download. Lock contention skips
CloneKit rather than delaying canonical checkout.

For on-host mirror creation, put the seed in a deterministic sibling of the
existing mirror staging directory, on the same filesystem, and hold the existing
mirror clone lock. The seed cannot live inside the staging clone destination,
which must be empty. Clean both private paths under that lock; do not introduce
a second lock ordering.

Recovery state has two durable milestones:

- **seed-ready** — all files and Git closure are verified; canonical Git may
  reference it;
- **clone-complete** — the canonical clone returned successfully, so adoption
  may preserve it. Until this marker exists, a partial checkout is discarded
  rather than guessed complete.

Neither state file contains the manifest URL, artifact URLs, credentials, or
raw repository URL.

### 7.2 Fresh checkout sequence

`prepareCheckoutWorkdir` retains the existing `.git` decision and adds this
fresh arm:

1. Repair or remove agent-owned interrupted state before ordinary existing
   checkout reconciliation.
2. If eligible, acquire the seed lock and probe hard-link support with a tiny
   file in seed and target parent directories.
3. Obtain credentials, manifest, artifacts, verification, bare seed, and
   anchor. Failures while the parent is live clean the seed and continue to the
   existing canonical clone without returning a `gitError`.
4. Run the ordinary canonical `git clone` once, with the same user flags and
   sparse-derived flags plus the final `--reference <seed>`.
5. If canonical clone fails, remove the incomplete checkout and seed and return
   the existing clone error to the outer retrier. Do not immediately run the
   same canonical network operation again merely because CloneKit was present.
6. Persist clone-complete, adopt the triplet, remove the sole CloneKit
   alternate, and remove the seed.
7. Continue with existing clean/fetch/verify/checkout behavior.

The seed anchor disappears with the seed; it is never copied into checkout
refs. Only pack files are adopted.

### 7.3 Adoption failure

Preflight should make adoption failure rare. If a link or alternate operation
fails after clone:

1. Keep the seed and alternate intact.
2. While the parent is live, run the existing full repack/dissociation behavior
   under an explicit recovery span. If it succeeds, remove the alternate and
   seed and continue. This is expensive but preserves the completed canonical
   clone on an exceptional path.
3. If dissociation fails, classify through the existing clean-and-retry path.
   Remove the checkout before removing a seed it may still reference. The next
   outer attempt is canonical-only.
4. On parent cancellation, do no network fallback. Use
   `context.WithoutCancel` with a fixed short deadline for Git adoption or
   dissociation. If the whole checkout must be quarantined, use the existing
   bounded `removeCheckoutDir` retry behavior, then remove the seed. Return the
   parent cancellation regardless; do not pretend that a context deadline can
   bound `removeCheckoutDir`, whose current implementation does not accept a
   context.

Seed deletion after a removed alternate is best-effort and non-fatal: the
checkout is independent. Emit `cleanup-error` and let the next seed lock owner
remove the URL-free orphan.

### 7.4 Recovery matrix

At the start of every default checkout attempt, inspect only the deterministic
agent-owned directory and an alternate that resolves exactly to its verified
`objects` path:

| Observed state | Recovery |
| --- | --- |
| Seed exists, no checkout / no alternate | remove orphan seed, then resolve this attempt normally |
| Checkout exists, seed-ready only | clone did not complete; remove checkout, then seed; retry canonical |
| Checkout exists, clone-complete, seed and alternate exist | verify recovery record, resume idempotent hard links, remove alternate, remove seed |
| Some target links already exist | verify size/hash, link only missing files, then continue adoption |
| Alternate removed, seed remains | verify target triplet, remove orphan seed |
| Agent-owned alternate exists but seed is missing or invalid | checkout may be incomplete; return clean-and-retry, never delete just the alternate and hope |
| Any unrelated alternate or temp directory | leave untouched |

Recovery runs before `updateRemoteURL` or any Git operation that might traverse
an interrupted alternate. Tests inject process death after every table
transition.

### 7.5 Missing on-host mirror sequence

Inside `updateGitMirror`, only after the clone lock confirms `mirrorDir` is
absent:

1. Remove an interrupted CloneKit/mirror staging directory under the lock.
2. Prepare the seed on the mirror filesystem.
3. Clone canonical with existing `--mirror` and
   `BUILDKITE_GIT_CLONE_MIRROR_FLAGS`, plus `--reference <seed>`, into the
   existing staging directory.
4. Mark clone complete and adopt/dissociate exactly as above.
5. Set staging `origin` to canonical before publication.
6. Atomically rename staging to `mirrorDir`, then use the existing snapshot
   behavior.

CloneKit failure with a live parent removes only CloneKit/staging state and
runs today's canonical mirror clone in the same lock acquisition. Cancellation
removes private staging and returns cancellation without starting canonical.
Existing mirror update logic is unchanged.

## 8. Compatibility decisions

| Area | Initial behavior |
| --- | --- |
| **Existing checkout** | Repair interrupted adoption if present, otherwise skip CloneKit and use normal canonical targeted fetch. |
| **Clean checkout** | Existing checkout is removed before attempt resolution, so it is eligible as fresh. Orphan seed cleanup is separate and ownership-checked. |
| **Existing on-host mirror** | Never download a full kit. Use current exact branch/PR/custom-refspec canonical update and existing snapshots/locks. |
| **Missing on-host mirror** | Use CloneKit under existing clone lock/staging/publication, then retain current checkout reference/dissociate mode. |
| **Remote Git mirror** | Backend should emit one accelerator. If both are present, CloneKit suppresses all remote-mirror sites for that attempt/job and failure falls directly to canonical. |
| **Custom clone flags** | Pass through unchanged, with CloneKit's reference appended last. At the direct site, alternate/hard-link-affecting, shallow/filter, sparse, and separate-Git-dir flags cause a named CloneKit skip. At the on-host site, apply the equivalent check to mirror clone flags; downstream checkout flags remain unchanged. |
| **Custom fetch refspec / tags / PRs / `HEAD`** | Eligible. CloneKit does not replace or skip `fetchSource`; current exact semantics remain authoritative. |
| **Shallow clone** | Direct site skips in v1. A local experiment showed the shallow boundary remains logical while the adopted full seed makes parent objects physically present. Missing full on-host mirror creation remains eligible because today's on-host mirror already has that physical shape. |
| **Partial/blobless clone** | Direct site skips in v1. A full CloneKit pack defeats missing-object and transfer expectations even though canonical `origin` remains the promisor. A missing full on-host mirror remains eligible; its downstream clone retains canonical partial-clone configuration. |
| **Sparse checkout** | Direct site skips in v1 because the agent normally adds `--filter=blob:none`. A missing full on-host mirror remains eligible; sparse paths, mode, clone, and fetch flags remain downstream checkout concerns. |
| **`--separate-git-dir`** | Skip. External Git-directory cleanup is outside `removeCheckoutDir`, and recovery cannot quarantine it safely. |
| **User references / dissociation / shared clone** | Skip. Initial adoption relies on owning the sole alternates-file entry. |
| **Submodules** | CloneKit seeds only the primary repository. Clone-time and later submodule operations still resolve against canonical URLs because canonical is `origin` throughout. On-host submodule mirrors remain canonical. |
| **Git LFS** | CloneKit contains Git objects, not LFS objects. Existing `GIT_LFS_SKIP_SMUDGE`, install, fetch, and checkout behavior remains canonical. Unlike a remote-mirror clone, origin never temporarily points at Origin solely for CloneKit. |
| **Commit verification** | Unchanged and still fetches/checks canonical branch state as configured. The anchor is never trusted as branch evidence. |
| **Hooks/plugins** | Pre-checkout repository rewrite makes CloneKit ineligible. A custom checkout hook/plugin bypasses built-in CloneKit. Post-checkout behavior is unchanged. |
| **Split checkout/command phases** | Direct checkout adoption completes and seed is removed before checkout exits. A CloneKit-created on-host mirror is durable. No cleanup instruction has to cross containers. |
| **Git object format** | Manifest v1 is SHA-1 only: 40-hex object IDs and pack names. SHA-256 repositories require a new agreed manifest version/contract. |
| **Filesystems** | ext4/XFS/APFS/NTFS and other same-volume hard-link-capable filesystems are candidates. Network/FAT-like filesystems that reject the preflight skip before download. Canonical support is unchanged. |

The direct shallow/partial skips are a deliberate correction to the candidate
design. Passing current clone flags preserves Git configuration and logical
shallow traversal, but adopting a full multi-GB pack still changes transfer
volume, physical object presence, disk use, and pruning behavior. That is not
semantic parity under the existing remote-mirror requirement and should not be
smuggled in as an implementation detail. The on-host site does not introduce
that difference: on-host mirrors are already full stores referenced by
downstream shallow/partial checkouts.

## 9. Telemetry and logs

Add attributes to the existing `repo-checkout` span and child spans for
manifest, download, verify, seed validation, clone, and adoption:

- `git.clone_kit.outcome`: `notReached|hit|miss|timeout|error|skipped`;
- `git.clone_kit.site`: `none|on-host-mirror|fresh-checkout`;
- `git.clone_kit.skip_reason`: bounded enum when skipped;
- `git.clone_kit.failure_stage`: bounded enum when timeout/error;
- `git.clone_kit.delivery`: `cdn|s3` only after validation;
- numeric `duration_ms`, `manifest_ms`, `download_ms`, `verify_ms`,
  `seed_validation_ms`, and `adopt_ms` when measured;
- numeric downloaded bytes and range count; and
- booleans for chunk verification, canonical fallback, and cleanup error.

Initial skip reasons:

```text
no-capability, incomplete-capability, invalid-url, repo-id-invalid,
delivery-invalid, canonical-changed, repository-topology-mismatch,
not-first-attempt, existing-checkout, shallow-clone,
partial-clone, sparse-checkout, user-alternates, separate-git-dir,
hardlink-controls, hardlink-unsupported, lock-contended,
mirror-flags-incompatible
```

The exact list should be centralized and table-tested. Do not put URLs, repo
IDs, commit IDs, pack names, digests, branch names, pipeline slugs, or error
strings into span attributes.

Emit one concise job-log line from the default checkout, for example:

```text
CloneKit: outcome=hit site=fresh-checkout delivery=cdn duration=12.4s
```

Failures add only the safe stage and canonical-fallback decision. Do not log the
manifest URL merely because it is non-presigned; a uniform no-URL policy is
easier to audit. Low-level HTTP debug dumping is disabled for CloneKit.

Useful rollout ratios are: reached sites versus `notReached`, hit/miss/error by
site and delivery, skip reasons, fallback rate, optional-work duration, total
checkout duration, canonical pack bytes, and cleanup errors. A high error rate
must remain visible even when every build succeeds through fallback.

## 10. Files and functions to change

The expected implementation map is:

| File | Change |
| --- | --- |
| `clicommand/bootstrap.go` | Add internal CloneKit URL/repo-ID/delivery fields and flags; pass immutable values to `ExecutorConfig`. |
| `clicommand/config_completeness_test.go` | Assert every new bootstrap config field has CLI/env plumbing. |
| `env/protected.go`, `env/protected_test.go` | Protect all CloneKit capability values from in-job mutation. |
| `agent/job_runner.go` | Mask ambient values, apply repository allowlist to the CloneKit base, drop the capability atomically, and record a warning decision. |
| `agent/run_job.go` | Emit the job-facing allowlist warning after logs exist. |
| `agent/job_runner_env_test.go`, `agent/integration/config_allowlisting_integration_test.go` | Cover backend, ambient, allowlist, and case-normalized environment behavior. |
| `internal/job/config.go`, `internal/job/config_test.go` | Add immutable executor fields with no `env` tags and prove hooks cannot refresh them. |
| `internal/job/executor.go` | Redact CloneKit URL in debug env; reuse canonical repository binding. |
| `internal/job/checkout.go` | Resolve and emit one CloneKit attempt per outer attempt; establish precedence with `remoteMirrorAttempt`; thread it to mirror/workdir sites. Do not change the checkout tail. |
| `internal/job/checkout_remote_mirror.go` | Add deterministic suppression when CloneKit is configured; keep existing behavior when it is absent. |
| `internal/job/checkout_workdir.go` | Repair interrupted direct adoption and add the fresh canonical `--reference` clone arm. |
| `internal/job/checkout_mirror.go` | Add CloneKit only to missing main-repository mirror creation under current lock/staging/publication; leave update and submodule paths unchanged. |
| `internal/clonekit/manifest.go` (new) | Strict v1 types/parser/validation with URL-free errors. |
| `internal/clonekit/http.go` (new) | Authenticated manifest request, unauthenticated artifact requests, redirect policy, timeouts, idle watchdog, and error sanitization. |
| `internal/clonekit/download.go` (new) | Preallocation, exact ranged GET worker pool, streaming/chunk/whole-file verification. |
| `internal/job/checkout_clone_kit.go` (new) | Executor eligibility, credential fill, attempt telemetry, owned paths/locks, hard-link adoption, recovery, fallback. |
| `internal/job/checkout_clone_kit_seed.go` (new) | Bare Git seed assembly, anchor closure validation, durable state milestones, and Git-command integration through the existing shell. |
| `internal/clonekit/*_test.go`, `internal/job/checkout_clone_kit*_test.go` (new) | Contract, HTTP, real-Git lifecycle, recovery, and redaction tests. |
| `internal/job/integration/checkout_integration_test.go` | Direct full-clone command and behavior matrix. |
| `internal/job/integration/checkout_git_mirrors_integration_test.go` | Missing/existing mirror behavior and lock/staging expectations. |
| `docs/git-mirror.md` and this document | After implementation, fold durable behavior into user/operator docs and mark this record implemented, following `remote-git-mirrors.md` convention. |

`internal/clonekit` is a deliberate deep boundary: given capability/auth input
and an owned destination, it validates the manifest and returns a
cryptographically verified artifact set without logging. It knows nothing about
Git commands, checkouts, mirrors, hooks, retries, or telemetry. `internal/job`
already owns shell process/cancellation behavior, so it assembles and validates
the bare Git seed there rather than adding a one-implementation command-runner
interface to `internal/clonekit`. Do not create wrappers around the existing
checkout tail or refspec logic.

## 11. Verification plan

### 11.1 Manifest and security unit tests

- table test every §6.1 field, unknown/duplicate key, integer overflow, path
  separator, case, URL, multiplicity, basename, delivery, and chunk-coverage
  rule;
- fuzz manifest parsing and assert no panic, unbounded allocation, or accepted
  path outside the three computed filenames;
- assert expected repo-ID mismatch fails before artifact requests;
- assert auth is present on manifest and absent on every artifact request;
- assert credential fill receives the base repository URL, never `/clone-kit`;
- reject manifest and artifact redirects and all non-HTTPS URLs;
- seed all URLs with unique tokens and scan errors, logs, spans, and recovery
  files for each token;
- ensure malformed server response bodies are not copied into errors.

### 11.2 Downloader and failure injection

Use `httptest.Server`/TLS fixtures for:

- exact 32-worker upper bound and observable parallelism;
- range partitioning with and without chunk metadata;
- `200` to range, wrong/missing `Content-Range`, wrong total, wrong
  `Content-Length`, truncation, extra byte, gzip, early EOF, connection reset,
  and one failed range cancelling all siblings;
- idx/rev/pack digest mismatch and chunk mismatch;
- manifest total timeout, dial/TLS/header timeout, body stall timeout, and a
  healthy slow progressing body that exceeds 60 seconds total without timing
  out;
- parent cancellation returning cancellation, no canonical-fallback callback,
  all workers joined, and staging removed;
- CDN and S3 selection with no cross-mode fallback;
- preallocation, close, sync, disk-full, permissions, and cleanup errors.

### 11.3 Real-Git seed and adoption tests

- construct a packed bare fixture with only `refs/clone-kit/anchor`; capture
  `GIT_TRACE_PACKET` and prove canonical clone advertises the anchor `have`;
- corrupt or omit an object reachable from the anchor and prove connectivity
  validation rejects the seed before clone;
- hard-link a verified triplet, remove the alternate and seed, then run
  `git fsck --full --no-dangling` and materialize the requested worktree;
- assert target files are hard links (`os.SameFile` where supported), not copied
  bytes, before seed removal;
- inject process death before/after seed-ready, clone-complete, each link,
  alternate removal, and seed deletion; start a new executor and assert the §7.4
  recovery result;
- precreate matching and conflicting destination pack names;
- verify unrelated alternate files/directories remain byte-for-byte unchanged;
- force hard-link and adoption failure and exercise exceptional dissociation,
  then clean-and-retry when dissociation also fails.

### 11.4 Checkout integration and differential tests

For direct checkout:

- compare canonical and CloneKit runs for origin URL, fetch refspecs, checked-out
  commit, refs, config, worktree, hooks, and both clean operations;
- cover exact commit, `HEAD`, custom refspec, tag, GitHub PR head/merge, custom
  clone/fetch flags, single branch, no-tags, LFS, submodules, and commit
  verification;
- assert canonical is contacted and sends a materially smaller pack on a hit;
- assert `fetchSource` still runs and obtains a requested commit absent from the
  default clone refs;
- assert manifest miss/error/timeout/hash failure takes the canonical clone in
  the same outer attempt, while parent cancellation does not;
- assert attempt two never contacts CloneKit;
- assert existing checkout skips CloneKit, while clean checkout can use it;
- assert shallow, partial, sparse, user-reference/dissociate/shared, and
  separate-Git-dir cases make zero CloneKit requests and exactly match current
  canonical behavior;
- run checkout-only then command-only executors and prove no seed dependency is
  needed by the second phase.

For on-host mirrors:

- missing mirror created through CloneKit has canonical origin, no alternate,
  passes fsck, publishes only after adoption, and then supplies the checkout;
- manifest/download/verification/adoption failure cleans staging and performs
  today's canonical mirror clone under the same lock;
- cancellation performs no canonical mirror clone;
- existing mirror, warm commit, targeted update, snapshot, skip-update,
  clone-lock timeout, repository rename collision, and submodule mirror tests
  assert zero CloneKit requests and unchanged behavior;
- shallow, partial, and sparse downstream checkouts can use a newly
  CloneKit-created full mirror and remain differential-equivalent to checkouts
  using a canonically created on-host mirror;
- mixed-version invariant: no published shared mirror ever exposes CloneKit
  origin, anchor ref, recovery file, or alternate.

For coexistence, configure both CloneKit and a remote Git mirror and assert the
remote Git server sees no request on CloneKit hit, miss, timeout, malformed
manifest, compatibility skip, and later outer attempt.

### 11.5 Cross-platform matrix

Run targeted and integration suites on Linux, macOS, and Windows with:

- paths containing spaces, Unicode, and long components;
- same-volume hard links on ext4/XFS-equivalent, APFS, and NTFS runners;
- a filesystem/test double where hard links are unsupported or cross-device;
- Windows open-file/delete behavior and antivirus-like delayed close;
- case-insensitive environment keys and filesystem paths;
- process cancellation during active range writes and Git clone;
- race detector on Linux for worker/watchdog/state transitions.

Canonical fallback passing on all platforms is a release gate even where
CloneKit reports `hardlink-unsupported`.

### 11.6 Performance validation

Benchmark on the same Everysphere revision, host type, network path, Git
version, and empty caches:

1. current canonical agent checkout;
2. existing shell CloneKit prototype;
3. native seed download/verification only;
4. native direct reference plus hard-link adoption;
5. native reference plus `--dissociate` as a control;
6. native missing on-host mirror creation and downstream checkout;
7. previous tarball seed where still available.

Record at least 10 runs of total checkout, manifest, download, verification,
connectivity scan, canonical clone/fetch, adoption, worktree checkout, canonical
bytes, artifact bytes, CPU, peak disk, and disk bytes written. Report median and
p95, not the best run.

Rollout acceptance:

- native hard-link adoption performs no full-pack rewrite after download;
- canonical bytes on a hit are the negotiation delta, not clone-sized;
- the final repository passes fsck and all differential behavior tests;
- median full native checkout is no slower than the 22-second tarball baseline
  and is within 20% of the shell prototype under the same measurement boundary;
- p95 CloneKit failure plus canonical fallback stays inside the agreed optional
  network budget; and
- connectivity validation cost is included, not hidden outside the reported
  timer.

If the hard-link path cannot meet those gates, do not make `--dissociate` the
default to claim correctness. Revisit the provider contract or keep CloneKit
disabled.

## 12. Delivery and rollout

### PR 1 — Capability plumbing, decision, and telemetry

**Behavior:** no CloneKit network or Git behavior. Backend values become safely
observable and a disallowed capability is dropped rather than refusing the job.
Do not suppress the working remote Git mirror in this foundations PR: until a
CloneKit production site exists, doing so would turn an accidentally early
backend emission into a canonical-only regression.

Includes environment masking/protection, immutable executor config,
allowlisting, debug redaction, `cloneKitAttempt`, skip reasons, and telemetry.

**Acceptance:** ambient/job/hook values cannot enable or mutate CloneKit;
allowlist miss runs the job canonically with a warning; every resolution row has
stable telemetry; no checkout command changes with or without the inert
capability.

### PR 2 — Strict manifest, credentials, and safe HTTP client

**Behavior:** no checkout integration. Add `internal/clonekit` manifest parsing,
authenticated manifest GET, delivery selection, timeout/redirect policy, and
URL-safe errors, plus the prompt-hidden credential-fill boundary in
`internal/job`.

**Acceptance:** parser/fuzz/security/redaction tests in §11.1 pass; no artifact
request can inherit repository auth; no returned/logged error contains a test
URL token.

### PR 3 — Parallel download, verified seed, adoption, and crash recovery

**Behavior:** no production site calls it yet. Add exact ranged download,
verification, bare seed closure validation, same-filesystem state, hard-link
adoption, exceptional dissociation, and recovery primitives.

**Acceptance:** downloader/failure-injection and real-Git tests in §§11.2–11.3
pass on all three operating systems; every injected crash yields a valid target
or a deterministic clean retry; hard-link-unsupported exits before artifact
requests in the caller fixture.

### PR 4 — Missing on-host mirror creation

**Behavior:** first production site, still behind backend emission gate. A
missing main-repository mirror may use CloneKit under the existing lock and
atomic publication lifecycle. Existing and submodule mirrors are unchanged.
Land the one-accelerator arbitration here: once CloneKit can actually run, its
configured attempt suppresses the remote Git mirror for the whole job and fails
directly to canonical.

**Acceptance:** all on-host tests in §11.4 pass; published mirror has canonical
origin/no alternate/no anchor, mixed-version behavior is preserved, canonical
fallback occurs in the same attempt, coexistence never contacts both
accelerators, and hosted-fleet benchmark beats the tarball gate.

Ship/update `docs/git-mirror.md` with this PR because it is the first behavior
change.

### PR 5 — Fresh direct checkout

**Behavior:** fresh full checkouts without an active on-host mirror may clone
canonical with the verified seed reference and adopt it. Existing, shallow,
partial, sparse, separate-Git-dir, and user-alternate shapes remain canonical.

**Acceptance:** direct differential, failure, coexistence, split-phase, LFS,
submodule, PR/refspec, cross-platform, and performance tests in §11 pass.
`fetchSource` remains unchanged.

PRs 4 and 5 depend on PRs 1–3 but are independently revertible and can be
enabled separately by backend site targeting. Land PR 4 first because the
hosted on-host mirror is the highest-value and simpler lifecycle.

### Rollout order

1. Backend implements the structured capability, expected repo ID, delivery
   decision, provider token mapping, mutual exclusion with remote mirror, and a
   global emission kill switch. Keep emission off.
2. Land agent PRs 1–3 and run parser/downloader/adoption tests against an Origin
   staging repository.
3. Land PR 4, deploy to a hosted canary fleet, enable a small set of full-clone
   Origin-mirror pipelines, and compare against tarball/remote-mirror baselines.
4. Expand PR 4 by fleet while watching hit/error/fallback/duration/canonical-byte
   telemetry. Disable backend emission on provider incident; do not redeploy
   agents.
5. Land PR 5 and canary direct full-clone pipelines separately.
6. Enable S3 and CDN cohorts separately so delivery failures are attributable.
7. Only after Q10 is resolved and production data supports it, design a new
   gated slice for direct shallow/partial/sparse support. It is not part of
   PR 5; missing full on-host mirrors already cover those downstream shapes.

Every stage can be rolled back by removing backend emission. Cached pack
objects are content-addressed and may remain in a checkout/on-host mirror after
disablement; no credentials or URLs remain.

## 13. Product and provider questions

These need explicit owner answers. Q1–Q8 block implementation contract or safe
defaults; Q9–Q12 block rollout or later compatibility expansion.

**Q1 — Direct versus mirror URL topology.** For an Origin-backed mirror
pipeline, is `/clone-kit` rooted at `BUILDKITE_GIT_REMOTE_MIRROR_URL`, while a
direct pipeline roots it at `BUILDKITE_REPO`? If not, provide the exact mapping
that lets the agent validate where credentials may be sent. Owners: Origin and
Buildkite backend.

**Q2 — Credential identity and auth scheme.** Confirm the existing
`repository_access_token` endpoint mints valid credentials when filled for the
base Origin remote-mirror URL, and confirm whether manifest auth is HTTP Basic
with the helper's username/password. If it requires Bearer or an explicit
CloneKit token, change the backend contract rather than adding a user-settable
secret env escape hatch. Owners: Origin and repository-token backend.

**Q3 — URL signalling.** Approve the proposed URL + expected repo ID + delivery
capability, or provide the backend/API field that replaces it. Is the manifest
URL always the exact `/clone-kit` child with no redirect? Can it rotate between
jobs? Owner: backend.

**Q4 — Repository identity.** Is `repo_id` a stable UUID across repository
renames, direct/mirror forms, and credential rotation? Which backend value is
authoritative for comparison? What event intentionally changes it? Owner:
Origin.

**Q5 — CDN versus S3.** Should backend select `cdn` or `s3` per fleet/job as
proposed? Are all three `s3_url` values present together? Are S3 URLs region
specific and directly reachable by hosted and self-hosted agents? Is any
CDN↔S3 retry desired despite the one-accelerator budget? Owners: Origin and
hosted-agent infrastructure.

**Q6 — Manifest evolution.** Are unknown v1 fields forbidden, with semantic
changes requiring `version: 2`, or are additive unknown fields expected? Are
duplicate JSON keys forbidden? What statuses represent a normal miss versus an
incident? Owner: Origin.

**Q7 — Artifact and chunk limits.** What are current/p95/max pack, idx, and rev
sizes and chunk counts for Everysphere and the largest intended repository?
What minimum/maximum chunk sizes and manifest body limits can the agent enforce
without rejecting legitimate kits? Owner: Origin.

**Q8 — Timeout and retry expectations.** Confirm expected p95/p99 connect,
TLS, manifest, time-to-first-byte, per-range no-progress, and full download
times. Are presigned URLs long-lived enough for 32 non-retrying ranges? Is a
60-second no-progress bound compatible with cold generation/CDN behavior?
Owners: Origin and Buildkite reliability.

**Q9 — Pack contract.** Is v1 always exactly one self-contained, non-thin SHA-1
pack with matching idx/rev and the complete reachable closure of `tip_commit`?
May it contain unreachable extra objects? Is `.rev` mandatory for every
supported Git version? Are bitmaps ever supplied? Owner: Origin.

**Q10 — Supported Git object formats and checkout shapes.** Is SHA-1-only
acceptable for v1? Does product accept a full local object store for a
logically shallow/partial direct checkout, or should the direct site remain
canonical until Origin can issue shape-compatible manifests? Missing on-host
mirrors are already full stores and do not depend on this answer. Owners:
Origin, agent, and product.

**Q11 — Measurement boundary.** Does the reported 12 seconds include manifest
auth, all artifact downloads, SHA-256 verification, Git connectivity checking,
the canonical clone/fetch, pack adoption/dissociation, worktree checkout, LFS,
and submodules? Provide command boundaries and run distribution, not one best
sample. Owner: Origin.

**Q12 — Rollout and coexistence.** Can backend guarantee mutual exclusion
between CloneKit and `BUILDKITE_GIT_REMOTE_MIRROR_URL` while still supplying the
Origin base URL needed for mirror-mode credentials? Is a backend emission kill
switch available at organization and global scope? Owners: backend and agent
operations.

Until Q1–Q3 are settled, implementation should not encode “same origin as
canonical” as if it supported both advertised product modes. Until Q9 and Q11
are settled, the 12-second result is evidence to benchmark against, not an
acceptance claim for the native lifecycle.
