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
- Import organization: stdlib, then everything else (gofumpt groups all non-stdlib imports together)
- Error handling: explicit errors, wrapped with context
- Naming: PascalCase for exported, camelCase for private, ALL_CAPS for constants
- Interface types end with -er suffix where appropriate
- Use `github.com/urfave/cli` for CLI commands

## Development environment notes

This is a single Go CLI application (the Buildkite Agent). There is no long-running
server to keep up for development; you build a binary and/or run subcommands directly.
See also [`README.md`](README.md) (Development section), [`mise.toml`](mise.toml), and
[`CONTRIBUTING.md`](CONTRIBUTING.md).

Prerequisites (a plain checkout does not install these for you):
- Go toolchain matching `mise.toml` (`mise install` sets up the pinned versions).
- `gofumpt` and `gotestsum` need no separate install: they are Go tools (declared in
  `go.mod`'s `tool` block) and run via `go tool ...`.
- `golangci-lint` is a standalone binary (not a `go tool`), pinned in `mise.toml`. Get it
  via `mise install`, or install the pinned version manually and put it on `PATH`, e.g.:
  `curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.9.0`
  (then ensure `$(go env GOPATH)/bin` is on your `PATH`).
- The polyglot hook integration tests (e.g. `TestPolyglotScriptHooksCanBeRun`) require
  `ruby` on `PATH`; install it via your OS package manager if it is missing.

Non-obvious environment/run caveats:
- `internal/job` test `TestResolvingGitHostAliasesWithFlagSupport` only runs when
  `/.dockerenv` exists (i.e. inside a container) and expects the SSH aliases from
  `.buildkite/build/ssh.conf` to be present at `/etc/ssh/ssh_config.d/` (mirroring
  `.buildkite/Dockerfile-compile`). If this test fails with unresolved aliases, copy it:
  `sudo cp .buildkite/build/ssh.conf /etc/ssh/ssh_config.d/`
- Cold-cache flakiness: `internal/job/integration` tests spawn many `bintest` mock
  binaries that are compiled on first use. On a completely cold Go build cache, the
  timing-sensitive `TestPreExitHooksFireAfterCancel` (500ms sleep before cancel) can
  panic/fail. Warming the build cache first (e.g. `go build ./...` or simply running the
  package once) makes the full suite pass reliably. This is a pre-existing test timing
  assumption, not a product bug.

Running the app end-to-end without a Buildkite token — build the binary, then use
`bootstrap` to run a job locally (invoke the freshly built `./buildkite-agent`, since a
plain checkout does not put `.` on `PATH`):
```
go build -o buildkite-agent .
./buildkite-agent bootstrap \
  --build-path=/tmp/bk-builds --job demo --phases command \
  --repository . --commit HEAD --branch main --pipeline-provider custom \
  --agent a --organization o --pipeline p \
  --command 'echo hello'
```
(`./buildkite-agent start` requires a real agent token and network access to buildkite.com.)
