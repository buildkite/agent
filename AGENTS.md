# Buildkite Agent Development Guide

## Documentation contributions

When creating a pull request for the Buildkite documentation site as part of a task
that involves modifying or analyzing this repository, open it in
`buildkite/docs-private` by default. This destination is required for Buildkite
staff. For known external contributors, use `buildkite/docs` instead.

Verify the push destination and pull request base repository before publishing.
If the required destination is unavailable, ask for guidance rather than switching
repositories.

Documentation maintained in this repository, such as `README.md`, `CONTRIBUTING.md`,
and `AGENTS.md`, stays in `buildkite/agent`.

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

## Reviewing release PRs

A release PR has a title like `release: v4.2.1` and the `release` label, and usually only changes `version/VERSION`. Its body is published verbatim as the GitHub release notes, so review the body as carefully as the diff. Check it against the rules in the release skill, [`.agents/skills/buildkite-agent-release/SKILL.md`](.agents/skills/buildkite-agent-release/SKILL.md). Skip the skill's interactive steps; they're for the person cutting the release.

- **Version bump:** find the previous release on the same line (PRs against `main` are v4, PRs against `v3` are v3) and classify the changes using the SemVer rules in step 4 of the skill. One ✨ Added entry, or a 🔧 Changed entry that changes existing behaviour, makes the release at least a minor. Read the PRs behind 🏠 Internal entries too, since user-facing changes sometimes end up there. If the bump is too small, request changes and list the PRs that need the bigger bump. For v3, flag anything that would make the release a minor instead of accepting a minor bump.
- **Release notes:** check that entries are in the right category and are written for people who use the Buildkite Agent, following step 7 of the skill. Entries shouldn't have ticket IDs or conventional-commit prefixes, should name the command, flag or setting involved, and should describe the effect rather than the implementation. Dependabot PRs should be grouped into a single bullet. Check that the "Full Changelog" link compares the previous release with the new tag. Suggest rewrites for entries that would read poorly to someone outside Buildkite. Dropping minor entries is fine, but flag any missing user-facing change.

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
