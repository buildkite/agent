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
