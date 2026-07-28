# Agent Guidelines

For general build/test/lint commands and architecture, see [`AGENT.md`](AGENT.md),
[`README.md`](README.md) (Development section), [`mise.toml`](mise.toml), and
[`CONTRIBUTING.md`](CONTRIBUTING.md).

## Cursor Cloud specific instructions

This is a single Go CLI application (the Buildkite Agent). There is no long-running
server to keep up for development; you build a binary and/or run subcommands directly.

Standard commands (already documented in `AGENT.md` / `mise.toml`):
- Build: `go build -o buildkite-agent .`
- Test: `go tool gotestsum --format pkgname -- ./...` (or `go test ./...`)
- Lint: `go tool gofumpt -extra -w .` then `golangci-lint run`
- Run a job locally (core functionality): `buildkite-agent bootstrap ...` (see below)

Non-obvious environment/run caveats:
- `gofumpt` and `gotestsum` are Go tools (declared in `go.mod`'s `tool` block) and run
  via `go tool ...`; they do not need separate installation.
- `golangci-lint` is a standalone binary (not a `go tool`). It is installed into
  `$(go env GOPATH)/bin`, which is added to `PATH` via `~/.bashrc`. If `golangci-lint`
  is not found, reinstall it with the version pinned in `mise.toml` (currently 2.9.0):
  `curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.9.0`
- Some integration tests need `ruby` on `PATH` (polyglot hook tests, e.g.
  `TestPolyglotScriptHooksCanBeRun`). `ruby` is installed system-wide.
- `internal/job` test `TestResolvingGitHostAliasesWithFlagSupport` only runs when
  `/.dockerenv` exists (which it does in the Cloud VM) and expects the SSH aliases from
  `.buildkite/build/ssh.conf` to be present at `/etc/ssh/ssh_config.d/`. That file is
  copied there during setup (mirroring `.buildkite/Dockerfile-compile`). If this test
  fails with unresolved aliases, re-copy it:
  `sudo cp .buildkite/build/ssh.conf /etc/ssh/ssh_config.d/`
- Cold-cache flakiness: `internal/job/integration` tests spawn many `bintest` mock
  binaries that are compiled on first use. On a completely cold Go build cache, the
  timing-sensitive `TestPreExitHooksFireAfterCancel` (500ms sleep before cancel) can
  panic/fail. Warming the build cache first (e.g. `go build ./...` or simply running the
  package once) makes the full suite pass reliably. This is a pre-existing test timing
  assumption, not a product bug.

Running the app end-to-end without a Buildkite token — `bootstrap` runs a job locally:
```
buildkite-agent bootstrap \
  --build-path=/tmp/bk-builds --job demo --phases command \
  --repository . --commit HEAD --branch main --pipeline-provider custom \
  --agent a --organization o --pipeline p \
  --command 'echo hello'
```
(`buildkite-agent start` requires a real agent token and network access to buildkite.com.)
