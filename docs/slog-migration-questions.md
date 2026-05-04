# slog + tint Migration — Open Questions

This doc captures decisions needed before/while migrating the custom
[`logger`](../logger) package to `log/slog` (with
[`tint`](https://github.com/lmittmann/tint) for human-readable output and
`slog.JSONHandler` for JSON output).

For each question:
- **Context** — what the code does today and where.
- **Options** — concrete choices.
- **Recommendation** — a default if you don't have a strong opinion.
- **Decision** — fill in your answer here.

---

## 1. Logger plumbing & the standard `slog` default

### 1a. How is the logger passed around?
**Context:** Today, `logger.Logger` is plumbed explicitly through ~123 sites
(struct fields, function args). slog supports both an explicit
`*slog.Logger` and a process-global `slog.Default()` set via
`slog.SetDefault`.

**Options:**
- (A) Keep explicit plumbing; pass `*slog.Logger` everywhere.
- (B) Switch to `slog.Default()` everywhere.
- (C) Hybrid: set a sensible default, but keep explicit plumbing for
  long-lived components (agent worker, job runner).

**Recommendation:** (C). Matches current pattern; keeps tests deterministic.

**Decision:** C

---

### 1b. Keep an internal logger interface?
**Context:** Many structs accept the `logger.Logger` interface today. We
could replace that with a concrete `*slog.Logger` (slog's `Handler`
provides the same swap-for-tests seam).

**Options:**
- (A) Use `*slog.Logger` directly everywhere.
- (B) Keep a thin internal interface (`type Logger interface{ ... }`)
  for backward compat / easier mocking.

**Recommendation:** (A). slog's `Handler` is the right seam.

**Decision:** A

---

### 1c. `context.Context` propagation
**Context:** slog's idiomatic API is `InfoContext(ctx, msg, args...)`,
which lets handlers read trace IDs etc. from context. Many call sites in
[`agent/`](../agent/) already have a `ctx` in scope; some don't.

**Options:**
- (A) Always use `*Context` variants where `ctx` is available; plain
  variants otherwise.
- (B) Always use plain variants (ignore ctx-aware logging).
- (C) Plumb `ctx` to every log site (largest diff).

**Recommendation:** (A). Best-effort; future-proofs for trace
correlation without forcing a refactor.

**Decision:** A

---

## 2. Levels

### 2a. What happens to `NOTICE`?
**Context:** [`logger/level.go`](../logger/level.go) defines
`DEBUG < NOTICE < INFO < WARN < ERROR < FATAL`. Note the **non-standard
ordering**: `NOTICE` is *more* verbose than `INFO`. `NOTICE` is also the
**default level** today (set in
[`global.go`](../clicommand/global.go#L351)) and is user-selectable via
`--log-level notice`.

**Options:**
- (A) Define a custom `slog.Level(-2)` for `NOTICE`, keep ordering and
  default. Backward compatible; quirky.
- (B) Collapse `NOTICE` into `Info`; make `Info` the default. Aligns
  with slog conventions; user-visible behavior change.
- (C) Keep the name `NOTICE` but reorder so `INFO < NOTICE < WARN`
  (matches syslog). Breaks `--log-level` semantics.

**Recommendation:** (A) for a non-breaking migration; (B) if we're
willing to take a breaking change in this release.

**Decision:** B. We're making breaking changes here, so it makes sense to normalise this. IIRC notice is barely used anyway

---

### 2b. What happens to `Fatal`?
**Context:** `logger.Fatal` logs and calls `os.Exit(1)`. slog has no
fatal level.

**Options:**
- (A) Add a small package-level helper:
  `func Fatal(l *slog.Logger, msg string, args ...any) { l.Error(msg, args...); os.Exit(1) }`.
- (B) Inline `Error` + `os.Exit` at each call site.

**Recommendation:** (A). Few call sites; helper keeps intent clear.

**Decision:** A.

---

### 2c. Runtime level changes
**Context:** `--log-level` and `--debug` flags must keep working.

**Options:**
- (A) Use `slog.LevelVar` so flag parsing can mutate the level.
- (B) Re-create the logger when the level changes.

**Recommendation:** (A). Standard slog idiom.

**Decision:** A.

---

## 3. Output format compatibility

### 3a. JSON schema
**Context:** Current JSON keys are `ts`, `level`, `msg`. Default
`slog.JSONHandler` emits `time`, `level`, `msg`. Customers may parse
these.

**Options:**
- (A) Preserve `ts`/`level`/`msg` via `HandlerOptions.ReplaceAttr`.
- (B) Adopt slog defaults; document as breaking change.

**Recommendation:** (A). Preserve schema.

**Decision:** B.

---

### 3b. JSON value typing
**Context:** Today every field is stringified (`"%q":%q`). slog emits
typed JSON (numbers as numbers, bools as bools, durations as strings or
nanoseconds depending on config).

**Options:**
- (A) Accept typed JSON (improvement; minor break for any consumer that
  parsed strings).
- (B) Force everything to strings via `ReplaceAttr` for compat.

**Recommendation:** (A). Improvement; flag in changelog.

**Decision:** A

---

### 3c. Level names in JSON
**Context:** Today JSON contains `DEBUG`, `NOTICE`, `INFO`, `WARN`,
`ERROR`, `FATAL`. slog emits `DEBUG`, `INFO`, `WARN`, `ERROR` and
`DEBUG-4` etc. for custom levels.

**Options:**
- (A) `ReplaceAttr` to render custom levels with the existing names.
- (B) Use slog defaults.

**Recommendation:** (A) if (2a-A) is chosen; (B) if (2a-B).

**Decision:** B

---

### 3d. Timestamp format
**Context:** Text uses `"2006-01-02 15:04:05"` local time; JSON uses
RFC3339.

**Options:**
- (A) Preserve both.
- (B) Use RFC3339 everywhere.
- (C) Use slog defaults.

**Recommendation:** (A). Minimal user-visible change.

**Decision:** C. Defaults are good.

---

## 4. tint text format & color behavior

### 4a. How closely should colored output match today?
**Context:** [`log.go`](../logger/log.go#L163-L200) colors level,
message body (per-level — red for FATAL), prefix, and field keys
independently. tint colors level + key/value pairs out of the box.

**Options:**
- (A) Accept tint's defaults (cleaner, slightly different look).
- (B) Wrap tint with a small custom handler / `ReplaceAttr` to preserve
  per-level message-body coloring.

**Recommendation:** (A). Easier; tint's look is conventional.

**Decision:** A.

---

### 4b. Color detection
**Context:** Current code uses `term.IsTerminal(os.Stdout.Fd())` plus
[`init_windows.go`](../logger/init_windows.go) for ANSI on Windows. tint
exposes a `NoColor` option.

**Options:**
- (A) Detect TTY + Windows ANSI as today; pass `NoColor: !supported` to
  tint.
- (B) Add `NO_COLOR` env var support
  ([no-color.org](https://no-color.org)) on top of (A).

**Recommendation:** (B). Trivial, standard.

**Decision:** B, so long as both are supported.

---

### 4c. Output streams
**Context:** Text → **stderr**, JSON → **stdout** today (see
[`global.go`](../clicommand/global.go#L323-L345)).

**Options:**
- (A) Preserve asymmetry.
- (B) Send everything to stderr (more conventional for logs).

**Recommendation:** (A). Some users likely pipe `--log-format=json` to
files via stdout redirection.

**Decision:** B.

---

## 5. The `IsPrefixFn` / `IsVisibleFn` mechanism

### 5a. Per-job inline prefix
**Context:** Attributes with key `agent` or `hook` render inline in the
message (e.g., `INFO   agent=instance-1 Some message`) rather than as
trailing `key=value`. Configured in
[`global.go`](../clicommand/global.go#L326-L333) and used by
[`job_runner.go`](../agent/job_runner.go) to tag per-job log lines.

**Options:**
- (A) Drop the special prefix; agent/hook appear as trailing
  `key=value` pairs (tint default behavior).
- (B) Write a thin `slog.Handler` wrapping tint that lifts those
  attributes into the message prefix.
- (C) Bake the `agent=...` string into the message at the call sites
  (no handler magic).

**Recommendation:** (B). Preserves familiar log layout; localized to
the handler.

**Decision:** A. These logs aren't generally for humans (who aren't maintainers of the agent) to read, so i think it's reasonable to make this more idiomatic to go logs.

---

### 5b. Field visibility filtering
**Context:** `IsVisibleFn` lets the printer hide certain fields. Used
together with `IsPrefixFn`.

**Options:**
- (A) Drop visibility filtering.
- (B) Implement via `ReplaceAttr` returning empty `Attr`.

**Recommendation:** Audit current usage first; if unused beyond the
`agent`/`hook` prefix path, drop it.

**Decision:** A. IsVisibleFn is never used.

---

## 6. Migration strategy at call sites

### 6a. Printf-style vs. structured
**Context:** All ~761 call sites use printf style today
(`l.Info("uploaded %s in %v", path, dur)`). slog idiom is structured
(`l.Info("uploaded", "path", path, "duration", dur)`).

**Options:**
- (A) Fully structured everywhere. Largest diff; cleanest result.
- (B) Mixed: structured for stable machine-relevant context (job_id,
  agent, path, count, duration); keep `fmt.Sprintf` for human prose.
- (C) Keep printf style; just call `l.Info(fmt.Sprintf(...))`. Smallest
  diff, but loses most of the value of the migration.

**Recommendation:** (B). Pragmatic middle ground.

**Decision:** B.

---

### 6b. `WithFields` → `With`
**Context:** 22 call sites use `WithFields(StringField("k", v),
IntField("k", n), DurationField("k", d))`. These map cleanly to
`l.With("k", v, "k", n, "k", d)`.

**Options:**
- (A) Mechanical rewrite; remove typed `Field` constructors.
- (B) Keep `Field` constructors as thin wrappers over `slog.Attr` for
  compatibility.

**Recommendation:** (A).

**Decision:** A

---

### 6c. Style: positional args vs. `slog.Attr`
**Context:** slog accepts `"key", value, "key2", value2, ...` or
`slog.String("key", value), slog.Int("key2", value2)`. Mixing is allowed
but error-prone (odd-args bug).

**Options:**
- (A) Positional `"k", v` everywhere (shorter).
- (B) `slog.Attr` everywhere (verbose; type-checked).
- (C) Either, enforced consistently per file.

**Recommendation:** (A) + add `sloglint` to golangci-lint to catch
odd-arg bugs.

**Decision:** A

---

## 7. Test sink

**Context:** [`logger/buffer.go`](../logger/buffer.go) records messages
as `[level] formatted-msg` strings; tests assert on those. There's also
a `TestPrinter` ([log.go#L280-L291](../logger/log.go#L280-L291)) that
calls `tb.Logf`.

**Options:**
- (A) New `slog.Handler` that records `slog.Record`s for assertions
  (move tests to attribute-based asserts).
- (B) Preserve `[level] msg` string contract in a slog handler.
- (C) Provide both: a recording handler + a `tb.Logf` handler.

**Recommendation:** (C). Attribute-based asserts for new tests; keep
string contract during migration to avoid touching every test.

**Decision:**

I think it makes most sense to do something like have a dedicated test logger that records log records passed to it, and optionally outputs those logs using t.Logf

---

## 8. Third-party / boundary concerns

### 8a. Public API impact
**Context:** [`api/`](../api/) and [`core/`](../core/) accept the
current `logger.Logger`. `core/` is a programmatic interface meant for
embedders.

**Options:**
- (A) Change `core/` and `api/` to accept `*slog.Logger`.
- (B) Keep accepting an interface; provide a `*slog.Logger` adapter.

**Recommendation:** (A). Embedders are better off with stdlib types.

**Decision:** A

---

### 8b. External consumers of `logger` types
**Context:** Other Buildkite repos (e.g., `go-pipeline`) may import
`logger.Logger` or `logger.Field`.

**Action item:** Audit reverse dependencies before locking the API.

**Decision:** Few repos do, and nothing should, import the agent repo;. It's fine to break this.

---

## 9. Source location / call-site annotation

**Context:** slog can emit `source` (file:line) via
`HandlerOptions.AddSource`. Current logger doesn't.

**Options:**
- (A) Off by default; on for `--debug`.
- (B) Always on.
- (C) Off always.

**Recommendation:** (A).

**Decision:** A.

---

## 10. Concurrency & ordering

**Context:** Current `Print` holds a global `sync.Mutex` per line
([`log.go#L39`](../logger/log.go#L39)). slog handlers serialize
per-handler. Multiple agents writing to the same `os.Stderr` from
goroutines must not interleave bytes.

**Options:**
- (A) Trust slog's per-handler serialization (text & JSON handlers do
  this).
- (B) Wrap the writer in a locked writer if we mix slog output with
  direct `os.Stderr.Write` from elsewhere.

**Recommendation:** (A); add (B) only if we observe interleaving.

**Decision:** A

---

## 11. Tooling / lint

**Context:** Without enforcement, post-migration call sites tend to
drift (mixed positional/Attr, odd-args bugs).

**Options:**
- (A) Add `sloglint` to `.golangci.yml` with rules: no-mixed-args,
  attr-only or kv-only (see (6c)), no-global-default.
- (B) Document conventions in `AGENT.md` only.

**Recommendation:** (A).

**Decision:** A.

---

## 12. Versioning & release

### 12a. Breaking change strategy
**Context:** Possible user-visible changes: log line format (text),
JSON value typing, `--log-level notice` semantics if (2a-B), default
level if (2a-B).

**Options:**
- (A) Major-version bump (`v5`); CHANGELOG entry.
- (B) Minor release with prominent CHANGELOG entry; only do
  non-breaking choices above.

**Recommendation:** Pick (B) if you choose conservative answers (2a-A,
3a-A, 3d-A); (A) otherwise.

**Decision:** We're currently nutting out v4, this is fine to put into v4.

---

### 12b. Deprecation / opt-out flag
**Context:** A `--log-format=text-legacy` flag could let users keep the
old format for one release.

**Options:**
- (A) Provide opt-out for one release.
- (B) Hard cutover.

**Recommendation:** (B) if changes are minor; (A) if (12a-A).

**Decision:** B.

---

## Summary of decisions to make first

These decisions cascade into the rest:
1. **(6a)** Printf vs. structured — dominates the diff size.
2. **(2a)** What happens to `NOTICE` — drives backward-compat posture.
3. **(3a)** Preserve JSON schema — affects every JSON consumer.
4. **(5a)** Preserve `agent=` prefix layout — affects handler design.
5. **(12a)** Breaking change appetite — frames everything else.
