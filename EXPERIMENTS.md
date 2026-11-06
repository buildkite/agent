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

Complete valid reports are decoded, their messages and titles redacted with the current registered secrets and Buildkite token patterns, and then re-encoded into the log. This protects both the encoded fields and the readable description. Unknown fields and complete invalid reports are omitted; incomplete, canceled or oversized sequences still pass through as ordinary log bytes and are not forwarded as reports. Reports that repeat the previous report for the same `id`, or only change its `progress`, don't add a line. Unknown secrets remain unknown: this is not a general detector for credentials or arbitrary encoded data.

The experiment also sets `BUILDKITE_PROGRAM_STATUS=true` in the job environment, unless the job already sets it. Tools such as the Buildkite CLI (`bk`) only send reports in a Buildkite job when it's `true`. Set it to `false` in the job to turn them off.

`buildkite-agent pipeline upload` and `buildkite-agent artifact upload` send their own reports, with `app=buildkite-agent`, following the same `BUILDKITE_PROGRAM_STATUS` rules as `bk`. `artifact upload` reports its progress as the percentage of files uploaded.

The agent also reports the job's own progress, on records with these ids, so they don't overwrite reports from programs the job runs:

- `checkout`, `command` and `artifacts` for those parts of the job.
- `hook/agent/<hook>` and `hook/repository/<hook>` for agent and repository hooks, such as `hook/agent/pre-command`.
- `hook/plugin/<plugin>/<hook>` for plugin hooks, such as `hook/plugin/docker/pre-command`.

These reports don't add a line to the log, because the log already has a header for each of them. When the job ends, the agent clears the records that finished successfully, except `command`.

As a proof of concept, captured job errors also emit `state=error`, `id=diagnostic`,
`app=buildkite-agent`, with the redacted diagnostic text in `msg`. This depends on
the existing Local Job API capture producing a diagnostic; automatic command,
hook and Git diagnostics require `BUILDKITE_CAPTURE_AGENT_ERRORS=true`. Phase and
hook status reports remain independent of error capture. Diagnostics use the
executor's output stream, not a child command's potentially redirected stdout.

The Local Job API validates and redacts registered secrets, Buildkite tokens and
URL credentials and queries before truncation and OSC base64 encoding. OSC text
fields replace control characters with spaces and are bounded by the protocol's
size limit. Unregistered secrets that these rules don't recognise can still be
included in diagnostics. Each diagnostic reuses the same record id, so a status
viewer can replace the previous one; consumers needing every report must read
the stream. Emission is best-effort and precedes upstream delivery checks, so it
doesn't depend on backend acceptance. Direct error delivery and job exit status
are unchanged.

The separate `BUILDKITE_OSC_EVIDENCE_POC=true` agent-process setting enables thin
forwarding when the `program-status` experiment is also enabled. It sends the
first 256 valid decoded reports and their redacted-log offsets per job in batches
of up to 32, every two seconds or at stream close. All states consume this cap,
so progress reports can exhaust it before a later error; malformed reports do not.
The agent forwards the supported report fields without deciding which messages
are useful or inferring error codes or root causes. Existing direct API errors
remain independent. This forwarding protocol is experimental and may change.

Each batch has a five-second deadline covering up to three attempts, and stream
shutdown gives all pending evidence a shared five-second budget after required
log uploads finish. Evidence continues draining during those uploads. Non-retryable
responses, exhausted retries, shutdown deadlines and process restarts lose evidence.
Delivery is memory-only, with no disk buffer or durable replay; evidence failure
does not fail the job. Trying this POC requires an agent built with these changes
and explicit opt-in, and tools still need to emit supported reports. It cannot
report failures before an agent starts a job or after the agent disappears.

**Status:** Experimental. The agent doesn't reply to the protocol's feature detection query yet.

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
