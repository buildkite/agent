# Buildkite Agent Development Guide

## Build/Test/Lint Commands

- **Build:** `go build -o buildkite-agent .` or `go run *.go <command>`
- **Test:** `go test ./...` (run all tests)
- **Test (single package):** `go test ./path/to/package`
- **Test (race detection):** `go test -race ./...`
- **Lint/Format:** `go tool gofumpt -extra -w .` and `golangci-lint run`
- **Generate:** `go generate ./...`
- **Deps:** `go mod tidy`

## Architecture

Go CLI application with main packages:
- **[`agent/`](agent/)**: Core agent worker, job runner, log streaming, pipeline upload
- **[`api/`](api/)**: HTTP client for Buildkite API communication
- **[`core/`](core/)**: Programmatic job control interface
- **[`jobapi/`](jobapi/)**: Local HTTP server for job introspection during execution
- **[`clicommand/`](clicommand/)**: CLI command implementations
- **[`internal/`](internal/)**: Internal utilities (shell, sockets, artifacts, etc.)
- **[`process/`](process/)**: Process execution, signal handling, output streaming
- **[`logger/`](logger/)**: Structured logging
- **[`env/`](env/)**: Environment variable management

## Code Style

- Formatting with `gofumpt` in extra mode: `go tool gofumpt -extra -w .`
- Struct-based configuration patterns (e.g., `AgentWorkerConfig`, `JobRunnerConfig`)
- Context-aware functions: `func Name(ctx context.Context, ...)`
- Import organization: stdlib, external deps, internal packages
- Error handling: explicit errors, wrapped with context
- Naming: PascalCase for exported, camelCase for private, ALL_CAPS for constants
- Interface types end with -er suffix where appropriate
- Use `github.com/urfave/cli` for CLI commands

## Logging

The agent uses `log/slog` (with [`tint`](https://github.com/lmittmann/tint) for
human-readable text output and `slog.JSONHandler` for JSON). Conventions:

- Plumb a `*slog.Logger` explicitly through long-lived components.
  Do NOT call `slog.Default()` / `slog.Info()` etc. — `sloglint`'s
  `no-global` rule enforces this.
- Use the `*Context` method variants (`l.InfoContext(ctx, …)`,
  `l.DebugContext(ctx, …)`, …) wherever a `ctx` is in scope, so that
  trace/correlation IDs flow through. `sloglint`'s `context: scope`
  rule enforces this.
- Choose ONE style per call site: either positional key/value
  (`l.Info("acquired", "job_id", id)`) or `slog.Attr` values
  (`l.Info("acquired", slog.String("job_id", id))`). Don't mix both
  in a single call; `sloglint`'s `no-mixed-args` rule enforces this.
- Attribute key names should be `snake_case` and stable. Standard
  attributes used across the codebase: `agent_name`, `job_id`,
  `pipeline_slug`, `org`, `branch`, `path`, `duration`, `status`,
  `proto`.
- For one-off log messages where structured attributes would be
  awkward, wrap the formatted string in `fmt.Sprintf` and pass it as
  the message: `l.Info(fmt.Sprintf("…%s…", x))`. `sloglint`'s
  `static-msg` rule is intentionally NOT enabled.
- For fatal errors, use `logger.Fatal(l, …)` (or `logger.FatalContext`)
  rather than calling `os.Exit` directly after a log line. There is no
  `slog.LevelFatal`.
- The text/JSON formatter and active level are chosen by
  `logger.New(logger.Config{…})`; tests use `logger.Test(t)` (returns
  a logger plus a `*Recorder` for assertions) or `logger.Discard`.
