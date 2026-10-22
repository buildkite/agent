# Experiments

We frequently introduce new experimental features to the agent. You can use the `--experiment` flag to opt-in to them and test them out:

```bash
buildkite-agent start --experiment experiment1 --experiment experiment2
```

Or you can set them in your [agent configuration file](https://buildkite.com/docs/agent/self-hosted/configure):

```
experiment="experiment1,experiment2"
```

If an experiment doesn't exist, no error will be raised.

**Please note that there is every chance we will remove or change these experiments, so using them should be at your own risk and without the expectation that they will work in future!**

## Available Experiments

### `origin-clonekit`

Seeds a missing on-host Git mirror using Cursor Origin's CloneKit pack data.
Requires `BUILDKITE_GIT_MIRRORS_PATH` and a repository URL of the form
`https://origin.cursor.com/git/owner/repo.git` or
`https://origin.cursor.com/owner/repo.git`. The URL is preserved for credential
lookup and manifest requests; use the backend-provided form for managed credentials.
The backend-provided remote mirror URL takes precedence over the canonical
repository URL. Manifest credentials come only from the agent's repository
credential helper; SSH authentication is not supported.

The experiment does not configure credentials for the subsequent canonical Git
checkout. Self-hosted Origin jobs must retain working operator-provided Git auth
or explicitly opt into `BUILDKITE_USE_REPOSITORY_PROVIDER_GIT_CREDENTIALS=true`,
with repository access enabled in the backend.

Only the first checkout attempt of a fresh checkout is eligible. Existing mirrors,
submodule mirrors, skipped mirror updates, hook-rewritten repositories and custom
mirror clone flags retain their normal behavior (the default `-v` and empty mirror
flags are eligible). Downloads and verification have a 30-second budget;
failures fall back directly to canonical Git in the same
attempt. Cancellation does not start fallback. Canonical Git remains authoritative
for the build commit. Disable the experiment to restore normal mirror creation;
already-created mirrors remain usable.

**Status:** Experimental, HTTPS Origin only. Uses primary artifact URLs, not the
optional S3 delivery URLs. No top-up fetch or verification bypass is provided.

### `agent-api`

This exposes a local API for interacting with the agent process.
...with primitives that can be used to solve local concurrency problems (such as multiple agents handling some shared local resource).

The API is exposed via a Unix Domain Socket. The path to the socket is not available via a environment variable - rather, there is a single (configurable) path on the system.

**Status:** Experimental while we iron out the API and test it out in the wild. We'll probably promote this to non-experiment soon™.

### `pty-raw`

Set PTY to raw mode, to avoid mapping LF (\n) to CR,LF (\r\n) in job command output.
These extra newline characters are normally not noticed, but can make raw logs appear double-spaced
in some circumstances.

We run commands in a PTY mostly (entirely?) so that the program detects a PTY and behaves like it's
running in a terminal, using ANSI escapes to provide colours, progress meters etc. But we don't need
the PTY to modify the stream. (Or do we? That's why this is an experiment)

**Status:** Experimental for some opt-in testing before being promoted to always-on.

### `program-status`

Shows [OSC 7501 program status](https://www.superlogical.com/rex/docs/build/program-status) reports in the job log. Programs send these escape sequences to say whether they're working, blocked, done or failed. The Buildkite log viewer hides escape sequences it doesn't recognise, so with this experiment the agent writes a line after each report, such as:

```
# Program status: state=working id=build progress=40 app=cargo msg="Compiling"
```

The report itself stays in the log unchanged. Reports that repeat the previous report for the same `id`, or only change its `progress`, don't add a line. Messages and titles are decoded before secret redaction, so redacted values are removed from them.

The experiment also sets `BUILDKITE_PROGRAM_STATUS=true` in the job environment, unless the job already sets it. Tools such as the Buildkite CLI (`bk`) only send reports in a Buildkite job when it's `true`. Set it to `false` in the job to turn them off.

`buildkite-agent pipeline upload` and `buildkite-agent artifact upload` send their own reports, with `app=buildkite-agent`, following the same `BUILDKITE_PROGRAM_STATUS` rules as `bk`. `artifact upload` reports its progress as the percentage of files uploaded.

The agent also reports the job's own progress, on records with these ids, so they don't overwrite reports from programs the job runs:

- `checkout`, `command` and `artifacts` for those parts of the job.
- `hook/agent/<hook>` and `hook/repository/<hook>` for agent and repository hooks, such as `hook/agent/pre-command`.
- `hook/plugin/<plugin>/<hook>` for plugin hooks, such as `hook/plugin/docker/pre-command`.

These reports don't add a line to the log, because the log already has a header for each of them. When the job ends, the agent clears the records that finished successfully, except `command`.

**Status:** Experimental. The agent doesn't reply to the protocol's feature detection query yet.

### `job-errors-osc`

**Status:** Proof of concept for review, not a production-ready error protocol.

Extracts standard OSC 7501 `state=error` reports from live job output using
[`github.com/wolfeidau/osc7501`](https://github.com/wolfeidau/osc7501). The
bootstrap decodes each report, validates and redacts its message with the job's
current secret registry, then sends it to the existing job errors API.
The original OSC bytes remain in the log. Enable on the agent:

```bash
buildkite-agent start --experiment job-errors-osc
```

Existing `job capture-error` calls **still send directly**, independently of
OSC extraction. Existing automatic direct captures still require
`BUILDKITE_CAPTURE_AGENT_ERRORS=true` (or the Git-specific opt-in). Nothing
replaces the existing capture API in this POC. Reporting the same failure both
ways intentionally creates two error records for comparison.

#### A customer or plugin can print standard OSC

A shell producer needs neither the capture CLI nor a private JSON envelope:

```bash
message='Failed to pull image: manifest unknown'
encoded=$(printf '%s' "$message" | base64 | tr -d '\n')
printf '\033]7501;state=error:msg=%s\033\\' "$encoded"
```

Print this to the actual job/hook output, not a redirected stream. Docker and
Docker Compose currently suppress the capture CLI's stdout and stderr, so merely
printing OSC inside that CLI would lose their reports. Their existing capture
calls continue to work unchanged; no plugin repository is changed by this POC.

OSC extraction does not depend on `program-status` or
`BUILDKITE_PROGRAM_STATUS`. Those control the agent's own OSC production and
the existing human-readable status decoration, not acceptance of customer OSC.

#### Explicit OSC beside existing automatic emitters

Enable both experiments to see the agent produce OSC beside its existing
captures. Its native reports respect `BUILDKITE_PROGRAM_STATUS=false`:

```bash
buildkite-agent start --experiment program-status --experiment job-errors-osc
```

For example, a command failure has two explicit calls using the same message:

```go
message = e.withRecentOutput(message)
errorStatus(ctx, e.shell).Failed("error/command", message)
captureJobError(ctx, e.shell, code, message)
```

Git reports likewise include the checkout description (repository, ref/commit)
and retained Git output, not just the classifier summary. Hook reports include
the hook path and retained output. Other instrumented families still use shorter
agent-written summaries, so their native and direct messages are not identical.
The scanner deliberately excludes the agent's existing phase-status reports to
avoid treating a phase failure as another duplicate of its explicit error report.
Parent-owned failures are scanned separately; child output is scanned only in
bootstrap, where runtime secret registrations are available.

#### Redaction and information differences

- Both the Local Job API and OSC extractor use the same validation/redaction
  function: registered secrets, runtime additions, Buildkite token prefixes,
  URL credentials and query strings are masked before the API's 1000-character
  truncation. Invalid, blank, malformed or incomplete OSC does not create errors.
- **Only the extracted API copy is sanitized.** Original encoded OSC remains
  in stored logs, even when it contains a secret. Base64 is not encryption.
  Redaction is best effort, not a guarantee against arbitrary sensitive text or
  secrets altered/truncated by a producer before extraction.
- The library's encoder replaces controls, including newlines/tabs, with spaces
  and caps `msg` at 2048 decoded bytes. Its parser rejects control-containing or
  oversized messages rather than accepting arbitrary base64 data. Consequently,
  multiline formatting and long Unicode diagnostics can differ from direct API
  captures. There is no private chunking, nested encoding or JSON convention.
- Classifier transport is out of scope. The backend requires a code, so OSC
  captures use `buildkite.osc_error`. Timestamp and idempotency key are generated
  at extraction, not copied from the direct capture. `app`/`id` are not trusted
  identities or classifier fields.
- OSC is a status protocol whose records replace previous visible status.
  This experiment instead captures every complete `state=error` observation as
  an independent API error; it does not deduplicate repeated status messages.

#### Differences and open questions

- Existing capture-error success semantics are unchanged. Printing OSC confirms
  a local write, not backend acceptance. Delivery failures do not fail the job.
- Delivery is synchronous and can delay output by up to two seconds per report.
  There is no durable queue, retry or stored-log replay. Requests stop after
  404/422; 429/5xx do not disable later reports. These quotas can affect the two
  comparison paths independently.
- Any process writing job output can create an error. Validation/redaction is
  not authentication. Interleaved or stripped escape sequences can lose reports.
- Mixed-version and Kubernetes/container forwarding need compatibility testing.
  Existing plugin calls retain their capability/socket requirements; raw OSC
  producers do not use those calls.

### `interpolation-prefers-runtime-env`

When interpolating the pipeline level environment block, a pipeline level environment variable could take precedence over environment variables depending on the ordering. This may contravene Buildkite's [documentation](https://buildkite.com/docs/pipelines/environment-variables#environment-variable-precedence) that suggests the Job runtime environment takes precedence over that defined by combining environment variables defined in a pipeline.

We previously made this the default behaviour of the agent (as of v3.63.0) but have since reverted it.

**Status:** Available as an experiment to allow users who have since depended on this behaviour to re-enable it. If you use this feature please let us know so we may better understand your use case.

### `zip-plugins`

Allows plugins to be downloaded as zip archives instead of being cloned from a Git repository. This is useful for plugins hosted as zip files on HTTP(S) URLs.

**Status:** Experimental while we test zip archive support for plugins.

### `legacy-post-hook-order`

This experiment is an escape hatch that reverts to the v3 execution order of `post-checkout` and `post-command` hooks.

In Agent v3, hooks of any kind would run in the same order as one another (for plugins, the order in which plugins are specified for a step). In v4, multiple `post-checkout`, `post-command`, or `pre-exit` hooks execute in _reverse_ order. This change makes it easier for multiple plugins and hooks to compose.

For example, suppose a step specifies two plugins A and B, and there are also agent and repository hooks. Under version 3, each hook type would execute in the same order:

- agent pre-checkout
- (pre-checkout is not possible for repository hooks)
- plugin A pre-checkout
- plugin B pre-checkout
- (checkout)
- agent post-checkout
- repository post-checkout
- plugin A post-checkout
- plugin B post-checkout
- agent pre-command
- repository pre-command
- plugin A pre-command
- plugin B pre-command
- (command)
- agent post-command
- repository post-command
- plugin A post-command
- plugin B post-command
- agent pre-exit
- repository pre-exit
- plugin A pre-exit
- plugin B pre-exit

Under version 4, the execution order is (key differences in bold):

- agent pre-checkout
- (pre-checkout is not possible for repository hooks)
- plugin A pre-checkout
- plugin B pre-checkout
- (checkout)
- plugin **B** post-checkout
- plugin **A** post-checkout
- **repository** post-checkout
- **agent** post-checkout
- agent pre-command
- repository pre-command
- plugin A pre-command
- plugin B pre-command
- (command)
- plugin **B** post-command
- plugin **A** post-command
- **repository** post-command
- **agent** post-command
- plugin **B** pre-exit
- plugin **A** pre-exit
- **repository** pre-exit
- **agent** pre-exit

**Status:** This escape-hatch experiment will be removed in a future release, once we are confident that it isn't needed.
