# Remote Git mirrors: review of three competing plans

Status: review. This document compares three independently produced design and
implementation plans for remote Git mirrors, recommends one, and records what
to carry over from the others and why. It is a decision document, not a plan:
the plan it recommends lives on its own branch. Once §6's amendments have been
folded into that plan, this document becomes a historical record: mark each
folded amendment with the folding commit, and record the answers to §8's open
questions in the plan of record's own open-questions section, not here.

## The three documents

Each plan is the file `docs/remote-git-mirrors.md` on its own branch (the
"requested as" names are the filenames used in the review request; they do not
exist in the repository). Branch tips are pinned so citations survive branch
pruning. This review refers to the plans by short name:

| Name | Requested as | Branch (tip at review time) | Length |
| --- | --- | --- | --- |
| **Opus** | `remote-git-mirrors-opus-5.md` | `pda/remote-git-mirrors-plan-c275` (`d1a4c7c4`) | 1177 lines |
| **Fable** | `remote-git-mirrors-fable-5.md` | `pda/fable-remote-git-mirrors-plan-5858` (`2ffd3c01`) | 563 lines |
| **Sol** | `remote-git-mirrors-sol.md` | `pda/sol-remote-git-mirror-plan-23ea` (`f3c40488`) | 650 lines |

Citations use the document's own headings, e.g. (Opus §5.3), (Fable "Tier A"),
(Sol "Fallback details"). Requirement numbers are always qualified with their
plan (Opus R2, Fable R9), because the plans' numbering schemes collide;
rejected alternatives in §7 use RA-numbers to stay out of both namespaces.

### Method

Every consequential claim in this review was checked against one of:

- the agent codebase at `main` (`3a1d970a`, after
  [#4152](https://github.com/buildkite/agent/pull/4152)): `checkout.go`,
  `checkout_mirror.go`, `checkout_fetch.go`, `git.go`,
  `commit_verification.go`, `agent/run_job.go`, `agent/job_runner.go`,
  `env/protected.go`, `clicommand/git_credentials_helper.go`,
  `tracetools/span.go`;
- the backend (`buildkite/buildkite`): `app/models/job/environment.rb`,
  `app/models/job/code_access_token_issuer.rb`,
  `app/models/feature/pipeline_clone_mirror.rb`;
- empirical Git experiments run for this review on git 2.43.0 (the same
  version Opus cites), listed in the verification appendix.

The appendix is the single record of what was and was not re-verified;
statements in the body defer to it. Length and rhetorical confidence were
treated as costs, not evidence: every plan's central factual claims were
tested the same way.

---

## 1. Executive decision memo

**Decision: adopt Opus as the plan of record, amended with specific items
from Fable, Sol, and this review's own measurements (§6, A1–A10). Record
Sol's `insteadOf` transport mechanism as the principal rejected alternative.
Defer packfile-URI/CDN offload, per Opus §11, keeping Sol "Path 4" as the
design sketch for when it is picked up.**

Rationale, in order of weight:

1. **Correctness, verified.** Opus is the only plan whose factual foundation
   (its §4.2 G-table and its codebase mechanics in §4.1, §5.1–5.8) survived
   re-verification essentially intact — every claim tested for this review
   reproduced, including the exact exit code of G2's local-transport case and
   the silent `--filter` degradation (G10); the appendix records which claims
   were not re-tested. One Opus measurement was strengthened rather than
   contradicted: protocol v2 serves even *unreachable* SHAs on stock
   `git-upload-pack`, where Opus §11 only claimed the reachable case.
   By contrast, Fable's caveat 5 ("Mirror servers must support SHA
   fetches [via] `uploadpack.allowAnySHA1InWant`") is wrong as stated for
   stock protocol-v2 servers (measured, appendix T1a–T1c), and Sol's flat
   "conservative cap such as 30 seconds" on mirror attempts (Sol "Fail open
   without multiplying retries") reintroduces the exact failure Opus §5.3 and
   Fable's item 12 ("Anticipated review feedback") independently identify: a
   wall-clock cap kills legitimate bulk transfers on precisely the
   repositories large enough to justify a mirror.
2. **It resolves the review history most completely.** All three plans map
   the blocking threads from
   [#4144](https://github.com/buildkite/agent/pull/4144) and
   [#4153](https://github.com/buildkite/agent/pull/4153) to dispositions
   (Opus §7, Fable "Anticipated review feedback", Sol "Prior review concerns").
   Only Opus resolves them to the level of mechanism — e.g. the promisor
   left behind by a filtered mirror fetch on a reused checkout is not just
   "cleaned up" but retargeted, with the measured silent-corruption mode that
   makes retargeting (not unsetting) mandatory (Opus PR 3), a failure mode
   neither other plan notices.
3. **It is the most implementable.** Opus grounds each insertion point in the
   real control flow — the `hasGitCommit` short-circuit inside
   `isMainRepository`, the `urlChanged`-gated `fsck`/`gc`, the nil
   `r.jobLogs` at `createEnvironment` time, the `gitFetchArgs.GitFlags`
   re-splitting hazard — all verified here. To be precise about what is
   unique: all three plans settle the fleet-visible *semantics* (the
   allowlist declines the mirror rather than refusing the job; mirror
   attempts never retry), but only Opus grounds the *mechanics* — where the
   drop must happen and why the warning cannot be logged from
   `createEnvironment`, and which retrier arms the new error classification
   must touch. Those mechanics are where the earlier attempts (#4144, #4153)
   actually foundered.
4. **The costs of choosing Opus are real but acceptable.** It is the most
   prescriptive document and its PR 1 is large; §6 amends the delivery plan
   to allow splitting it. Its eligibility is the narrowest (branch-name
   clauses apply to sites that do not need them, Opus §5.2), which costs
   coverage on rare builds, is deliberate, and is cheap to relax later.

What the other plans contribute (details in §5 and §6): Fable contributes a
verified tag-auto-follow rationale, the mixed-fleet caveat, the
`--git-clone-mirror-flags` credential note, and `GIT_NO_LAZY_FETCH` hardening;
Sol contributes the cancellation-fallback rule, the cache-contents caveat, the
routing of lazy/materialization fetches through the mirror as a data-driven
follow-up, and the sharpest version of the kill-switch question — made
concrete by the fact that the backend already emits
`BUILDKITE_GIT_REMOTE_MIRROR_URL` unconditionally whenever
`clone_mirror_url` is set (`app/models/job/environment.rb`), with no
emission-side flag to turn it off fleet-wide.

Confidence: high on the mechanism-level comparisons (measured); medium on the
operational judgements (kill switch, packfile-URI deferral), which §8 returns
to humans.

---

## 2. Independent analyses

### 2.1 Opus (`remote-git-mirrors-opus-5.md`)

**Central architecture.** The mirror is consulted at most once per checkout
attempt, at exactly one of three mutually exclusive sites — on-host mirror
refresh, existing-checkout fetch, fresh clone — selected by a decision
resolved once at the top of `defaultCheckoutPhase` and threaded through
(Opus §5.1). Fresh acquisition is a real `git clone` of the mirror followed by
`git remote set-url origin <canonical>` (§5.6); the on-host mirror is warmed
by an exact-SHA fetch into a namespaced, sanitised ref
(`refs/buildkite-agent/remote-mirror/<branch>`, §5.8); a confirmed hit also
skips the canonical commit fetch (§5.7). Budgets are shaped: 30-second
wall-clock for probe-shaped fetches, stall-guard (`http.lowSpeedLimit/Time`)
for bulk transfers (§5.3). Four stacked PRs: telemetry-first foundations, then
one site per PR (§6).

**Strongest ideas and useful revelations.**

- The measured G-table (§4.2). G1/G2 (exact-SHA fetch needs protocol v2, not
  server config), G10 (silent `--filter` degradation), and G4 (reference-clone
  negotiation reads non-standard ref namespaces) were re-verified here and are
  load-bearing for the whole design space, not just for Opus.
- §5.7's "hit must also skip the canonical commit fetch" — with the
  observation, verified in `checkout_fetch.go`, that the two sites sit on
  opposite sides of `fetchSource`'s `skipFetch` expression and therefore
  cannot share one expression.
- PR 3's promisor handling: after a filtered mirror fetch into an existing
  checkout, the mirror-keyed promisor keys must be *retargeted* to `origin`,
  not merely removed — removal leaves a partial repository with no promisor,
  and the failure is silent (`git checkout` exits 0 with files missing).
  Neither other plan identifies this.
- The allowlist analysis (§5.2): `validateConfigAllowlists` refuses the job
  (`SignalReasonAgentRefused`, verified in `agent/run_job.go`), so checking
  the mirror URL there would stop every job on a fleet the moment a pipeline
  sets `clone_mirror_url`. The drop-in-`createEnvironment` mechanism, and the
  observation that `r.jobLogs` is nil at that point (verified in
  `agent/job_runner.go`), is the difference between a plan and an incident.
- Opus R9's telemetry design: `notReached` as the zero value, with the argument
  that a site is frequently selected and never reached (the `hasGitCommit`
  short-circuit — verified — makes `notReached` the *common* case on warm
  hosted caches, §5.1, §9).
- §5.5's argv plumbing: `gitFetchArgs.GitFlags` is a `string` re-split with
  `shellwords.Split`, and the credential-helper value is itself
  space-separated. Verified, including the pre-existing mis-split of a
  space-containing `--git-mirrors-path` in the one production caller.
- The caveat register with comment placement (§10) — each accepted risk names
  the file where the acceptance comment lives, which is what stops the next
  reader re-filing it as a bug.

**Assumptions and constraints.** Customer controls both hosts; no adversarial
relationship (§3). `repository_access_token` answers for the mirror URL —
grounded in `Job::CodeAccessTokenIssuer`'s clone-mirror mint path, and
correctly noted as currently Cursor-Origin-specific (§5.4; verified in the
backend). Eligibility requires a full SHA, non-empty sanitisable branch, no
tag/PR/refspec, first attempt, https (§5.2). Backend gating suffices for
rollout; no agent flag, no experiment (§9).

**Weaknesses, risks, unresolved questions.**

- *Prescriptive weight.* At 1177 lines it specifies down to enum ordering and
  defer registration order. Much of that is justified by a named failure, but
  the document risks the same review dynamic it criticises: no reviewer can
  hold all of it, and its PR 1 (config, binding, threading, argv plumbing,
  error type, telemetry, allowlist drop) is a single large review.
- *Coverage cost of uniform eligibility.* The branch clauses exist only for
  the on-host-mirror site's ref name, but decline the mirror at all sites
  (§5.2 admits this is for uniformity). A rare no-branch build loses the
  optimisation for no correctness reason.
- *C11 residual*: on the on-host-mirror site a hit still pays the canonical
  commit fetch from the checkout. Deliberate and argued (it keeps
  `refs/remotes/origin/*` advancing), but it concedes exactly the round trip
  Opus R2's own wording targets; the practical defence is that hosted fleets run
  `--git-skip-fetch-existing-commits`.
- *No answer for blobless reused checkouts.* On the existing-checkout path, a
  hit transfers commits/trees from the mirror, but the blobs a sparse
  checkout materialises still lazy-fetch from canonical (promisor = origin).
  Sol is the only plan that notices this gap (see §2.3).
- *Unverified-here measurements.* G8/G9 (header reset), G12–G14 (low-speed
  timer), and the LFS smudge measurements were not reproduced for this review
  — they need HTTP and timing fixtures — so conclusions that rest on them
  (notably the stall-guard values and RA2's G12/G14 citations) carry Opus's
  measurement plus consistency with documented curl behaviour, not a
  reproduction. PR 2's end-to-end zero-object table was likewise not
  reproduced in full; T5 verifies its mechanism (namespaced-ref negotiation)
  on a small fixture, not the table itself.
- *Open questions it leaves open* (§11) are the right ones: staleness
  contract, capability parity, URL rotation, `refs/pull/*`, LFS proxying.

**Where it is more detailed or insightful than the others.** Everywhere the
agent's own machinery is the risk: retrier interactions (`Break()` or pay 10
attempts), wipe-arm classification, defer ordering, `Getwd()` versus
`BUILDKITE_BUILD_CHECKOUT_PATH`, `snapshotMirror` exits, the
`updateRemoteURL`/`fsck` ordering. Also uniquely: the LFS analysis (don't
suppress the smudge filter — measured pointer-stub corruption in the default
configuration, PR 4), and C14's kill-window self-healing argument.

### 2.2 Fable (`remote-git-mirrors-fable-5.md`)

**Central architecture.** The same three transfer points, framed as tiers
(A: on-host mirror, B: existing checkout, C: fresh clone), plus tier D
(packfile-URI). The design principle: "an alternate URL for the same
repository data, substituted at existing transfer points"
(Fable "Background"). Tier C is `git clone <mirror>` + `set-url`, like Opus.
Tier A fetches `+refs/heads/<branch>:refs/heads/<branch>` from the mirror URL
with `--no-tags`. Five PRs: light foundations, then one tier per PR.

**Strongest ideas and useful revelations.**

- The tag auto-follow asymmetry (Fable "Tier A"): a destination-less
  `git fetch origin <branch>` does not auto-follow tags, while an explicit
  destination refspec from a URL does — so the mirror-directed refresh needs
  `--no-tags` that today's fetch does not. **Verified empirically for this
  review** (appendix T2), against this reviewer's initial expectation. This is
  the single best "revelation" in any of the three documents relative to its
  size: it is non-obvious, correct, and prevents importing lagged tags.
- The review-history mapping ("Anticipated review feedback, and how it's
  handled"): fourteen threads, each attributed to its reviewer and PR, each
  dispositioned, with item 13 (allowlist semantics change from #4153)
  explicitly flagged for reviewer sign-off and item 14 naming the bot-review
  dynamic the caveat register exists to counter. This is the best *process*
  artefact of the three.
- The credential-bearing-local-config gate (Fable "Credentials", caveat 6): a
  reused checkout whose local config carries bearer carriers
  (`http.extraHeader`, `credential.*`, `url.*.insteadOf`) skips tier B rather
  than either leaking to the mirror or excluding all reused checkouts (which
  is what #4153 did). Narrow, cheap, and honest about the trade
  (mirror-sourced fresh clones, canonical delta fetches thereafter).
- `GIT_NO_LAZY_FETCH=1` on commit-presence probes (Tier B) — a #4144 review
  finding preserved; the other plans drop it (Opus structurally avoids most
  probes, but the flag is still cheap hardening for `hasGitCommit` on partial
  clones).
- The mixed-fleet constraint (caveat 8): a shared on-host mirror must
  interoperate with agents that predate the feature, which is *why* its
  `origin` must remain canonical at all times. Opus's design satisfies this
  but never states the constraint.
- The `--git-clone-mirror-flags` note (Tier A): operator-supplied flags go to
  the mirror on the initial clone; operator config is trust-model territory,
  but it is named rather than silent.

**Assumptions and constraints.** Same trust model as Opus, stated more
compactly ("Trust model and pragmatism"). Assumes mirror providers support
exact-SHA fetches (caveat 5) — see below. Tag/PR/refspec/HEAD builds out of
scope ("Out of scope"). Tier B applies only when no on-host mirror is
configured; routing the checkout fetch through the mirror on hosted fleets is
an explicit follow-up ("Possible follow-ups").

**Weaknesses, risks, unresolved questions.**

- *Caveat 5 is factually wrong for stock v2 servers*, and the design omits
  the one-flag fix. Measured here (T1a–T1c): under protocol v2, default
  server config serves exact-SHA fetches for reachable *and* unreachable
  objects; `uploadpack.allowAnySHA1InWant` matters only under protocol v0.
  Fable never pins `protocol.version=2` on mirror-directed operations, so a
  customer or distribution that pins v0 produces a permanent, silent
  100% miss rate on tier B (and on tier C's post-clone SHA fetch) — tier A's
  branch-refspec refresh and the clone itself still work — exactly the
  configuration Fable R2 says "must disable the mirror attempt up front...
  wherever we can detect them cheaply". Opus §5.4 pins v2 for precisely this
  reason.
- *Tier A writes `refs/heads/<branch>` in the shared mirror from a lagging
  replica.* On the rebuild-of-an-older-commit hit (mirror lags canonical but
  contains the wanted commit), the shared mirror's branch ref is durably
  moved *backwards*, un-referencing objects that canonical had already
  delivered and that concurrent `reference`-mode checkouts may depend on —
  the exact `gc` corruption mode `git-mirror.md` documents. Opus §5.8 avoids
  the class by never touching `refs/heads/*` (namespaced exact-SHA ref;
  verified equivalent for negotiation by T5/G4). Fable does not discuss
  backward moves. This is Fable's most substantive design defect.
- *Tier A ordering skips rename maintenance.* Fable places the mirror fetch
  after `updateRemoteURL` ("Tier A"); a hit then returns before the
  `urlChanged`-gated `fsck`/`gc` (verified present in `checkout_mirror.go`),
  silently skipping the maintenance a rename needs. Opus places the fetch
  before `updateRemoteURL` and argues the ordering (PR 2). Minor (renames are
  rare) but real.
- *Tier C pays one extra mirror round trip on a hit*: after the mirror clone,
  "the standard `fetchSource` runs (with tier B mirror-first for the SHA)"
  (Tier C) — `git fetch` contacts the remote even when the object is local,
  so the hit path re-contacts the mirror. Cheap (the mirror is the fast
  host), and it does avoid the canonical round trip; Opus's §5.7 skip is
  strictly cleaner.
- *Underspecified budget values*: "Exact values to be settled in the
  implementing PR" (The universal pattern) — acceptable, but Opus's
  time-to-first-byte analysis (G14) is the part an implementer actually needs,
  and Fable doesn't have it.

**Where it is more detailed or insightful than the others.** The review
history mapping; the tag-following measurement; credential-carrier handling
for reused checkouts; scope discipline (its "Out of scope" and "Possible
follow-ups" sections are the crispest statement of what is deliberately not
being built). It has the best effort-to-insight ratio of the three documents.

### 2.3 Sol (`remote-git-mirrors-sol.md`)

**Central architecture.** One mechanism everywhere: a command-scoped URL
rewrite, `-c url.<mirror>.insteadOf=<canonical>`, applied to the existing
clone/fetch/materialisation commands, which continue to receive the canonical
URL (Sol "Preserve the canonical repository at stable boundaries"). Durable
state never mentions the mirror; a "source-attempt boundary" wraps one mirror
attempt per checkout with explicit entry points for existing-repository and
fresh-clone cases ("One source-attempt boundary"). Four PRs by path, no
foundations PR.

**Strongest ideas and useful revelations.**

- The `insteadOf` mechanism itself. Verified here (T4): a correctly scoped
  `git -c url.M.insteadOf=C clone C .` transports from the mirror, persists
  canonical `origin`, persists no rewrite, and the promisor follows `origin`
  so later lazy fetches reach canonical. It is the smallest-diff mechanism of
  the three — no `set-url` step, no window where `origin` is the mirror
  (Opus needs C14 for that window), and the on-host mirror's refspec mapping
  and no-tag-follow semantics are preserved automatically because the remote
  is still named `origin` (T2 destination-less behaviour).
- Routing lazy/materialisation fetches through the mirror (Sol "Path 2" step
  4, "Path 3" step 2). For a blobless reused checkout, the per-job bulk is
  the blobs materialised at `git checkout`, and under Opus/Fable those come
  from canonical. Sol is the only plan that covers them. This is a genuine
  coverage gap in the recommended plan, recorded as a follow-up in §6.
- The cancellation rule: fallback runs only "if the mirror failed and the
  parent context is still active"; a cancelled job must not trigger a fresh
  canonical attempt ("Fail open without multiplying retries", test-matrix row
  "cancelled checkout"). Opus and Fable get this only implicitly via context
  inheritance; Sol makes it a testable requirement.
- The product-contract framing: assumptions stated as contract ("Product
  assumptions"), and the promotion rule — "a caveat can become a requirement
  when it is observed in ordinary customer configurations; it should not
  become implementation complexity solely because a pathological
  configuration can be constructed" ("Caveat register"). The best one-sentence
  governance rule in the three documents.
- Immutable-commit wording ("a full object ID for the repository's hash
  format, or an equivalent immutable value attested by the backend",
  "Correct checkout") — future-proofs SHA-256 repositories where the other
  two hard-code "40-hex".
- Caveat 10: removing the mirror URL does not purge already-cached objects —
  true, unstated elsewhere, and the kind of thing an operator asks during an
  incident.
- The rollout section is the most operationally complete: kill switch,
  per-path enablement, telemetry questions phrased as questions
  ("Observability", "Rollout").

**Assumptions and constraints.** Same trust model, stated as five product
assumptions. Eligibility is broader than the other two: tag, PR, and custom
refspec builds stay eligible using today's refspec selection, falling back if
the mirror lacks the ref ("Correct checkout"). Requires an agent-side kill
switch ("Configuration and binding"). No foundations PR ("A foundations-only
pull request is not required").

**Weaknesses, risks, unresolved questions.**

- *The flat cap.* "Give the mirror a short, mirror-specific sub-deadline...
  Start with no mirror retries and a conservative cap such as 30 seconds when
  there is no parent deadline. When a parent deadline exists, use at most
  half of its remaining time" ("Fail open without multiplying retries"). The
  attempt boundary covers clones, so this caps bulk transfers at 30 seconds —
  burning the fast host's transfer and re-cloning from the slow one, worse
  than no mirror at all for the target workload. Opus §5.3 documents this as
  its own first draft's error, and Fable's item 12 independently rejects
  #4153's identical rule. Sol also revives the reserve-a-fraction arithmetic
  ("half of its remaining time") that Opus discards as not actionable. This
  is Sol's most consequential defect, and it is not incidental — it follows
  from treating "the mirror attempt" as one uniform bounded thing, which is
  also what makes the design attractive.
- *Measured foot-guns in the mechanism* (appendix T4): the rewrite must be
  `git -c ... clone`, because `git clone -c ...` is clone's `--config` and
  **persists the rewrite into the new repository while also applying it** —
  an easy transposition that silently violates Sol's own "must not persist"
  requirement; and plain local-path URLs bypass the rewrite entirely (clone
  fails before remote resolution), which matters for the test suites all
  three plans lean on. Neither hazard is mentioned.
- *Rewrite-precedence interactions.* Because the command line carries the
  canonical URL, a customer's own `url.*.insteadOf` whose base matches
  canonical (the GitHub-token pattern, e.g.
  `url.https://x-access-token:…@github.com/.insteadOf=https://github.com/`,
  present in ambient config on real CI hosts) now competes with the agent's
  rewrite under Git's longest-match rule. Opus/Fable's explicit-mirror-URL
  commands bypass canonical-matching rewrites entirely. Sol does not discuss
  this.
- *Shared-ref regression in Path 1.* Fetching today's branch refspec through
  the rewrite writes the mirror's lagging branch value into the shared
  on-host mirror's `refs/heads/<branch>` — the same backward-move hazard as
  Fable's tier A, here on every lag window (the canonical fallback then
  corrects it, except on the rebuild-of-older-commit hit where it doesn't).
- *Materialisation fallback is underspecified.* "If the fetch or
  materialization shows that the mirror is behind, rerun the source-dependent
  operation without the rewrite" (Path 2) — but the failure it must catch can
  be silent: Opus PR 3 measured `git checkout` exiting 0 after
  `error: unable to read sha1 file` in a mispointed-promisor state. Making
  materialisation mirror-scoped multiplies the states this recovery must
  handle, and Sol gives it one sentence.
- *No error-classification telemetry.* Sol infers miss-versus-error from a
  presence check after the attempt ("Fallback details"), avoiding `gitError`
  changes. Coherent, but it cannot distinguish "mirror lagging" from "mirror
  broken" in Opus R9's terms, which is the distinction rollout monitoring
  needs (Opus §2 R9; Fable PR 1's `git.remote_mirror.result`).
- *Credential mechanics are thin.* Sol never specifies the per-invocation
  `credential.helper` reset, `credential.useHttpPath`, or a protocol pin
  outside packfile-URI ("Trust and credential model") — the concrete flags
  both other plans agree on.

**Where it is more detailed or insightful than the others.** Operational
rollout and observability framing; test-matrix breadth (cancellation,
option-injection row, skip-update row); the review-strategy section written
explicitly for the bot-review dynamic ("Ask reviewers whether an issue can
violate the stated product assumptions before adding defensive code");
coverage thinking (lazy fetches, materialisation) that the other two plans
simply don't have.

---

## 3. Comparison matrix

Grades: **S** strong, **A** adequate, **W** weak — relative to the other two
plans, with the reason. "Verified" means checked against code or measured for
this review.

| Criterion | Opus | Fable | Sol |
| --- | --- | --- | --- |
| **Correctness** | **S** — every load-bearing claim tested here reproduced (T1a–T7; code checks §2.1). Unverified residue is timing/HTTP-specific (appendix). | **A** — architecture sound; caveat 5 wrong for stock v2 (T1a–T1c), missing protocol pin contradicts Fable R2; tier A backward ref moves unexamined (§2.2). Tag-follow claim verified correct (T2). | **A** — mechanism verified viable (T4), but the flat 30s cap caps the bulk transfers the feature exists for (RA2); two measured mechanism foot-guns unmentioned (RA1); materialisation fallback trusts an exit code that can be 0 on failure (§2.3). |
| **Simplicity** | **A** — three site-specific mechanisms, each individually simple; the *document* is not simple, and PR 1 concentrates complexity. | **S** — smallest conceptual delta: substitute the URL at three existing transfer points; lightest foundations PR. | **A** — one rewrite everywhere and no `set-url`, offset by the boundary machinery ("source-attempt boundary", fetch selection/execution split) layered on top (§2.3). |
| **Maintainability** | **S** — caveat register with named comment sites; shared helper contracts fixed before use; file-extraction plan for the 342-line `defaultCheckoutPhase` (PR 4). | **A** — caveat register present; dispositions link review threads to code comments; less prescriptive about where logic lives. | **A** — "one boundary" is a good shape, but the promised fetch-dispatcher refactor (PR 2) is the kind of extraction that grows; caveat-promotion rule is excellent governance. |
| **Operational risk** | **A** — no kill switch beyond backend unset + `--allowed-repositories`; strongest telemetry design (`notReached`, dual sinks — verified that span-only telemetry is invisible on default fleets). | **A** — same rollout controls; good trace attributes; silent 100%-miss risk under v0 pinning is an operational blind spot its telemetry would catch but not prevent. | **S** on paper — kill switch, per-path enablement, explicit telemetry questions; **W** on the cap (a fleet-wide 30s clone ceiling is itself an incident); net **A**. Backend emission is currently unconditional (`app/models/job/environment.rb`), which strengthens Sol's kill-switch case — see §8 Q1. |
| **Migration safety** | **S** — every PR inert without the backend value; no canonical-path behaviour change except one argued narrow case (protocol-v0 + local-path fetch classification, PR 1); mixed-fleet safe (origin never rewritten). | **S** — same shape; classification opt-in per invocation is even more conservative than Opus's global change; mixed-fleet constraint stated explicitly (caveat 8). | **A** — durable state untouched by construction (verified), but Path 1's shared-ref writes from a lagging replica are a cross-version behaviour change for concurrent readers of the shared mirror. |
| **Performance** | **S** — hit skips the canonical round trip (§5.7); zero-object reference clone verified (T5); stall-guard preserves bulk transfers; residual: blobless materialisation still canonical, C11's deliberate round trip. | **A** — hit avoids canonical but tier C re-contacts the mirror once; tier B excluded on hosted fleets (the highest-traffic case) by design; stall-guard preserved. | **A**, with the widest spread of the three — the cap is a defect on big-repo cold paths (the feature's core case, RA2), while the materialisation routing is the best blobless-reuse coverage of any plan (F2). |
| **Implementation effort** | **A** — most total work up front (threading, argv plumbing, error type, telemetry all in PR 1), but each later PR is small and the traps are pre-cleared. | **S** — least up-front work; each tier is a contained diff; effort deferred to implementers for budget values and tier A ordering. | **A** — mechanism is a small diff, but the fetch selection/execution extraction (PR 2) and materialisation-scoped rewrite are invasive in `fetchSource`/checkout, the code the other plans deliberately leave structurally unchanged. |
| **Reversibility** | **S** — per-PR revert clean; decision resolves to `none` without the env var; no persisted state anywhere (C14's window self-heals). | **S** — same; tier D independently droppable. | **S** — same, plus the kill switch; minus: objects/refs already written to shared mirrors by Path 1 persist (its caveat 10 acknowledges the general form). |
| **Future flexibility** | **A** — eligibility clauses designed to be relaxed (§5.2, §11); attempt struct extends to new sites; packfile/bundle-URI deferred with both options named (§11). | **A** — follow-ups section names the same extensions; tier D sketched. | **S** — the uniform rewrite extends naturally to materialisation, submodule mirrors (per-URL rewrites), and future transports; hash-format-agnostic eligibility wording. |

---

## 4. Disagreements, complements, false friends, unique insights

### 4.1 Genuine disagreements (must pick one)

Where a decision has an expanded entry in §7, the rationale lives there once;
the item here states the disagreement and the outcome.

1. **Transport mechanism.** Explicit mirror-URL commands + `set-url`
   (Opus §5.6; Fable "Tier C") versus command-scoped
   `url.<mirror>.insteadOf=<canonical>` (Sol "Preserve the canonical
   repository..."). Both verified workable (T4, T7). Decided for
   explicit-URL — grounds in RA1.
2. **Bulk-transfer budget.** Stall-guard, no wall clock (Opus §5.3; Fable
   item 12) versus flat 30s cap / half-remaining-deadline (Sol "Fail open
   without multiplying retries"). Decided for the stall guard — grounds in
   RA2.
3. **On-host mirror ref writes.** Namespaced exact-SHA ref, `refs/heads/*`
   never touched (Opus §5.8) versus branch-ref writes from the mirror (Fable
   "Tier A"; Sol "Path 1" via rewrite). Decided for the namespaced ref —
   grounds in RA3. Consequence, accepted: `refs/heads/*` in the mirror stop
   advancing on steady-state hits (Opus C8).
4. **Mirror fetch placement in `updateGitMirror`.** Before `updateRemoteURL`
   (Opus PR 2) versus after (Fable "Tier A"). Decided for before — grounds
   in RA4.
5. **Packfile-URI scope.** In the stack (Fable R6/Tier D/PR 5; Sol "Path 4")
   versus deferred pending provider answer and a measurement (Opus §11).
   Decided for deferral — see F1; Sol Path 4 is the design sketch of record
   for the eventual PR (its "let Git own the packs, https only, no agent
   downloader" rules are correct and complete).
6. **Agent-side kill switch.** Required (Sol "Configuration and binding") vs
   none (Opus §9) vs if-asked-for (Fable "Possible follow-ups"). Not decided
   here — sharpened and returned to humans (§8 Q1), with the new fact that
   backend emission is currently unconditional.
7. **Existing-checkout fetch when an on-host mirror is configured.** Never
   (Opus §5.1 mutual exclusivity, C11) / no, follow-up (Fable "Tier B") /
   yes (Sol source-selection table). Decided for Opus/Fable's restraint
   initially; Sol's position becomes §6's follow-up F2 with a data gate.
8. **Eligibility breadth.** Tag/PR/refspec builds excluded (Opus §5.2; Fable
   "Out of scope") versus included with fallback (Sol "Correct checkout").
   Decided for exclusion: Fable's argument is the operative one — a provider
   that doesn't replicate `refs/pull/*` converts every PR build into a paid
   miss, systematically violating the never-slower-than-no-mirror rule both
   Opus R2 and Fable R2 state; relax when §8 Q3 is answered.
9. **Miss classification.** New `gitError` type smelting the two miss
   strings — "smelting" is Opus's coinage for matching Git's stderr text to
   classify an error — (Opus PR 1; Fable PR 1, opt-in per invocation) versus
   no classification, presence-check only (Sol "Fallback details"). Decided
   for classification — grounds in RA6. Fable's opt-in variant and Opus's
   global variant differ marginally; Opus's global change is accepted
   because its one behavioural delta (protocol-v0 + local-path canonical
   breaking on the first attempt instead of retrying ten times) is an
   improvement and is documented (Opus PR 1).
10. **First-attempt-only mechanism.** Derived from the attempt count already
    passed to `defaultCheckoutPhase` (Opus §5.1/§5.2; Fable eligibility)
    versus persisted executor state (Sol "One source-attempt boundary" step
    5). Decided for derivation: same behaviour, no new job-lifetime state,
    and Opus documents why executor state is the trap (per-attempt semantics
    masked).
11. **Foundations PR.** Yes (Opus PR 1, heavy; Fable PR 1, light) versus no
    (Sol "Proposed pull request stack"). Decided for yes: two later PRs
    share the bounded-fetch helper and `fetchSource` threading, and
    "introduced by whichever lands first" is how shared contracts drift
    (Opus §6). §6 amends the split to keep any single review tractable.
12. **Creation-arm lag healing.** When the initial on-host mirror clone comes
    from a lagging mirror, Fable ("Tier A") and Sol ("Path 1", new local
    mirror step 5) fetch the missing delta into the shared mirror from
    canonical in the same job; Opus (PR 2 creation arm) deliberately does
    not — the checkout's `--reference` clone gets its delta from canonical
    and the shared mirror heals on the next job's update arm. Decided for
    Opus's restraint: the checkout is correct either way, the extra
    canonical fetch would run inside the creation lock, and the mirror
    converges one job later. Low stakes; revisit only if telemetry shows
    fresh-cache-volume jobs repeatedly paying the same delta.

### 4.2 Compatible, complementary ideas (adopted into §6)

- Fable's verified `--no-tags` rationale (T2) → recorded as the reason on
  Opus PR 2's update-arm flag.
- Fable's mixed-fleet caveat 8 and `--git-clone-mirror-flags` note → new
  entries in Opus §10.
- Fable's `GIT_NO_LAZY_FETCH=1` on presence probes → cheap hardening for
  `hasGitCommit` against partial clones.
- Fable's "documentation ships alongside PR 2".
- Sol's cancellation rule and its "cancelled checkout" / "option-like URL"
  test rows → added to the PR test matrices.
- Sol's caveat 10 (cache contents survive mirror removal) → Opus §10.
- Sol's low-cardinality telemetry rule ("no URLs in metric labels") →
  A8.
- Sol's review-strategy items (before/after command sequences per PR;
  "can this violate the product assumptions?" as the review question) and
  Fable's item-13 reviewer-sign-off flag on the allowlist semantics change →
  A6's PR-description conventions.

### 4.3 Superficially compatible, not safely combinable

- **Sol's rewrite + Fable's credentialish `--config` deferral.** Under Sol
  the clone command carries the canonical URL, so deferred-then-reapplied
  `url.*.insteadOf` keys and the agent's own rewrite compete under
  longest-match precedence; under Fable/Opus the mirror URL never matches
  customer canonical-keyed config. Combining the two credential strategies
  produces config whose winner depends on URL string lengths. Pick one
  transport model and its matching credential strategy.
- **Sol's cap + Opus/Fable's stall guard.** "Wall-clock cap with fraction
  reservation" and "no wall clock, stall-guard only" are contradictory budget
  philosophies for the same command; averaging them (e.g. cap at 5 minutes)
  recreates both failure modes.
- **Fable's tier A branch-ref write + Opus's §5.7 hit-skip.** Fable's tier A
  keeps the shared mirror's `refs/heads/*` advancing (from the mirror);
  Opus's C8 accepts them not advancing because the canonical fetch is
  skipped. Adopting Opus's skip while keeping Fable's ref write yields the
  backward-move hazard *without* the freshness benefit that motivated it.
  The ref-write model and the skip model must be chosen as a pair.
- **Sol's "checkout also prefers the remote mirror" after an on-host refresh
  + Opus's one-attempt accounting.** Two mirror-directed operations per
  checkout breaks Opus R2's "at most one bounded attempt" arithmetic and
  Opus §5.1's single-site telemetry; adopting Sol's coverage requires
  re-deriving the budget and the telemetry model, which is why it is a gated
  follow-up, not a merge.
- **Sol's no-classification stance + the miss/timeout/error precision that
  Opus R9 requires (and Fable's PR 1 trace attributes provide).** Presence
  checks can say hit/miss but not miss/timeout/error; keeping both means the
  boundary owns outcomes while `gitFetch` owns classification — workable only
  if one of them is authoritative (in §6, the shared helper is).

### 4.4 Unique insights that would otherwise be lost

- **Opus:** the retarget-don't-unset promisor repair with its measured
  silent-corruption mode (PR 3); the LFS smudge analysis with the measured
  pointer-stub corruption (PR 4); `notReached`-as-zero-value and the
  defer-ordering trap (§5.1); the nil-`jobLogs` mechanics (§5.2); the argv
  re-splitting hazard with its existing production instance (§5.5); C2's
  exit-zero-*and*-presence hit rule; C14's self-healing kill window; G14's
  time-to-first-byte argument for the stall-guard value.
- **Fable:** the tag auto-follow asymmetry (T2-verified); the
  credential-carrier gate for reused checkouts; the mixed-fleet constraint;
  the reviewer-sign-off flag on the allowlist semantics change (item 13);
  `GIT_NO_LAZY_FETCH`.
- **Sol:** materialisation/lazy-fetch coverage for blobless reuse; the
  cancellation-fallback rule; hash-format-agnostic eligibility wording;
  caveat 10; the caveat-promotion governance rule; the packfile-URI
  ownership rules (Git owns packs, https only).

Each of these has an explicit disposition. Opus's items are in the plan of
record already. Fable's: tag auto-follow → A2; mixed-fleet → A3;
item-13 sign-off → A6; `GIT_NO_LAZY_FETCH` → A5; the credential-carrier gate
is deliberately superseded, with the reasoning recorded in RA9. Sol's:
materialisation coverage → F2; cancellation rule → A4; hash-format wording →
A9; caveat 10 → A3; caveat-promotion rule → A10; packfile-URI ownership
rules → F1.

---

## 5. Recommendation

**A modified version of one plan — Opus — not a synthesis.**

A synthesis was considered and rejected. The three plans agree on the
skeleton (real `git clone`, canonical `origin`, fail-open, SHA-pinned
eligibility, on-host mirror first, allowlist-declines-mirror-not-job, no
mirror retries — a striking three-way convergence that itself validates the
skeleton), but they disagree on load-bearing pairs that §4.3 shows cannot be
averaged: transport model ↔ credential strategy, ref-write model ↔ hit-skip
model, budget philosophy. A merged document would preserve each plan's words
and lose each plan's consistency. Choosing a base and importing compatible
items keeps one consistent decision structure with named owners for every
trade-off.

Opus is the base because the review criteria that matter most for this
feature — correctness against real Git behaviour and against the agent's own
retry/cleanup machinery — are where Opus is strongest *by measurement, not by
volume*. The measurements in the appendix were run to control for exactly the
verbosity/confidence bias the reader should worry about with a 1177-line
document: its claims were tested identically to the others', and it was the
only plan of the three where testing found no factual error (it found one
under-claim: T1c). Fable's plan is well-shaped and its historical mapping is
the best process artefact, but it carries one wrong provider requirement, one
missing transport pin its own requirements demand, and one unexamined
shared-state hazard. Sol's plan contains the most interesting single idea
(the rewrite) and the best operational sections, but its budget rule damages
the feature's core case and its mechanism has measured foot-guns it doesn't
name.

---

## 6. Consolidated design and implementation plan

The plan of record is **Opus (`docs/remote-git-mirrors.md` on
`pda/remote-git-mirrors-plan-c275`) with the amendments below.** The
amendments are written as deltas so Opus's internal cross-references stay
valid; they should be folded into that document, not maintained here.

**A1 — Correct the provider-requirement framing (from this review's
T1a–T1c).**
In §4.2 and §11: stock `git-upload-pack` under protocol v2 serves wants for
*any* object it has, reachable or not — `uploadpack.allowAnySHA1InWant` is a
protocol-v0 concern. Rephrase §11's capability question as: "does the mirror
provider's server implementation restrict v2 wants (stock git does not), and
does it advertise `uploadpack.allowFilter`?" Fable's caveat 5 is superseded
by this. The `protocol.version=2` pin in §5.4 stays; it is what makes the
question answerable per-fleet rather than per-git-distribution.

**A2 — Add the tag auto-follow rationale to PR 2 (from Fable "Tier A", T2).**
The update-arm fetch already carries `--no-tags`; record *why*: an explicit
destination refspec auto-follows tags where today's destination-less
`git fetch origin <branch>` does not (measured), so omitting the flag would
import lagged tags into the shared mirror.

**A3 — New §10 caveats (from Fable caveat 8, Fable "Tier A" note, Sol
caveat 10).**
- C16: shared on-host mirrors are used by agents that predate this feature;
  the mirror's `origin` remaining canonical at all times is a compatibility
  contract, not a preference.
- C17: `--git-clone-mirror-flags` is operator config and is passed to the
  mirror-directed creation clone; operator-embedded credentials therefore
  reach the mirror. Trust-model accepted, named.
- C18: unsetting `clone_mirror_url` stops future mirror use but purges
  nothing already cached in checkouts or on-host mirrors; the objects are
  valid content from a customer-controlled replica.

**A4 — Cancellation rule (from Sol "Fail open without multiplying retries").**
In §5.3 and the shared bounded-fetch helper's contract (Opus §6, PR 1): the
canonical fallback runs only if the parent context is still live; a
cancellation that kills the mirror attempt must not spend the cancelled job's
time on canonical. Add Sol's "cancelled checkout → no fallback" row to each
site PR's test list, and its "URL begins with option-like text" row to PR 1's
(the `--` discipline is asserted, not assumed).

**A5 — `GIT_NO_LAZY_FETCH=1` on presence probes (from Fable "Tier B").**
Set it inside `hasGitCommit`. On git ≥ 2.45 it prevents a probe against a
partial clone from lazy-fetching through the promisor; older gits ignore it.
One line, benefits the pre-existing `GitSkipFetchExistingCommits` probe too.

**A6 — PR process (from Opus §6's own "merging 2 and 3" concession, Sol
"Review strategy", and Fable item 13).** Two parts. *Splitting:* if review
size demands, cut PR 1 into PR 1a — the `[]string` plumbing (§5.5) and the
`gitErrorFetchRefNotOnRemote` classification (no behaviour change except the
classification's one documented narrow delta, which needs its attempt-count
assertion) — and PR 1b — config, binding, allowlist drop, resolved decision,
Opus R9's emission, shared helper. Whether to split is a reviewer-capacity call for
whoever reviews it; the plan supports both. Do not let the helper or the
`fetchSource` threading slip out of the foundations layer; that re-creates
the drift Opus §6 warns about. *Descriptions:* each PR description opens with
the one checkout path that changes and a before/after command sequence for
hit and lag fallback (Sol "Review strategy"); reviewers should ask whether a
concern can violate the stated trust model before requesting defensive code
(Sol, and Fable item 14); and PR 1's description explicitly flags the
allowlist semantics change from #4153 for reviewer sign-off (Fable item 13).

**A7 — Documentation ships with PR 2 (from Fable "Proposed PR stack").**
Agent docs and buildkite/docs updates land with the first behaviour-changing
PR, updated per site thereafter.

**A8 — Telemetry hygiene (from Sol "Observability").** Opus R9's span
attributes and log line carry outcomes, durations, and skip reasons only —
never URLs in metric-shaped fields; URLs appear in the job log via
`redact.URLCredentials` as §3 already requires.

**A9 — Hash-format-agnostic eligibility wording (from Sol "Correct
checkout").** In §5.2, note that the 40-hex clause is the SHA-1
instantiation of "a full object ID for the repository's hash format"; if the
agent grows SHA-256 repository support, the clause widens rather than the
requirement changing.

**A10 — Caveat-promotion rule (from Sol "Caveat register").** Add to §10's
preamble: a caveat is promoted to a requirement when it is observed in
ordinary customer configurations; it does not become implementation
complexity solely because a pathological configuration can be constructed.

**Follow-ups, explicitly not in the initial stack, each with its gate:**

- **F1 — packfile-URI / bundle-URI** (Fable Tier D, Sol Path 4, Opus §11).
  Gate: provider answers which mechanism it serves; a landed PR 4 to measure
  against. Sol Path 4's rules (Git owns download/verify/retention; `https`
  only; no agent-side eligibility parsing) are the sketch of record.
- **F2 — route blobless materialisation through the mirror** (Sol Path 2
  step 4 / Path 3 step 2). Gate: Opus R9 telemetry showing meaningful
  lazy-fetch volume from canonical on mirror-hit jobs (sparse pipelines on
  long-lived agents). Implementation note from T4: a command-scoped
  `-c url.<mirror>.insteadOf=<canonical>` on the materialisation commands
  composes with Opus's architecture (the promisor stays `origin`); it must
  be `git -c`, never `clone -c`/`--config`, and it inherits §5.4's transport
  flags. The silent-materialisation-failure mode (Opus PR 3's measured
  exit-0 case) must get an explicit post-checkout presence assertion before
  this ships.
- **F3 — relax eligibility to PR/tag builds** (Sol "Correct checkout").
  Gate: §8 Q3 (provider replicates `refs/pull/*` / tags).
- **F4 — agent-side kill switch** (Sol "Configuration and binding").
  Gate: §8 Q1's answer; if the backend adds an emission-side kill, F4 is
  closed as unnecessary (Opus §9 stands).

Everything else in Opus stands as written, including the decisions this
review examined and endorses explicitly: the resolved-decision struct and
`notReached` zero value (§5.1); eligibility including its deliberate
over-breadth (§5.2, with the relaxation note kept and A9's wording); shaped
budgets (§5.3); transport/credential flags including the v2 pin (§5.4);
clone-then-`set-url` (§5.6 — shape parity verified in T7, the
promisor-follows-origin kernel in T4); hit-skips-canonical-fetch with its two
site-specific expressions (§5.7); namespaced sanitised refs (§5.8, T5);
the four-PR shape with PR 2 first (§6); assert-on-bytes testing (§8);
no experiment / backend-gated rollout (§9, pending Q1); the caveat register
(§10) plus A3's and A10's additions; and §11's open questions as amended by
A1.

---

## 7. Rejected alternatives

Numbered RA1–RA9 to avoid colliding with the plans' own requirement numbers.
These are the canonical statements of each rejection's grounds; §4.1 points
here.

**RA1 — Sol's command-scoped `insteadOf` as the transport mechanism**
(Sol "Preserve the canonical repository at stable boundaries"). Verified
working (T4): transport from mirror, canonical `origin` persisted, nothing
durable written, promisor follows `origin`. Rejected because: (a) the
`git -c` / `clone --config` transposition silently persists the rewrite —
measured — and nothing in review tooling would catch it; (b) plain-path URLs
bypass the rewrite before clone's existence check — measured — which poisons
the integration-test strategy every plan relies on; (c) with the canonical
URL on the command line, customer `url.*.insteadOf` config keyed on canonical
competes under longest-match precedence, a class of interaction the
explicit-URL design structurally avoids; (d) its uniformity invites applying
one budget to all command shapes, which is how Sol's cap defect arose. The
mechanism remains the recommended tool for F2, where the command already
carries the canonical URL and no clone is involved.

**RA2 — flat wall-clock cap on all mirror attempts** (Sol "Fail open without
multiplying retries"). Rejected: caps the bulk transfer the feature exists to
accelerate; both other plans reject it independently (Opus §5.3 as its own
corrected first draft; Fable item 12 against #4153's version); Opus's
G12/G14 measurements — not reproduced here, but consistent with documented
curl behaviour — show the stall guard bounds the actual failure (hangs) in
the right currency.

**RA3 — writing `refs/heads/<branch>` in the on-host mirror from the mirror**
(Fable "Tier A"; Sol "Path 1"). Rejected for the backward-move/unreferencing
hazard on rebuild hits and lag windows (§2.2); T5 shows the namespaced ref
achieves the same negotiation effect with no shared-ref writes. Cost
accepted in exchange: mirror `refs/heads/*` staleness on steady-state hits
(Opus C8).

**RA4 — mirror fetch after `updateRemoteURL`** (Fable "Tier A"). Rejected:
verified that an early return there skips the `urlChanged` `fsck`/`gc`
maintenance; Opus PR 2's placement is structurally identical to the existing
short-circuit exit.

**RA5 — packfile-URI in the initial stack** (Fable R6/PR 5; Sol Path 4).
Deferred, not refused — the grounds and the design sketch of record are F1.

**RA6 — no miss classification, presence-check-only outcomes** (Sol "Fallback
details"). Rejected: forfeits Opus R9's miss/timeout/error distinction, the
main rollout instrument; the classification cost is two smelt strings and a
`Break()` (verified against `gitFetch`'s retrier structure).

**RA7 — #4153's `git init` + fetch acquisition, and RA8 — checking the
mirror URL in `validateConfigAllowlists` (also #4153).** Neither is proposed
by any of the three plans; both shipped in #4153, so the unanimous reversal
is recorded. RA7: all three plans independently conclude that reproducing
`git clone`'s flag semantics by hand was the generative source of the
#4144/#4153 review spiral (Opus §5.6/§7; Fable "Tier C"; Sol "Summary").
RA8: all three decline the mirror instead of the job (Opus §5.2; Fable
eligibility; Sol "Configuration and binding"); verified here that the
existing path refuses the job with `SignalReasonAgentRefused`, making #4153's
version a fleet-outage-by-backend-setting. A6 carries Fable's item-13
sign-off note for this change.

**RA9 — Fable's credential-carrier gate for reused checkouts** (Fable
"Credentials", caveat 6): skip the mirror when a reused checkout's local
config contains `http.extraHeader`, `credential.*`, or `url.*.insteadOf`.
Superseded rather than adopted, deliberately: Opus §5.4's per-invocation
flags already neutralise the carriers that can leak a canonical bearer
credential to the mirror — `-c credential.helper=` resets inherited helpers
including URL-scoped ones, `-c http.extraHeader=` resets the unscoped header
list (Opus G8), and a URL-scoped `http.<url>.extraHeader` or an `insteadOf`
can only reach the mirror if its base matches the mirror URL, which requires
the customer to have keyed config on the mirror deliberately (Opus C5's
topology argument, §3's trust model). Opus's resets keep the optimisation
where Fable's gate forfeits it. Residual difference accepted: Fable's gate
also declines the mirror under exotic carriers Opus does not reset; those
fall under Opus C6's accepted ambient-config inheritance.

---

## 8. Open questions requiring human decisions

**Q1 — Kill switch and emission gating (decides F4; Opus §9 vs Sol
"Rollout").** The backend currently emits `BUILDKITE_GIT_REMOTE_MIRROR_URL`
unconditionally when `project.clone_mirror_url` is present
(`app/models/job/environment.rb`); `Feature::PipelineCloneMirror` gates who
can *set* the setting, not whether it is *emitted*. In an incident affecting
many pipelines (e.g. a mirror provider outage making every eligible job pay a
bounded miss), the current controls are per-pipeline data changes or an agent
fleet upgrade. Decide: (a) add an emission-side backend kill (flag check in
`Job::Environment` — recommended by this review; closes F4), (b) add Sol's
agent-side `--no-git-remote-mirrors` flag, or (c) accept bounded degradation
as not incident-worthy. Owner: backend + agent leads jointly.

**Q2 — Staleness contract (Opus §11).** Can the backend advertise the mirror
URL only when it believes the commit has replicated? This flips a miss from
"expected, must be cheap" to "alertable bug" and would let eligibility and
budget machinery shrink. Needs a backend/provider answer before PR 3/PR 4
tuning.

**Q3 — Capability parity per provider (Opus §11 as amended by A1; Fable
caveat 5; Sol "Correct checkout").** For each supported mirror provider:
does its server restrict protocol-v2 wants; does it advertise
`uploadpack.allowFilter` (C3's silent full transfer otherwise — measured);
does it replicate `refs/pull/*` and tags (gates F3); does it proxy LFS
(Opus C10)? The `repository_access_token` mint path is Cursor-Origin-specific
today (verified in `Job::CodeAccessTokenIssuer`); any second provider needs
the same confirmation (Opus §5.4).

**Q4 — Mirror URL rotation contract (Opus §11; Sol "Configuration and
binding").** May `clone_mirror_url` change between jobs of one pipeline?
Both plans key the on-host mirror by canonical URL so rotation cannot
fragment the cache; a rotation contract still affects credential caching and
telemetry interpretation. Backend to confirm.

**Q5 — Blobless materialisation coverage (F2; Sol Path 2/3 vs Opus C10
scope).** Is the sparse/blobless-on-long-lived-agents population large enough
to justify F2's added recovery complexity? Answer from Opus R9 telemetry
after PR 3 has been in production; product/infra call, not an agent-code
call.

---

## Appendix: verification record

Empirical tests run for this review on git 2.43.0 (Linux), local and
`file://` transports. **Not reproduced here** (this list is the single
authority the body defers to): Opus G8/G9 (HTTP header reset), G12–G14
(low-speed timer behaviour), G2's exit-128-over-HTTP half, the LFS smudge
measurements (Opus PR 4), and Opus PR 2's full end-to-end object-count table
(its mechanism is T5). All of these need HTTP or timing fixtures; each is
consistent with documented git/curl behaviour but carries only Opus's
measurement.

| # | Test | Result | Bears on |
| --- | --- | --- | --- |
| T1a | `git -c protocol.version=2 fetch <url> <full-sha>` for a reachable, **non-advertised** commit; default server config | succeeds | Opus G1 confirmed; Fable caveat 5 refuted for stock v2 |
| T1b | same under `protocol.version=0` | fails, `Server does not allow request for unadvertised object`, exit 1 (local transport) | Opus G2 confirmed for local transport, exit code included |
| T1c | same as T1a but the SHA is **unreachable** (pushed then branch deleted) | succeeds under v2, default config | stronger than Opus G1/§11's claim; stock v2 imposes no reachability check |
| T2 | in a `--mirror` repo lagging its source: destination-less `git fetch origin main` vs `git fetch <url> +refs/heads/main:refs/heads/main` after a new tag appears upstream | destination-less: tag **not** fetched; explicit refspec: tag auto-followed | Fable "Tier A" `--no-tags` rationale confirmed |
| T3 | `git clone --no-local --filter=blob:none` against a server without `uploadpack.allowFilter` | warning, exit 0, **full transfer** (9/9 objects), `remote.origin.promisor`/`partialclonefilter` still written | Opus G10 / C3 confirmed |
| T4 | `git -c url.M.insteadOf=C clone C .` (file://), canonical unreachable; then partial-clone variant with the mirror deleted post-clone | transport from mirror; `remote.origin.url` = canonical; no rewrite persisted; lazy blob fetch succeeds from canonical | Sol's mechanism viable (its "Preserve the canonical repository..." claims hold) |
| T4-fg1 | same but `git clone -c url.M.insteadOf=C ...` (clone's `--config`) | works **and persists the rewrite** in the new repo's config | Sol foot-gun: violates its own no-persistence rule; unmentioned |
| T4-fg2 | same as T4 with plain local paths instead of `file://` | clone fails `repository does not exist` before the rewrite applies | Sol foot-gun: transport-dependent; affects test fixtures |
| T5 | bare repo holding objects only under `refs/buildkite-agent/remote-mirror/main`, used as `--reference` for a clone of canonical | 0 objects transferred (control without warm reference: 9) | Opus G4/§5.8/PR 2 zero-object claim confirmed |
| T6 | `git -c protocol.version=2 fetch <url> <sha-the-server-lacks>` | `fatal: remote error: upload-pack: not our ref <sha>`, exit 128 | Opus G3 confirmed — the miss string PR 1's classification smelts |
| T7 | clone the mirror, `git remote set-url origin <canonical>`, then fetch+checkout a commit only canonical has; flag matrix: none, `--depth=2`, `--filter=blob:none`, `--single-branch`, `--no-tags` | `remote.origin.{fetch,tagopt,promisor,partialclonefilter}` and shallow state match a canonical clone in every row; fetch+checkout of the canonical-only commit succeeds in every row | Opus G6 confirmed — the §5.6 clone-then-`set-url` shape-parity claim |

Codebase checks (all confirmed; file references as of `main` = `3a1d970a`):
checkout retrier and wipe arm, `removeCheckoutDir` 10×10s loop,
`GitCheckoutTimeout<=0` skipping `WithTimeout` (`internal/job/checkout.go`);
`hasGitCommit` short-circuit placement, `updateRemoteURL`/`urlChanged`
`fsck`+`gc` ordering, `--git-dir=%s` string `GitFlags`
(`internal/job/checkout_mirror.go`); `skipFetch` expression above the refspec
switch, `refspecCommit` fetch below it (`internal/job/checkout_fetch.go`);
smelt strings lacking `not our ref`, 10-attempt retrier,
`gitFetchWithFallback` switch, `gitClone` argv shape, `gitCheckRefFormat`
unimplemented rules 1/2/5/6/8 (`internal/job/git.go`); `verifyCommit` skip
conditions and `checkCommitOnBranch`'s canonical branch-tip fetch
(`internal/job/commit_verification.go`); `validateConfigAllowlists` →
`SignalReasonAgentRefused`, `validateJobValue` (`agent/run_job.go`);
`createEnvironment` runs before `r.jobLogs` is assigned,
`delete(env, "BUILDKITE_AGENT_TOKEN")` (`agent/job_runner.go`); no-op span
default (`tracetools/span.go`); `errNotHTTPS`
(`clicommand/git_credentials_helper.go`); backend emission and flag scope
(`app/models/job/environment.rb`, `app/models/feature/pipeline_clone_mirror.rb`),
clone-mirror token mint path (`app/models/job/code_access_token_issuer.rb`).
