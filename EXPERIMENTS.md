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

### `pre-bootstrap-hook-timeout`

Opts in to a ten-second default timeout for the agent-side `pre-bootstrap`
hook. The agent rejects the job on timeout rather than running it without a
successful hook result. Without this experiment, `pre-bootstrap` still runs
with no dedicated timeout. See [agent-side bootstrap hooks](docs/agent-start.md#agent-side-bootstrap-hooks)
for configuration and reporting.

**Status:** Experimental; the timeout setting is intended to become a normal
agent option in a later release.

### `git-accelerator-mirror`

Use [git-accelerator](https://github.com/buildkite/git-accelerator) as an optional
pull-through mirror for the build repository:

```bash
buildkite-agent start --experiment git-accelerator-mirror \
  --git-accelerator-url https://cache.example.com \
  --git-accelerator-hosts github.com,git.example.com
```

Configure the HTTPS origin on the agent, not in the pipeline. Eligible jobs
pinned to a commit can clone or fetch through the accelerator. A repository
such as `https://github.com/acme/project.git` maps to
`https://cache.example.com/github.com/acme/project.git`. If you use
`--allowed-repositories`, allow both URLs. A permitted backend-provided mirror
takes precedence. The checkout keeps its canonical `origin` and falls back to
ordinary Git on an accelerator failure. SSH repository URLs and checkout
settings that need custom Git flags or transport config stay on the normal path.
Plugins, submodules, and Git LFS are not routed through this experiment.

`--git-accelerator-hosts` is optional. It matches the canonical repository's
hostname exactly, ignoring case. Use bare hostnames, without ports or
wildcards. Other hosts use normal checkout without an accelerator request.
Omitting the list lets any otherwise eligible host use the accelerator.
This option does not restrict backend-provided mirrors or replace
`--allowed-repositories` or the accelerator's server-side origin rules.

By default the accelerator requests are anonymous. For repositories that use
Buildkite-managed Git credentials, the agent sends the canonical repository's
credential to the configured accelerator. Use only an accelerator you trust
with that credential, and configure an exact-host rule for the upstream host.
The accelerator assumes clients authorized for a repository can see the same
cached objects, which may include objects no longer served by the upstream.
For a private CA, install the CA into the default certificate trust store used
by Git in the checkout environment. The accelerator Git invocation does not
inherit job-provided Git configuration or TLS overrides.

### `git-accelerator-clonekit`

Requires `--experiment git-accelerator-mirror` and an accelerator serving
CloneKit seeds at the configured HTTPS origin. Set
`GIT_ACCELERATOR_CLONEKIT_ENABLED=true` on the accelerator and set its
`GIT_ACCELERATOR_CLONEKIT_PUBLIC_BASE_URL` to the agent's `--git-accelerator-url`.
Eligible fresh clones can use a verified seed, either for the workspace or for
its first on-host Git mirror. An on-host mirror is not required. The accelerator
host allowlist applies; existing checkouts, shallow or sparse checkouts, and
custom clone flags do not use seeds.

Git still fetches refs and any objects missing from the seed. If a seed is
missing or cannot be verified, the agent tries Git through the accelerator
without it. If accelerator Git fails, the agent falls back to canonical Git.
Seed files must come from the configured accelerator origin, without redirects.
For a private CA, trust it in both Git and the bootstrap process's Go trust
store. If a job overrides the operator's `SSL_CERT_FILE` or `SSL_CERT_DIR`, the
agent preserves the job's settings and skips CloneKit and the HTTP check for
anonymous retry. It can still use Git through the accelerator.

### `git-accelerator-clonekit-seed-only`

Requires `--experiment git-accelerator-clonekit` (and therefore
`git-accelerator-mirror`). On a fresh branch checkout pinned to a full commit
SHA, skip `git clone` if the verified seed contains the commit. The agent
installs the seed directly in the workspace, keeps the canonical `origin`, and
does not clone other branches or follow tags. Normal commit verification still
runs and may fetch the requested branch to prove ancestry.

For an eligible fresh branch build with `Commit=HEAD`, the agent resolves the
branch tip through the Accelerator before cloning and keeps that SHA for this
job. If the seed doesn't cover it, the agent fetches that exact SHA into the
seed without following tags. If early lookup fails, the existing `HEAD` checkout
path still applies. If lookup succeeds but seed setup falls back, checkout still
targets the resolved SHA.

For a build submitted with a full SHA that is absent from the seed, the normal
Accelerator clone still runs, using the seed as a reference when available.
Pull requests and on-host mirrors continue to use the existing checkout paths.

### `post-bootstrap-hook`

Enables the agent-side `post-bootstrap` hook after each bootstrap attempt,
including when the bootstrap is killed with SIGKILL. Install the hook in the
agent's configured hooks path and opt in with
`buildkite-agent start --experiment post-bootstrap-hook` or
`experiment="post-bootstrap-hook"` in the agent config. Without the experiment,
the hook is ignored.

This is a last-resort notification backstop: report a problem or notify an
independent watchdog to recover leftover resources, rather than perform recovery
inside the hook. It has a one-second default timeout and runs before the agent
reports the job finished. Hook failure or timeout does not change the job result.
See [agent-side bootstrap hooks](docs/agent-start.md#agent-side-bootstrap-hooks)
for environment, timeout configuration, and shutdown limits.

**Status:** Experimental; the hook and its settings may change or be removed.

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

The report itself stays in the log without its message and title, which only appear in the description line. Messages and titles are decoded before secret redaction, so redacted values are removed from them. Reports that repeat the previous report for the same `id`, or only change its `progress`, don't add a line.

The experiment also sets `BUILDKITE_PROGRAM_STATUS=true` in the job environment, unless the job already sets it. Tools such as the Buildkite CLI (`bk`) only send reports in a Buildkite job when it's `true`. Set it to `false` in the job to turn them off.

`buildkite-agent pipeline upload` and `buildkite-agent artifact upload` send their own reports, with `app=buildkite-agent`, following the same `BUILDKITE_PROGRAM_STATUS` rules as `bk`. `artifact upload` reports its progress as the percentage of files uploaded.

The agent also reports the job's own progress, on records with these ids, so they don't overwrite reports from programs the job runs:

- `checkout`, `command` and `artifacts` for those parts of the job.
- `hook/agent/<hook>` and `hook/repository/<hook>` for agent and repository hooks, such as `hook/agent/pre-command`.
- `hook/plugin/<plugin>/<hook>` for plugin hooks, such as `hook/plugin/docker/pre-command`.

These reports don't add a line to the log, because the log already has a header for each of them. The agent attaches its reports to the end of a log line, so they never come before a section header. When the job ends, the agent clears the records that finished successfully, except `command`.

With OpenTelemetry tracing enabled, reports also become `buildkite.program_status` events on the active hook or phase span. This includes the agent's own reports, program output, repeated reports and progress updates. Events have these attributes:

| Attribute | OSC key | Meaning |
| --- | --- | --- |
| `buildkite.program_status.id` | `id` | Record path. An empty string means the root record. |
| `buildkite.program_status.state` | `state` | `idle`, `working`, `done`, `blocked`, `error` or `clear`. |
| `buildkite.program_status.message` | `msg` | Decoded message, or an empty string when absent. |

Record IDs and messages are secret-redacted before export. Event timestamps record when the agent processes each report. Events do not change the span's error status: an OSC `error` report can appear on a command span that exits successfully. Events export when their span ends, are subject to the OpenTelemetry SDK's event limit, and do not require OTLP job log export.

The [protocol's available keys](https://www.superlogical.com/rex/docs/build/program-status#keys) also include `kind` (`permission`, `question` or `auth`, for `blocked`), `progress` (0–100, for `working` or `blocked`), `app` (program identifier), and `title` (base64-encoded label). These four keys are not exported as event attributes yet. Both `msg` and `title` are base64-encoded on the wire; log descriptions decode them.

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
