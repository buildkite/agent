# Docker Bootstrap Plan

## Status

Implementation plan with an agreed first-milestone scope. No implementation
exists yet. Later phases describe the intended supported feature, not requirements
for the first milestone.

First-milestone decisions:

- Use the Docker CLI and one agent-configured image, defaulting to
  `buildkite/agent-base:ubuntu-noble-hosted`, maintained in
  <https://github.com/buildkite/agent-base-images>.
- Always mount the host agent binary read-only.
- Reject steps that specify `image` as a setup failure (exit 125). Pipeline image
  selection, resource controls, and per-job networks are deferred.
- Preserve container exit codes without inferring signals; retain JobRunner's
  cancellation reason. OOM diagnostics are best-effort.
- Defer local Linux/OrbStack setup and live Docker validation initially. Start
  with unit tests and Linux builds; live validation remains required to complete
  the executable prototype.

## Objective

Run each Buildkite job's bootstrap lifecycle inside a disposable Docker
container while keeping the long-lived Buildkite agent on the host.

The container runs the existing `buildkite-agent bootstrap` command so the
current plugin, checkout, hook, command, artifact, environment, and exit-status
semantics remain in one implementation.

```text
Linux host
└── buildkite-agent start
    └── JobRunner
        └── buildkite-agent docker-bootstrap
            └── disposable Docker container
                └── buildkite-agent bootstrap
                    ├── plugin setup
                    ├── checkout
                    ├── hooks
                    ├── command
                    └── artifact upload
```

This is a job-container model. It is different from wrapping only the command
phase with a Docker plugin: the checkout and the rest of the bootstrap lifecycle
also run in the container.

## Why this fits the existing agent

The agent already exposes the required interception point. `agent start`
launches a configurable `--bootstrap-script` for every job. `JobRunner` supplies
the job environment and working directory, captures stdout and stderr, forwards
cancellation, and consumes the subprocess exit status.

The first implementation should use this interface:

```text
--bootstrap-script="/usr/bin/buildkite-agent docker-bootstrap"
```

The new `docker-bootstrap` command acts as a host-side container supervisor. It
starts a container whose command is the existing:

```text
buildkite-agent bootstrap
```

This avoids copying or forking the job executor.

Relevant code:

- `agent/job_runner.go`: constructs and supervises the bootstrap process.
- `agent/run_job.go`: maps process completion, cancellation, and setup failures
  to job results.
- `clicommand/agent_start.go`: defines and defaults `--bootstrap-script`.
- `clicommand/bootstrap.go`: constructs the normal job executor.
- `internal/job/executor.go`: runs plugins, checkout, hooks, the command, and
  artifact upload.
- `clicommand/kubernetes_bootstrap.go`: useful prior art for launching the real
  bootstrap in another execution environment and forwarding cancellation. Its
  `rebaseJobContextPaths` helper, `BUILDKITE_BIN_PATH` override, and checkout
  path handling are directly reusable here.
- `internal/process/process.go` and `internal/process/signal_notwindows.go`:
  the interrupt-then-group-SIGKILL sequence that bounds how long the supervisor
  has to clean up after cancellation.

## Initial scope

The first release should have deliberately narrow semantics:

- Linux hosts only.
- A local Docker Engine reachable by the agent.
- One bootstrap container per job.
- An agent-configured default image, with the step-level `image` attribute
  honoured only when agent policy allows it. See "Image selection".
- All normal bootstrap phases run in the container.
- The `pre-bootstrap` hook remains on the host and runs before Docker starts.
- The agent must be started with `--job-context-dir` set to a dedicated
  per-agent directory. See "Job-context directory" below for why the default
  is unsafe to mount.
- Attached stdout and stderr feed the existing job log streamer.
- The container's exit status becomes the bootstrap exit status.
- Containers are always removed after completion.
- The host Docker socket is not mounted into the job container by default.
- Service containers and Docker-in-Docker are deferred until the base
  lifecycle is reliable. Pipeline-selected images are in scope for the MVP,
  gated by agent policy, but not the first milestone.

## Proposed components

### `docker-bootstrap` command

Add a new internal CLI command:

```text
buildkite-agent docker-bootstrap
```

Suggested files:

```text
clicommand/docker_bootstrap.go
clicommand/docker_bootstrap_test.go
internal/dockerbootstrap/runner.go
internal/dockerbootstrap/runner_test.go
```

Register the command in `clicommand/commands.go`.

Keep CLI parsing separate from lifecycle logic. The CLI command should load
configuration and signals; an internal runner should own Docker operations,
state transitions, cleanup, and exit-status handling.

### Bootstrap image

The image contract should require:

- A `buildkite-agent` binary compatible with the host agent.
- Git, SSH, CA certificates, and a supported shell.
- Any interpreters required by organization-level hooks.
- A writable temporary directory.
- A valid user and working directory.

Pipeline-selected images are arbitrary user images and will not contain
`buildkite-agent`. The supervisor must therefore bind-mount the host agent
binary read-only into the container and point `BUILDKITE_BIN_PATH` at that
location. This is the required approach, not a prototype shortcut. The agent is
a statically linked Go binary, so it runs unchanged on glibc and musl images.
An agent-provided default image may additionally bake the binary in, but the
mount is always applied so the host and container versions can never drift.

The supervisor should validate the image before running the bootstrap: the
mounted agent binary must be executable by the container user, and `git` and a
supported shell must be on `PATH`. Report these as a clear image-contract
failure rather than letting the checkout phase fail with a confusing error.

Use Docker's `--init` support, or an equivalent minimal init in the image, so
signals are forwarded and orphaned child processes are reaped correctly.

## Configuration

Start with host-controlled configuration. Do not allow arbitrary job
environment variables to supply raw Docker options.

Proposed settings:

```text
docker-bootstrap-image
docker-bootstrap-image-policy
docker-bootstrap-image-allowlist
docker-bootstrap-image-require-digest
docker-bootstrap-pull-policy
docker-bootstrap-user
docker-bootstrap-memory
docker-bootstrap-cpus
docker-bootstrap-pids-limit
docker-bootstrap-docker-socket
docker-bootstrap-additional-volume
docker-bootstrap-additional-env
```

During the prototype use arguments embedded in `--bootstrap-script` for
host-controlled policy. Do not read policy from unprotected environment variables:
the supervisor inherits job-supplied values. If Docker execution becomes a first-class
agent mode, promote them to normal `agent start` configuration and flags.

Image selection is agent-controlled by default. Pipelines may choose the image
through the step-level `image` attribute only when the agent operator opts in.
The policy, allowlist, plumbing, and signing requirements are covered in "Image
selection" below.

## Image selection

The pipeline YAML already supports a step-level `image` attribute, currently
honoured only on hosted agents where it sets the container image for the whole
job. That is the same job-container model as this design, so self-hosted Docker
mode should honour the same attribute rather than introduce a new one.

### Where the value comes from

The agent decodes each job's step into `pipeline.CommandStep` from
`github.com/buildkite/go-pipeline`. As of v0.18.0 that struct has no typed
`image` field; an `image` key in the accept-job payload lands in the untyped
`RemainingFields` map, and nothing in the agent reads it.

Before implementation, confirm with the backend team:

- That the step payload delivered to self-hosted agents includes `image` rather
  than consuming it server-side for hosted compute only. The existing cache
  warning keyed on `BUILDKITE_COMPUTE_TYPE == "self-hosted"` in
  `agent/run_job.go` shows hosted-oriented attributes do already reach
  self-hosted agents, so this is likely but must be verified.
- Whether the backend also exposes the value as a job environment variable. The
  agent currently references no image-related variable.

Add a typed `Image string` field to `CommandStep` in go-pipeline rather than
reading `RemainingFields`. This validates the value once and makes it available
to signing.

### Signing

Pipeline signing covers command, env, plugins, matrix, and repository URL, plus
secrets and checkout when non-empty (`signature/pipeline_invariants.go` in
go-pipeline, verified in `agent/verify_job.go`). `image` is not signed. With
signing enabled, an altered job payload could therefore swap the image without
invalidating the signature, and that image runs with the job token and the build
path mounted.

Add `image` to the signed fields, included only when non-empty so existing
signatures remain valid, and add the corresponding comparison in
`verifyJob`. This go-pipeline change must ship before pipeline-selected images
are enabled by default in any agent configuration.

### Resolution and precedence

1. `docker-bootstrap-image` is the agent default. It is used only when the step
   sets no image.
2. A step image is honoured only when `docker-bootstrap-image-policy` allows it:
   - `agent-only` (default): a step image is rejected.
   - `allowlist`: the step image must match an entry in
     `docker-bootstrap-image-allowlist`, a list of registry or repository
     patterns such as `123456789012.dkr.ecr.us-east-1.amazonaws.com/ci/*` or
     `ghcr.io/example-org/*`.
   - `any`: any image is accepted. Intended for isolated or single-tenant
     agents only.
3. When `docker-bootstrap-image-require-digest` is set, a step image must be
   pinned by digest. Mutable tags are rejected.
4. A step image that is present but rejected by policy fails the job with a
   message naming the image and the policy that rejected it. Never fall back
   silently to the default image: silent fallback hides misconfiguration and
   runs the job in an image the author did not ask for.
5. Rejection is a setup failure and exits with code 125 so `JobRunner` maps it
   to an agent-side failure, not a command failure. That keeps retry behaviour
   sensible.

### Plumbing

`JobRunner` sees the decoded step, but the supervisor only sees the
environment. `JobRunner` should place the requested image into an agent-set
variable, proposed as `BUILDKITE_JOB_IMAGE`, using `setEnv`. `setEnv` always
overwrites and records any job-supplied value in `BUILDKITE_IGNORED_ENV`, so a
pipeline environment variable cannot smuggle an image past the policy. Add the
variable to the protected list in `env/protected.go`.

The supervisor owns policy evaluation, defaulting, pull, and registry
authentication, because it is the Docker-aware component. It logs the resolved
image reference and digest at the top of the job log so users can see which
image ran.

### Operational notes

- Pre-pull the default image at agent start so the fallback path does not pay
  a cold pull on the first job.
- Registry authentication becomes per-registry once pipelines choose images.
  See "Registry authentication".
- Pull policy matters more with mutable tags. Default to pulling when the tag is
  not present locally, and document that `always` is the safer choice when the
  allowlist permits tags.
- The image contract in "Bootstrap image" applies to pipeline-selected images
  too: git, a shell, SSH, and CA certificates must be present, and the agent
  binary is mounted from the host.

## Per-job lifecycle

The supervisor should implement an explicit lifecycle:

```text
created → pulling → configured → starting → running → stopping → removed
```

For each job:

1. Validate the platform, Docker client, and daemon connectivity.
2. Resolve the image: take the step image from `BUILDKITE_JOB_IMAGE` if set,
   apply the image policy and allowlist, otherwise use the agent default. Exit
   125 on rejection.
3. Derive a unique container name from the job ID plus a random suffix.
4. Pull or inspect the image according to the configured pull policy, using
   credentials for the resolved image's registry.
5. Build the environment, mounts, labels, resource limits, and security options.
6. Create the container with `buildkite-agent bootstrap` as its command.
7. Attach stdout and stderr before starting it, avoiding early output loss.
8. Start the container and wait for completion or cancellation.
9. Inspect the container state and collect its actual exit status.
10. Translate infrastructure failures separately from bootstrap failures where
    possible.
11. Force-remove the container and any per-job network in a deferred,
    idempotent cleanup path.

Every operation should have a bounded timeout. Cleanup must still run when image
pull, create, attach, start, inspect, or cancellation fails partway through.

## Docker client strategy

There are two reasonable implementation stages.

### Stage 1: Docker CLI

Use the installed `docker` CLI for a small prototype. Prefer argument arrays
over shell command strings. Do not place secret values in command-line
arguments.

The supervisor needs more than a simple `docker run --rm` call. It should use an
explicit create/attach/start/inspect/remove sequence so it can:

- Attach before start.
- Identify the exact container during cancellation.
- Distinguish client errors from container exit codes.
- Inspect OOM and runtime state.
- Reliably remove a container if the local Docker CLI process is interrupted.

### Stage 2: Docker Engine API

Adopt a Go Docker Engine client if the feature progresses beyond a prototype.
The API provides structured create, attach, wait, signal, inspect, and remove
operations without parsing CLI output or relying on shell escaping.

The tradeoff is an additional dependency and API compatibility surface. Make
that change only after the execution contract is proven.

## Environment propagation

The bootstrap container must receive the environment assembled by `JobRunner`.
This includes job metadata, command configuration, API credentials, checkout
settings, tracing context, plugin definitions, and paths to coordination files.

`JobRunner` builds the subprocess environment as the host agent's own
environment plus the job environment, with no marker separating the two. The
supervisor therefore needs an explicit selection rule rather than "everything
`JobRunner` set". Propose:

1. Every variable named in `BUILDKITE_ENV_JSON_FILE`. This is the job
   environment as the agent assembled it.
2. Every `BUILDKITE_*` variable present in the supervisor's environment. This
   picks up values that are deliberately kept out of the env files, notably
   `BUILDKITE_AGENT_ACCESS_TOKEN` and the agent API request headers.
3. Control-plane OpenTelemetry exporter variables (`OTEL_EXPORTER_OTLP_*`),
   which `JobRunner` also adds after the env files are written.
4. An agent-configured allowlist (`docker-bootstrap-additional-env`) for
   anything else, such as proxy settings.

Host-only variables such as `PATH`, `HOME`, `USER`, `TMPDIR`, and
`BUILDKITE_JOB_LOG_TMPFILE` must not be forwarded unless the container is given
an equivalent path. `BUILDKITE_JOB_LOG_TMPFILE` in particular is set on the
agent process itself when `--enable-job-log-tmpfile` is on; either mount the
file or drop the variable so hooks do not see a path that does not exist.

Do not serialize secret values into Docker command arguments. With the Docker
CLI, pass environment variable names using repeated `--env NAME` options and let
Docker inherit their values from `docker-bootstrap`. With the Engine API, pass
the environment directly in the create request.

Validate environment names before passing them to Docker. Preserve values
exactly, including whitespace. Add tests for multiline and non-ASCII values.

The parent also exposes job environment files and a job-timeout marker through:

```text
BUILDKITE_ENV_FILE
BUILDKITE_ENV_JSON_FILE
BUILDKITE_AGENT_JOB_TIMEOUT_FILE
```

Their containing job-context directory must be mounted into the container at
the same absolute path. A read-only bind mount is sufficient: host-side creation
of a timeout marker remains visible through the mount.

### Job-context directory

In non-Kubernetes mode `jobContextDir` in `agent/job_runner.go` falls back to
`os.TempDir()`, so env files and the timeout marker are written directly into
the host `/tmp` alongside every other job's files. Mounting that directory into
the container is unacceptable for two reasons:

- It would shadow the container's own `/tmp` with a read-only host directory.
- It would expose other concurrent jobs' env files, which contain user-supplied
  pipeline environment, to this job.

The agent already has a `--job-context-dir` flag. The Docker mode must require
it to point at a dedicated per-agent directory, and JobRunner should
create a per-job subdirectory beneath it before writing coordination files so
only this job's files are mounted. Reuse `rebaseJobContextPaths` from
the Kubernetes bootstrap if the in-container path ever differs from the host
path.

## Filesystem layout and mounts

Initially mount important paths at the same absolute path on the host and in the
container. This avoids path translation throughout the existing bootstrap.

| Host path | Container mode | Purpose |
| --- | --- | --- |
| Build path | read/write | Checkout and build output |
| Plugins path | read/write | Plugin checkout and cache |
| Hooks paths | read-only where compatible | Agent hooks |
| Job-context directory | read-only | Environment and timeout files |
| Git mirrors path | read/write, when configured | `BUILDKITE_GIT_MIRRORS_PATH` and its lock files |
| Additional hooks paths | read-only, one mount per entry | `BUILDKITE_ADDITIONAL_HOOKS_PATHS` is a list |
| Per-job sockets path | read/write | Job API socket |
| Agent API socket | read/write, single file | `buildkite-agent lock` and other agent-level commands |
| Job log tempfile | read-only, optional | Only when `--enable-job-log-tmpfile` is set |
| SSH agent socket | optional | SSH authentication |
| CA and credential paths | optional, read-only | Enterprise trust and repository access |

Override container-specific values such as `BUILDKITE_BIN_PATH`, `PATH`, `HOME`,
and the sockets path when their inherited host values are not valid inside the
image.

### PTY and terminal handling

`agent start` runs the bootstrap in a PTY by default (`RunInPty`), so the
supervisor itself runs under a PTY, and the inner bootstrap will also allocate
PTYs for hooks and the command unless `BUILDKITE_PTY=false` is propagated. The
supervisor must decide explicitly whether the container gets a TTY:

- With a TTY, Docker merges stdout and stderr into one stream, matching what a
  PTY-mode bootstrap produces today.
- Without a TTY, stdout and stderr arrive as separate multiplexed streams. Both
  feed the same `JobRunner` writer, so nothing is lost, but interleaving can
  differ from the host behaviour.

Default to a TTY when the agent is in PTY mode and no TTY otherwise, propagate
`BUILDKITE_PTY` unchanged, and set `TERM` and the terminal size for the
container so ANSI output matches the non-Docker path.

### File ownership

A container running as root can leave root-owned files in the host build path.
The design must make this behavior explicit.

Support an agent-controlled container user. Test both:

- Running as the image's configured user for image compatibility.
- Running as the host agent UID and GID for bind-mount ownership compatibility.

Neither behavior works universally, so the selected policy must be configurable
and validated during container startup.

## Job API

The normal bootstrap starts the per-job Job API. Because the bootstrap and job
commands live in the same container, no host-to-container RPC protocol is
required.

Give each job a private writable sockets directory and set the bootstrap's
socket configuration to that path. Agent subcommands, hooks, and plugins inside
the same container can then use the existing Unix socket normally.

A private directory is required, not merely preferred: `jobapi.NewSocketPath`
names the socket `<pid>-<random>.sock` using the bootstrap's PID. Every
container has its own PID namespace, so with `--init` the bootstrap is the same
low PID in every job and a shared directory would produce colliding names.

Do not expose the Job API socket to other jobs or unrelated host processes.

The agent-level API socket is a separate concern. `buildkite-agent lock` and
related commands connect to `agent-<pid>` under the shared sockets path via
`agentapi.DefaultSocketPath`. If jobs use these commands, that single socket
file must be bind-mounted into the container at the path the bootstrap will
compute from `BUILDKITE_SOCKETS_PATH`, or the supervisor must set the sockets
path so both the private Job API directory and the agent socket resolve. Treat
lock support as a compatibility requirement for the MVP.

## Cancellation and signals

Cancellation is the highest-risk part of the implementation. Terminating the
local `docker` CLI process does not guarantee that the container stops.

When `docker-bootstrap` receives the configured cancellation signal:

1. Mark the job as cancelling and reject duplicate cancellation work.
2. Signal the bootstrap process in the container, allowing command cleanup,
   post-command hooks, artifact upload where applicable, and pre-exit hooks.
3. Wait for the existing cancellation grace period.
4. Stop or kill the container if it remains active.
5. Inspect the final state where possible.
6. Remove the container unconditionally.

### Nested grace periods

`JobRunner.Cancel` sends the cancel signal to the supervisor's process group and
then SIGKILLs the whole group once `CancelSignalTimeout` elapses. The inner
bootstrap receives the same value via `BUILDKITE_CANCEL_SIGNAL_TIMEOUT` and uses
it as the grace period for its own command. Without intervention the
supervisor, its `docker` CLI children, and the container's bootstrap all run out
of time at the same instant, and the supervisor is killed before any deferred
cleanup runs.

The supervisor must therefore:

- Hand the container a shorter `BUILDKITE_CANCEL_SIGNAL_TIMEOUT` than it was
  given, reserving a fixed margin (for example 5 seconds, configurable) for
  stop, inspect, and remove.
- Refuse to start when the configured agent timeout is smaller than the margin,
  with a clear error, rather than silently running with no headroom.
- Set daemon-side auto-remove (`AutoRemove` / `--rm`) on the container at
  create time. The daemon completes removal even if the supervisor is
  SIGKILLed. Explicit removal remains the primary path; auto-remove is the
  backstop.

Note the tradeoff: with auto-remove the container disappears immediately on
exit, so the supervisor must use `docker wait` (or the Engine API wait) to
obtain the exit code. Register exit observation before starting the container
and test fast exits explicitly. Final inspection is best-effort; correctness
must not depend on inspecting a container after it has been removed.

Test cancellation during image pull, create, bootstrap setup, checkout, command,
artifact upload, and post-command cleanup.

The existing bootstrap setup-failure exit code `125` must remain meaningful to
`JobRunner`. Be careful because the Docker CLI also uses exit codes in the
125–127 range for client and invocation failures. Inspection or structured
Engine API errors should distinguish these cases.

## Logs and exit status

Attach container stdout and stderr to the current bootstrap process stdout and
stderr. The existing `JobRunner` log buffer and uploader should remain unaware
that Docker is involved.

The supervisor should report concise infrastructure diagnostics for:

- Docker daemon unavailable.
- Image pull failure.
- Container create or start failure.
- Attach failure.
- Bootstrap binary missing.
- Container OOM kill.
- Container runtime failure.
- Lost daemon connection.
- Forced termination after cancellation timeout.

Do not print the full environment, registry credentials, Docker authorization
configuration, or secret-bearing create requests.

On normal completion, return the inner bootstrap exit status unchanged. Preserve
the agent's existing distinction between a user command failure and an
infrastructure failure that prevented the command from running.

### Signal-death reporting

`runJob` in `agent/run_job.go` populates the reported signal from
`WaitStatus().Signaled()`. Docker's container exit code does not reliably
distinguish an explicit `exit 143` from termination by SIGTERM, and inspect does
not provide a general signal-termination field.

For the first milestone, preserve the container exit code unchanged. Do not
re-raise a signal based on `128+n`, and do not change JobRunner's global exit
mapping. JobRunner retains its existing cancellation reason independently of
signal reporting. Collect OOM diagnostics when available, but treat them as
best-effort because auto-remove can race with inspection.

Exact signal reporting is deferred until there is a reliable explicit reporting
mechanism from the inner bootstrap.

## Docker socket policy

The job container must not receive the host Docker socket by default.

Support an explicit agent-side policy:

```text
docker-bootstrap-docker-socket=none   # default
docker-bootstrap-docker-socket=host
```

`host` bind-mounts:

```text
/var/run/docker.sock:/var/run/docker.sock
```

This gives the job control of the host Docker daemon and is normally equivalent
to root access to the host. It is a compatibility feature, not an isolation
feature. A pipeline-provided environment variable must never be able to enable
it when the agent has disabled it.

Be explicit with adopters about the consequence of the default: the `docker`
and `docker-compose` Buildkite plugins, which most existing pipelines use, need
a Docker daemon and will fail under `none`. Operators choosing `none` are
choosing to run only pipelines that do not build or run containers themselves,
until a `dind` mode exists.

A future `dind` mode could provide a private per-job Docker daemon. Treat that as
a separate project because it introduces daemon readiness, storage, networking,
resource accounting, and nested-container cleanup concerns.

## Networking

Create a uniquely named Docker network per job, even before service containers
are supported. This provides a clean future path for job-scoped services and
avoids coupling jobs through the default bridge.

The initial network policy should provide outbound connectivity and no published
host ports. Evaluate controls for access to:

- Host management endpoints.
- Cloud instance metadata services.
- Other job containers and networks.
- Internal networks not required by the build.

DNS, proxies, custom certificate authorities, VPN routes, IPv6, and MTU behavior
must be included in integration testing.

Check `BUILDKITE_AGENT_ENDPOINT` and any proxy variables during validation.
Some deployments point the agent at a localhost proxy or sidecar. That address
is unreachable from the container's network namespace, so the supervisor should
fail fast with a clear message or, where appropriate, rewrite loopback to the
host gateway address.

## Resource and security controls

Enforce resource limits from host-controlled agent configuration:

- CPU quota.
- Memory and swap.
- PID count.
- File descriptor limits.
- Temporary storage expectations.
- Stop timeout.

Prefer safe defaults where compatible:

- Drop unnecessary Linux capabilities.
- Enable `no-new-privileges`.
- Use Docker's default seccomp profile.
- Do not use privileged mode.
- Do not use host PID, IPC, user, or network namespaces.
- Do not permit arbitrary device mounts.
- Do not permit arbitrary raw Docker option strings from jobs.

Docker shares the host kernel. This design reduces accidental host pollution and
improves job cleanup, but it is not a virtual-machine security boundary.

## Registry authentication

Registry credentials are host-side configuration and must not become part of the
job environment.

For the CLI prototype, use a private temporary Docker configuration directory or
an existing agent-owned credential helper. Remove temporary authentication data
after pull. Do not mount the host's general Docker configuration into the job
container unless explicitly required.

Credentials are resolved per registry from the resolved image reference. The
allowlist in "Image selection" bounds which registries a pipeline can reach, so
operators only need to configure credentials for registries they have allowed.

## Cleanup and reconciliation

Label every resource with stable ownership metadata:

```text
com.buildkite.agent=true
com.buildkite.agent-id=<agent-id>
com.buildkite.agent-name=<agent-name>
com.buildkite.owner=<hostname>/<spawn-index>
com.buildkite.job-id=<job-id>
com.buildkite.bootstrap=docker
```

Normal cleanup should remove the container and per-job network regardless of job
success or failure.

Agent or host crashes can bypass deferred cleanup. Before starting a new Docker
job, reconcile stale resources owned by the same agent.

The agent ID is issued at registration and changes every time the agent
restarts, so it cannot be the reconciliation key: a restarted agent would never
match the previous process's orphans. Reconcile on a stable owner label such as
agent name or hostname plus spawn index, and keep `agent-id` only for
diagnostics. Because several workers on one host share a daemon, reconciliation
must also confirm the container's job is not currently running (for example by
checking the job ID against the agent's active jobs or requiring the container
to be older than a threshold and in an exited state) before removing it.

Cleanup must be idempotent: a missing container or network is already clean, not
an error that should fail an otherwise completed job.

## Observability

Add structured fields and metrics without logging secrets:

- Job ID and container ID.
- Image reference and resolved digest.
- Pull, create, start, bootstrap, stop, and cleanup durations.
- Container exit code and OOM state.
- Graceful versus forced cancellation.
- Cleanup and stale-resource reconciliation failures.

Use separate error categories for Docker infrastructure failures and normal
bootstrap failures so retry policies can distinguish them.

## Testing strategy

### Unit tests

- Configuration defaults and validation.
- Image policy: `agent-only` rejects a step image, `allowlist` pattern
  matching, `any`, digest requirement, and no silent fallback to the default.
- `BUILDKITE_JOB_IMAGE` cannot be set from the job environment.
- Container names, labels, and network names.
- Environment-name handling without exposing values.
- Mount construction and protected paths.
- Docker socket disabled by default.
- Container user selection.
- Lifecycle transitions.
- Exit-status mapping.
- Cancellation escalation.
- Idempotent cleanup after every partial state.
- Redaction of error messages and diagnostic output.

Use a small Docker client interface so lifecycle tests can inject deterministic
pull, create, attach, start, wait, inspect, signal, and remove failures.

### Docker integration tests

Gate integration tests on an explicitly enabled local Docker daemon. The
existing real-agent end-to-end harness in `internal/e2e`, driven by
`.buildkite/pipeline.e2e.yml`, is the natural home for these rather than a new
harness. Cover:

- A successful command.
- A non-zero command exit.
- Plugin, checkout, hook, command, and artifact phases.
- Environment mutations made by hooks.
- Job API use from a hook or command.
- Multiline and non-ASCII environment values.
- Graceful cancellation and post-command cleanup.
- Forced cancellation after timeout.
- Cancellation during image pull and checkout.
- Missing image, bad entrypoint, and unavailable daemon.
- A step-selected image that lacks `git`, failing with an image-contract error.
- A step-selected image rejected by policy exiting 125 with the reason in the
  job log.
- OOM termination.
- Parallel jobs with unique resources.
- File ownership in bind-mounted paths.
- Agent restart followed by stale-resource reconciliation.
- Absence of the host Docker socket by default.
- Presence of the socket only when enabled by agent policy.
- No secret values in process arguments or supervisor logs.
- `buildkite-agent lock` from inside a job.
- Cancellation with the shortest supported grace period, verifying the
  supervisor still removes the container before it is SIGKILLed.
- Explicit exits such as 143 preserved without fabricating a signal; cancellation
  retains the correct job-result reason.
- Git mirrors enabled, with a mirror shared across two concurrent jobs.

## Delivery phases

### Phase 1: executable prototype

- Add `docker-bootstrap` as an internal command.
- Use the Docker CLI.
- Use one agent-configured image, defaulting to
  `buildkite/agent-base:ubuntu-noble-hosted`, with the host binary mounted.
- Reject any step specifying `image` before launching Docker. Detect presence
  in the decoded step in JobRunner, including invalid or empty values; the
  prototype does not need the Phase 2 typed go-pipeline field to reject it.
- Bind-mount the minimum required paths.
- Attach logs and propagate normal exit codes.
- Implement graceful stop, forced kill, and unconditional removal.
- Exercise it through the existing `--bootstrap-script` setting.

Success criteria: a local job completes plugin, checkout, command, hook, and
artifact behavior inside the container, and cancellation leaves no running
container.

### Phase 2: supported MVP

- Add complete configuration validation.
- Add pull policies and per-registry authentication.
- Add step-level `image` support: typed field in go-pipeline, `image` in the
  signed fields, `BUILDKITE_JOB_IMAGE` plumbing, and the policy and allowlist.
- Add UID/GID handling.
- Add resource limits and baseline security options.
- Add per-job networks.
- Add labels and stale-resource reconciliation.
- Add Docker-gated end-to-end tests.
- Document compatibility differences and operating requirements.

Success criteria: concurrent production-like jobs complete reliably, failures
are diagnosable, and all tested shutdown paths clean up their resources.

### Phase 3: first-class execution mode

If the MVP proves useful, replace the configured bootstrap-script convention
with a native agent execution setting. Implement a Docker-backed job process
alongside the existing local process and Kubernetes runner.

This phase should preserve the same container contract rather than changing job
semantics. It primarily improves configuration, typed infrastructure errors,
daemon lifecycle handling, and observability.

### Phase 4: optional extensions

Consider independently:

- Job-scoped service containers.
- Private Docker-in-Docker daemons.
- Read-only root filesystems and declared writable paths.
- Ephemeral named volumes instead of host build-path mounts.
- Shared read-only caches with explicit trust boundaries.

Do not make these extensions prerequisites for the single-container bootstrap.

## Key risks and decisions

Resolve these before declaring the feature supported:

1. **Image authority:** decided as agent-policy-gated step `image`, defaulting
   to `agent-only`. Remaining open items are backend delivery of the field to
   self-hosted agents and the go-pipeline signing change.
2. **Docker socket:** whether host-daemon compatibility is allowed and how it is
   visibly marked as unsafe.
3. **File ownership:** image user versus host UID/GID behavior.
4. **Workspace storage:** host bind mount versus ephemeral Docker volume.
5. **Agent binary:** baked-in version versus a read-only host binary mount.
6. **Environment transport:** Docker CLI inheritance versus Engine API.
7. **Cancellation:** exact signal, nested grace-period margin, escalation, and
   reliable exit reporting without inferring signals.
8. **Infrastructure failures:** how they map to Buildkite retry behavior.
9. **Network policy:** metadata, host, and internal-network access.
10. **Compatibility:** which existing hooks and plugins rely on host-only paths,
    binaries, sockets, or services.

## Recommended first milestone

Build the smallest end-to-end vertical slice:

1. Default image `buildkite/agent-base:ubuntu-noble-hosted`, with the host
   agent binary mounted read-only.
2. A `docker-bootstrap` command selected through `--bootstrap-script`.
3. Same-path mounts for the build and per-job job-context directories, with
   `--job-context-dir` required.
4. Environment propagation by variable name using the selection rule above.
5. Explicit create, attach, start, wait, inspect, and remove operations, with
   daemon-side auto-remove as a backstop.
6. Signal forwarding with a reduced inner grace period, timed stop-to-kill
   escalation, and unchanged container exit codes without signal inference.
7. Unit tests for success, command failure, step-image rejection, cancellation,
   and cleanup, plus a Linux build. Defer live Docker validation and OrbStack
   machine setup initially; these remain required before milestone completion.
8. No Docker socket, custom volumes, service containers, or pipeline-selected
   image in the first milestone. Reject steps specifying `image` with a clear
   setup-failure message and exit 125 until Phase 2 enables policy-gated selection.

That milestone validates the architectural seam and the hard lifecycle behavior
before expanding the configuration or security surface.
