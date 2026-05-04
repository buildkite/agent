# slog + tint Migration — Implementation Plan

This plan implements the decisions captured in
[`slog-migration-questions.md`](./slog-migration-questions.md). The goal
is to replace the bespoke
[`logger`](../logger) package with `log/slog` (using
[`tint`](https://github.com/lmittmann/tint) for human-readable output
and `slog.JSONHandler` for JSON output), as part of the v4 release.

## 1. Decisions snapshot

| # | Decision |
|---|---|
| 1a | Hybrid plumbing: explicit `*slog.Logger` for long-lived components, `slog.SetDefault` for convenience |
| 1b | Use `*slog.Logger` directly (no wrapper interface) |
| 1c | Use `*Context` slog variants where ctx is in scope |
| 2a | Drop `NOTICE`; collapse the 4 call sites into `Info`. Default level becomes `Info` |
| 2b | Add a `logFatal` package helper: `Error` + `os.Exit(1)` |
| 2c | Use `slog.LevelVar` so `--log-level` / `--debug` mutate the active level |
| 3a | JSON schema follows slog defaults (`time`/`level`/`msg`) — **breaking** |
| 3b | Typed JSON values (slog default) |
| 3c | Default slog level names (`DEBUG`/`INFO`/`WARN`/`ERROR`) |
| 3d | Default slog timestamp formats |
| 4a | Use tint defaults for color |
| 4b | TTY auto-detect + honor `NO_COLOR` env var |
| 4c | All log output → **stderr** (JSON included) — **breaking** |
| 5a | Drop the inline `agent=`/`hook=` prefix; render as trailing key-value pairs |
| 5b | Drop `IsVisibleFn` (dead code) |
| 6a | Mixed: structured for machine context, `fmt.Sprintf` for human prose |
| 6b | Mechanical rewrite: `WithFields(...)` → `With(...)`; remove `Field` constructors |
| 6c | Key-value style (`l.Info("msg", "k", v)`); enforce with `sloglint` |
| 7 | Test handler: records `slog.Record`s + writes to `tb.Logf` (opt-out) |
| 8a | `core/` and `api/` accept `*slog.Logger` directly |
| 8b | Break external consumers; document in CHANGELOG |
| 9 | `AddSource` off by default, on with `--debug` |
| 10 | Trust slog handler serialization |
| 11 | Add `sloglint` to `.golangci.yml` |
| 12a | Ship in v4 |
| 12b | Hard cutover; no `--log-format=text-legacy` flag |

## 2. Target architecture

### 2.1 Replacement package layout

```diagram
core/                                      
  slog.go      ── small helpers (Fatal, level parsing)
  textlog.go   ── tint handler factory
  jsonlog.go   ── JSONHandler factory
  testlog.go   ── recording test handler (replaces Buffer + TestPrinter)
```

Final placement: keep the `logger` package name and directory for now
(minimizes import churn). Inside, replace the existing types with
slog-based factories. We can rename later.

### 2.2 Public surface

```go
package logger

// LevelVar is the process-wide log level controller. Flag parsing
// mutates this.
var LevelVar = new(slog.LevelVar)

// New constructs the agent's primary slog.Logger from CLI config.
// Replaces clicommand.CreateLogger.
func New(cfg Config) *slog.Logger

// Discard is a no-op slog.Logger.
var Discard = slog.New(discardHandler{})

// Fatal logs at Error level and exits the process.
func Fatal(l *slog.Logger, msg string, args ...any)

// FatalContext is the ctx-aware variant.
func FatalContext(ctx context.Context, l *slog.Logger, msg string, args ...any)

// NewTest returns a *slog.Logger that records every record AND
// writes to tb.Logf. Records can be inspected via the returned
// Recorder.
func NewTest(tb testing.TB, opts ...TestOption) (*slog.Logger, *Recorder)

type Recorder struct {
    Records []slog.Record
}

func (r *Recorder) HasMessage(substr string) bool
func (r *Recorder) MessagesAtLevel(l slog.Level) []string
```

`Config` carries: `Format` (text/json), `Level`, `Debug`, `NoColor`.

### 2.3 Handler details

**tint (text):**
```go
tint.NewHandler(os.Stderr, &tint.Options{
    Level:      LevelVar,
    NoColor:    !colorSupported(),
    TimeFormat: time.Kitchen, // or default
    AddSource:  cfg.Debug,
})
```
TTY detection logic moves from `logger/log.go` + `init_windows.go`
into a `colorSupported()` helper that also honors `NO_COLOR`.

**JSON:**
```go
slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level:     LevelVar,
    AddSource: cfg.Debug,
})
```

**Test (recording + tb.Logf):**
```go
type testHandler struct {
    tb       testing.TB
    rec      *Recorder
    quietTb  bool // opt-out
    fmt      slog.Handler // delegate for tb.Logf formatting
}
```

### 2.4 Removed types
- `logger.Logger` (interface)
- `logger.ConsoleLogger`
- `logger.TextPrinter`, `logger.JSONPrinter`, `logger.TestPrinter`
- `logger.Buffer`, `logger.NewBuffer`
- `logger.Field`, `logger.Fields`, `logger.GenericField`,
  `logger.StringField`, `logger.IntField`, `logger.DurationField`
- `logger.Level` and the `DEBUG/NOTICE/INFO/WARN/ERROR/FATAL`
  constants (replaced by `slog.Level`)

## 3. Migration phases

### Phase 0 — Pre-work (low risk, can land independently)
1. **Replace `secretLogger := logger.NewBuffer()` with `logger.Discard`** in
   [`internal/job/executor.go:932`](../internal/job/executor.go#L932).
2. **Delete `IsVisibleFn`** from [`logger/log.go`](../logger/log.go)
   (dead code).
3. **Add dependency:** `go get github.com/lmittmann/tint`.

### Phase 1 — New package surface
1. Build the new `logger` API alongside the old one in a new file
   (`logger/slog.go`). Don't remove the old types yet.
2. Add the test handler with `Recorder` and `tb.Logf` integration.
3. Wire `--log-level` and `--debug` to `LevelVar`.
4. New `logger.New(Config)` replaces `clicommand.CreateLogger`.

### Phase 2 — Mechanical migration
Big diff, but straightforward. Order matters: leaf packages first.

1. **`api/`** — change `NewClient(*slog.Logger, ...)`.
2. **`core/`** — same.
3. **`internal/*`** (artifact, process, agentapi, secrets, job) —
   migrate call sites.
4. **`agent/`** — agent worker, job runner, log streamer, etc.
5. **`clicommand/`** — CLI entry points; replace `CreateLogger`.
6. **`metrics/`, `kubernetes/`, `lock/`, `status/`, `jobapi/`, etc.**

For each call site:
- `logger.Logger` field/param → `*slog.Logger`.
- `l.Debug(fmt, args...)` → `l.Debug(fmt.Sprintf(fmt, args...))` for
  human prose, or `l.Debug("msg", "k", v)` for structured context
  (per 6a).
- `l.WithFields(StringField("k", v), IntField("n", n))` →
  `l.With("k", v, "n", n)`.
- `l.Notice(...)` → `l.Info(...)` (4 sites total).
- `l.Fatal(...)` → `logger.Fatal(l, ...)`.
- `*Context` variants where ctx is in scope.

Tooling:
- `gofmt -r` patterns can handle many `WithFields` rewrites.
- A short Go AST script can convert `Notice` → `Info` and `Fatal` →
  `logger.Fatal` calls.
- Manual review for printf vs structured judgment calls.

### Phase 3 — Tests
1. Replace `logger.NewBuffer()` (~46 sites) with `logger.NewTest(t)`,
   adapting `.Messages` assertions to `.Records` or
   `Recorder.HasMessage(...)`.
2. Replace `logger.NewConsoleLogger(logger.NewTestPrinter(t), ...)` (~20
   sites) with `logger.NewTest(t)`.
3. Run `go test ./... -race`.

### Phase 4 — Remove dead code
1. Delete the old `logger.Logger`, printers, fields, buffer, levels.
2. Delete `init_windows.go` if tint handles Windows ANSI internally
   (tint does — verify).
3. `go mod tidy`; remove `golang.org/x/term` if no other consumer.

### Phase 5 — Tooling & docs
1. Add `sloglint` to `.golangci.yml`:
   ```yaml
   linters:
     enable:
       - sloglint
   linters-settings:
     sloglint:
       no-mixed-args: true
       kv-only: true
       no-global: "all"   # forbid slog.Info() — use plumbed logger
       context: scope     # require ctx variant when ctx in scope
       static-msg: true   # message string must be a constant
   ```
2. Update [`AGENT.md`](../AGENT.md) with logging conventions:
   - Use plumbed `*slog.Logger`, not `slog.Default()`.
   - Key-value style.
   - Use `*Context` variants.
   - Standard attribute key names (`job_id`, `agent_name`,
     `pipeline_slug`, `path`, `duration`, etc.).
3. Update [`CHANGELOG.md`](../CHANGELOG.md) with a prominent
   "Logging changes (breaking)" section listing every user-visible
   change (see §4).

## 4. CHANGELOG entry (draft)

> **Logging changes (breaking, v4)**
>
> The agent's logging output has changed in several user-visible ways:
>
> - **JSON schema:** the timestamp key is now `time` (was `ts`).
> - **JSON values:** numbers, booleans and durations are now emitted
>   as JSON-typed values instead of strings.
> - **Level names:** the `NOTICE` and `FATAL` levels have been removed.
>   `--log-level notice` is no longer accepted; the default level is
>   now `info`.
> - **Output stream:** JSON logs are now written to **stderr** (was
>   stdout). If you redirect agent output to capture logs, update your
>   redirection accordingly.
> - **Color:** the agent now respects the `NO_COLOR` environment
>   variable.
> - **`agent=`/`hook=` prefixes:** these now appear as trailing
>   `key=value` pairs rather than inline before the message.
> - The new `--debug` mode includes source file:line for each log
>   line.

## 5. Verification checklist

Per `AGENT.md`:
- `go build -o buildkite-agent .`
- `go test ./...`
- `go test -race ./...`
- `go tool gofumpt -extra -w .`
- `golangci-lint run`
- Manual smoke tests:
  - `./buildkite-agent start --log-format text` — colored output on TTY.
  - `./buildkite-agent start --log-format text` piped to file — no ANSI.
  - `./buildkite-agent start --log-format json` — typed JSON on stderr.
  - `NO_COLOR=1 ./buildkite-agent start` — no color even on TTY.
  - `./buildkite-agent start --log-level debug` — debug lines + source.
  - `./buildkite-agent start --debug` — same.
  - End-to-end: run a real job, confirm `agent=`/`hook=` attributes
    appear (as trailing key-value pairs) on per-job log lines.

## 6. Risk register

| Risk | Mitigation |
|---|---|
| Customer log pipelines parsing `ts` key break silently | Loud CHANGELOG; mention in release notes |
| Customer scripts redirecting stdout for JSON logs break silently | Loud CHANGELOG |
| Big diff regressions | Phase the migration; run `-race` tests after each phase |
| `sloglint` rules too strict, slows reviews | Land lint config in a separate PR after migration; tune rules first |
| tint Windows ANSI behavior differs from current `init_windows.go` | Verify by running on Windows CI before final cutover |

## 7. Estimated effort

- Phase 0: ~1 hour
- Phase 1: ~half a day
- Phase 2: 2-3 days (this is the bulk; ~761 call sites)
- Phase 3: 1 day
- Phase 4: ~1 hour
- Phase 5: ~half a day

**Total: ~4-5 working days** for one engineer focused on it. Could
parallelize Phase 2 by splitting packages between agents.
