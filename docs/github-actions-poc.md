# Import GitHub Actions workflows

This branch embeds `github.com/buildkite/buildkite-gha` in the agent. There is
no plugin and no separate `buildkite-gha` installation.

## Run the proof of concept

Build the agent from `feat/github_actions_pipeline_upload`:

```sh
go build -o buildkite-agent .
```

Create a self-hosted queue named `gha-poc` in the pipeline's cluster. Run the
new binary on the machine that will execute the importer and generated jobs,
using your agent registration token through the environment:

```sh
export BUILDKITE_AGENT_TOKEN='your-registration-token'
./buildkite-agent start --queue gha-poc --shell '/bin/bash -e -c'
```

Do not configure pipeline signing or enforce signature verification for this
POC. Signing configuration makes the import fail rather than silently publish
unsigned jobs. The job's Agent API access token is provided by the running
agent; do not substitute the registration token in the importer.

Commit a workflow such as `.github/workflows/ci.yml` to the repository under
test. Start with a shell-only job:

```yaml
name: Agent import smoke test
on: push
jobs:
  smoke:
    runs-on: ubuntu-latest
    steps:
      - run: echo 'Hello from a native Buildkite job'
```

Set the Buildkite pipeline's initial steps to:

```yaml
steps:
  - label: ':github: Import workflow'
    key: import-github-actions
    agents:
      queue: gha-poc
    command: >-
      buildkite-agent pipeline upload --format github-actions
      --gha-disable-runner-user .github/workflows/ci.yml
```

Run a build at the commit containing the workflow. The importer must run in
the repository checkout, inside a Buildkite job. The compiler uploads plans,
event snapshots, continuation records and its own executable before publishing
the generated pipeline. Each generated job downloads and SHA-256-verifies the
executable before invoking its runtime protocol. A matrix or runner selection
derived from a previous job's outputs is expanded by a deferred upload job.

### Runner routing and platforms

The **job-scoped Agent API must resolve `runs-on` to your local queue and host
platform** for this local-agent POC. The compiler requests this automatically.
Inspect the dry-run before running: the generated `agents.queue` must be
`gha-poc`, and any additional agent tags must match your agent. For a native
local runner the resolved target should not require a hosted `image`.

If your server supports explicit queue validation, you can add the repeatable
`--gha-runner-queue ubuntu-latest=gha-poc` option. This is not an unchecked
queue override: the compiler requires server validation, and its preset image
policy still applies. If the runner-resolution service is unavailable, the
compiler warns and uses built-in presets, which may target hosted images or
queues rather than your machine. Do not mistake that successful compilation
for a working local-agent route. Configure the server's runner mapping or use
its supported runner labels; this branch does not rewrite compiled plans.

Only the importer's executable is supplied as the runtime distribution:

- Linux amd64 importer → Linux amd64 jobs (for example `ubuntu-latest`).
- Linux arm64 importer → Linux arm64 jobs when the server resolves the labels
  to that platform.
- macOS arm64 importer → macOS arm64 jobs (use `macos-latest` in the example).

A macOS importer cannot execute Ubuntu-labelled jobs using its own binary.
Cross-platform imports need matching executables supplied through
`gha.CompileRequest.RuntimeDistributions`; this POC does not expose those
inputs as agent flags. Missing distributions produce an error explaining the
importer's platform. Windows and Intel macOS importers are not supported by
the pinned compiler.

`--gha-disable-runner-user` avoids the default Linux bootstrap's provisioned
runner account and privilege requirements. Omit it only on a host prepared
for that bootstrap. It does not change the workflow's target platform.
Remote actions and tool setup add network/tooling requirements beyond the
shell-only smoke test; the compiler's supported-workflow policy is unchanged.

## Commands

```sh
buildkite-agent pipeline upload --format github-actions .github/workflows/ci.yml
buildkite-agent pipeline upload --format github-actions --dry-run .github/workflows/ci.yml
buildkite-agent pipeline upload --format github-actions --gha-event-path event.json .github/workflows/ci.yml
buildkite-agent gha run-job --plan plan.json --artifact-producer JOB_UUID
```

Multiple explicit workflow operands are supported; stdin and native default
pipeline-file discovery are not used for GitHub Actions imports. Existing
`--format json` and `--format yaml` retain their native dry-run meaning.
`--no-interpolation` is unnecessary for GitHub Actions: generated commands are
always uploaded without interpolation. `--replace` applies to the initial
upload, not to subsequent deferred uploads.

Dry-run writes the compiler's generated YAML to stdout and diagnostics to
stderr, without uploading artifacts or a pipeline. It is hosted preparation,
not an offline compiler: it still needs the job environment, can query APIs
and fetch action sources, and can publish diagnostic annotations. An explicit
event file uses the compiler's normalized event snapshot schema, not a raw
GitHub webhook payload. Without it, the compiler reads `buildkite:webhook` or
falls back to reduced-fidelity Buildkite environment data.

Both runtime forms accept `--plan-digest DIGEST --plan-producer JOB_UUID` as an
alternative to `--plan`. A local plan in a Buildkite job needs the verified
`BUILDKITE_GHA_PLAN_DIGEST` environment value and its artifact producer.
Exit code 78 remains authoritative for tolerated workflow failures.
The compiler's top-level `run-job`, `upload --stage-digest … --stage-producer …`
and private `__container-process` bootstrap commands are also supported.

## Embedding limitations and local development

- The pinned `gha.RunCLI` cannot accept native services. The agent therefore
  uses typed `RunJob` and `UploadStage` handlers for generated job commands,
  and calls `RunCLI` only for the private container-process helper.
- The typed API still reads job ID, endpoint and token from the environment.
  Conflicting CLI/config-file overrides are rejected, not temporarily applied
  to global process environment.
- Generated pipeline signing is not implemented. The agent does not parse
  compiled steps through go-pipeline or interpolate them.
- Backend operations use native agent API/artifact packages and credentials
  use native secrets and Job API redaction. There are no recursive agent CLI
  adapters. The compiler's emitted shell bootstrap still invokes
  `buildkite-agent artifact download`, and Git invokes the trusted credential
  helper subprocess as required by Git's protocol.
- The compiler's isolated Git credential environment omits
  `BUILDKITE_REQUEST_HEADER_*`. Provider credential requests therefore lose
  server routing headers in that subprocess. Provider credentials may fail
  against endpoints requiring those headers; the shell-only smoke test does
  not use them. The compiler needs to preserve the permitted request headers
  in its isolated helper environment.
- The metadata size limit applies to the decoded value returned to the
  compiler. The native API client decodes the response before checking that
  limit; it is not a streaming HTTP response-memory bound.

To develop against a sibling compiler clone, add this uncommitted go.mod line:

```go
replace github.com/buildkite/buildkite-gha => ../buildkite-gha
```

Then run `go mod tidy` and rebuild. Remove the replacement before publishing
the agent branch. Compiler/runtime version identity and executable digest
verification require rebuilding and re-importing after code changes.

The fake-server integration test builds the agent and executes its emitted
bootstrap, deferred matrix and tolerated failure:

```sh
go test ./clicommand -run 'Test(GitHubActions|GHA)' -count=1 -v
```

That test does not prove real server runner routing, Buildkite job scheduling,
live secret/Job API integration, provider credentials, or hosted image/tool
availability. Those require the local-agent build against a real pipeline.
