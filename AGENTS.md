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

## Job error codes

Errors the agent reports on a job (see `captureJobError` in
[`internal/job/job_errors.go`](internal/job/job_errors.go)) use codes named
`buildkite.<subject>_<outcome>`, such as `buildkite.git_clone_failed`. The
`buildkite.` root is reserved for the agent, official Buildkite plugins and the
backend; customers use any other code. When adding a code:

- Put what failed first and the outcome last: `git_ref_invalid`, not `git_invalid_ref`.
- Prefer the outcome words `failed`, `timed_out`, `invalid`, `not_found`, `rejected` and `lost`. Use another word only when it's clearer, such as Git's own `not_a_tree`.
- Use one code for each kind of failure, and give each code one meaning. If one code would cover two different failures, add a code.
- Put details, such as file paths, refs and output, in the message, not the code.

Codes are matched by people, coding agents and automation, so don't rename one after it's released. The `job capture-error` help text in [`clicommand/job_capture_error.go`](clicommand/job_capture_error.go) states the same convention for customers.

## Labelling pull requests

Every PR needs a category label. The [PR Labels workflow](.github/workflows/pr-labels.yml) is a required check, and it fails until the PR has at least one of `bug`, `feature`, `change`, `internal`, `dependencies` or `release`. The labels also decide where the PR appears in the release notes ([`.github/release.yml`](.github/release.yml)), so the check passing doesn't mean the label is right. A user-facing PR labelled `internal` or `dependencies` ends up under 🏠 Internal, where people reading the release notes are likely to miss it.

Add the label when you create the PR, for example `gh pr create --label bug ...` or `gh pr edit <number> --add-label bug`. Choose one category based on how the change affects people who use the agent, not on how much code it touches:

- `feature`: adds something new that users can use, such as a command, flag, setting or environment variable. Release notes: ✨ Added.
- `bug`: fixes behaviour that was broken or didn't match what was documented. Release notes: 🐛 Fixed.
- `change`: changes existing behaviour that users can see but doesn't fix a bug or add a feature, such as defaults, log or error output, or performance. Release notes: 🔧 Changed.
- `internal`: changes nothing users can see, such as refactors, tests, CI, release tooling, agent skills, or docs kept in this repo like `AGENTS.md`. Release notes: 🏠 Internal.
- `dependencies`: only bumps dependencies (Dependabot normally applies this itself). If the bump fixes a bug or a vulnerability users would care about, add `bug` or `security` too.
- `release`: only for release PRs (`release: vX.Y.Z`), which leaves them out of the release notes.

Add these alongside the category when they apply:

- `security`: fixes a vulnerability or hardens security, for example `bug,security` or `change,security`. These PRs are listed under 🔒 Security.
- `breaking`: changes behaviour that users might depend on (use it with `change`). This requires a major version bump, so check with the user before adding it.

If a PR's scope changes after you open it, check its labels again.

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
