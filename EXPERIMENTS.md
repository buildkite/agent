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

Transports captured job errors through live job output using
[`github.com/wolfeidau/osc7501`](https://github.com/wolfeidau/osc7501), then
extracts them in the parent agent and posts them to the existing job errors API.
Enable it on the **parent agent**, not just an independently invoked bootstrap:

```bash
buildkite-agent start --experiment job-errors-osc
```

Automatic agent errors still require `BUILDKITE_CAPTURE_AGENT_ERRORS=true`.
Explicit reports still use the existing command, including from plugins:

```bash
buildkite-agent job capture-error docker.pull_failed --message 'Could not pull the build image'
```

The bootstrap's Local Job API still validates, redacts registered secrets
(including runtime registrations), masks URL credentials/query strings, truncates
messages to 1000 characters, and assigns the timestamp and idempotency key.
Instead of posting to Buildkite, it writes that normalized payload to stdout.
The parent extracts it before timestamping and log-size limits, and sends it with
the job's token. Errors observed outside bootstrap also use the OSC path. With
the experiment off, the current direct API paths remain unchanged. The
`program-status` experiment is independent and can be enabled alongside this one;
`BUILDKITE_PROGRAM_STATUS=false` does not disable job error transport.

The Docker and Docker Compose plugins suppress the capture CLI's stdout and
stderr. Emitting from the bootstrap, rather than from that CLI process, keeps
their existing calls working without plugin changes.

#### Native OSC alongside the existing emitters

The POC also includes explicit native OSC calls beside the automatic error
emitters: Git/checkout, hooks/commands, plugin setup, secrets, CLI operations,
job rejection and Kubernetes failures. These use the `program-status` experiment
and respect `BUILDKITE_PROGRAM_STATUS=false`. Enable both experiments to compare
native status with the full-payload transport:

```bash
buildkite-agent start --experiment program-status --experiment job-errors-osc
```

For example, a command failure has two explicit calls:

```go
errorStatus(ctx, e.shell).Failed("error/command", "The step's command failed.")
captureJobError(ctx, e.shell, code, e.withRecentOutput(message))
```

A failed Git command emits the fixed summary returned by `classifyGitError`
before adding checkout context and Git output. For a missing tree, the native
report decodes to:

```text
state=error id=error/git app=buildkite-agent
msg="Git checkout could not resolve the reference to a tree."
```

These native reports deliberately carry **only agent-written summaries**. Raw
diagnostics cannot safely be base64-encoded before secret redaction. They do not
include the classified code, checkout context/output, timestamp or delivery key;
they are not equivalent replacements for the API error. Repeated reports for
the same status id replace the visible status, rather than create error records.
The `error/` ids keep these observations separate from existing phase status.

The parent does **not** submit native status reports to the errors API. Only the
separate private envelope below triggers submission, so enabling both reports
does not double-send errors. With `job-errors-osc` disabled, existing emitters
still send directly to the API while native status can be enabled independently.

#### Information preservation

OSC 7501 defines **program status**, not structured job errors. A plain
`state=error` report cannot preserve our complete API contract and is deliberately
not treated as a captured error. This POC uses a **private convention**, not a
standard OSC error schema:

- `app=buildkite-job-error`, `state=error`, `id=job-error/v1/<delivery-key>`.
- `title` is the 1-based part number and total, such as `1/3`.
- `msg` holds up to 2000 ASCII bytes of base64-encoded JSON, then the library
  applies OSC's normal base64 text encoding. Concatenate the decoded messages
  in part order, base64-decode, and JSON-decode to recover the error.
- JSON preserves `code`, `message`, `timestamp` (including subsecond precision)
  and `idempotency_key`. Splitting avoids the library's silent 2048-byte message
  truncation, including for 1000-character Unicode diagnostics. Encoding before
  splitting also preserves newlines, tabs and other controls.
- Redaction happens **before** encoding. Error reports emitted by the bootstrap
  bypass the program-status describer and the log redactor, because processing
  encoded data again cannot safely redact it and could corrupt the envelope.

#### Differences and open questions

- `capture-error` success now acknowledges local validation and the OSC write,
  **not upstream API acceptance**. Later delivery failure appears in agent logs,
  not as a command failure or Local Job API response. Truncation warnings still
  reach the caller. The parent stops delivery after 404/422; 429/5xx do not
  disable later reports. Requests are single-attempt with a two-second timeout.
- Delivery is synchronous in the parent log writer, so a slow API can delay
  draining output by up to two seconds per report. There is no durable queue,
  retry or stored-log replay. Incomplete, malformed or interrupted envelopes are
  not emitted. Assembly is bounded to 16 pending errors of at most 16 parts each.
- This is not an authenticated channel: job output can forge this private
  envelope, and the parent does not have bootstrap's live secret registry.
  Direct hand-written OSC envelopes bypass Local Job API redaction. **Keep
  plugins on `job capture-error`; do not use raw OSC to report sensitive data.**
  Establishing a safe boundary for arbitrary producers is required before rollout.
- Raw OSC payloads now persist in stored logs, including redacted diagnostics
  that previously existed only in the errors API. Base64 is not encryption.
  Consumers that strip escape sequences lose the transport, and independently
  merged stdout/stderr streams can corrupt frames if interleaved mid-sequence.
- Standalone bootstrap has no parent consumer. Mixed-version parent/bootstrap
  deployments and Kubernetes/container output forwarding need compatibility and
  interruption testing before rollout. The existing Local Job API OS and socket
  requirements have not been removed.

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
