# Job Executor Abstraction

> Drafted with GPT-5.4 and revised with Amp, with human input and review.

## Summary

Give the agent a typed, tested abstraction for *how the job-level bootstrap
process is launched*, and add Docker as the first non-local backend.

- `exec` (default): today's behaviour. The agent starts `bootstrap-script` as a
  local subprocess.
- `docker`: the agent runs its own `buildkite-agent bootstrap` inside an
  ephemeral container on the same host, using an operator-chosen image for the
  job's filesystem and tools.
- `kubernetes`: today's Kubernetes stack support, unchanged in behaviour but
  expressed as one more implementation of the same interface. `--kubernetes-exec`
  remains as an alias for `executor=kubernetes`.

The seam is the whole bootstrap process, not the command phase. That is what
makes it reusable for stronger sandboxes later (Firecracker, gVisor, remote
workers) without touching `internal/job`.

## Why

The agent already has the interface this proposal wants. `agent/job_runner.go`
declares:

```go
// jobProcess is either a *process.Process, or a *kubernetes.Runner.
type jobProcess interface {
	Done() <-chan struct{}
	Started() <-chan struct{}
	Interrupt() error
	Terminate() error
	Run(ctx context.Context) error
	WaitStatus() process.WaitStatus
}
```

and `NewJobRunner` picks an implementation with
`if conf.KubernetesExec { kubernetes.NewRunner(...) } else { process.New(...) }`.
Everything downstream (`JobRunner.Run`, log streaming, cancellation polling,
`FinishJob`) already works against `jobProcess`.

Two things are missing:

1. The choice is an `if` on one bool, so there is no room for a third backend
   and no unit test of selection.
2. `bootstrap-script` is the only way to run bootstrap somewhere else. It is a
   command string with no lifecycle: no way to name the thing that was
   launched, cancel it directly, map its exit status, or clean it up. The
   [docker-bootstrap-example](https://github.com/buildkite/docker-bootstrap-example)
   shows both the idea and the limits of a shell-script-only approach: no
   container name, no signal handling, no cleanup, and an image that must
   bundle a matching agent.

The deprecated in-bootstrap Docker integration (`BUILDKITE_DOCKER*`,
`BUILDKITE_DOCKER_COMPOSE_*`) was removed in
[17f5fb47](https://github.com/buildkite/agent/commit/17f5fb47). It only wrapped
the command phase and is not related to this proposal.

## Goals

- Preserve current behaviour by default.
- Keep `buildkite-agent bootstrap` as the unit that runs job phases.
- Let each backend own launch, cancellation, exit-status mapping, and cleanup.
- Land Docker as an opt-in Linux backend that works with ordinary images.

## Non-goals

- Changing bootstrap or `internal/job`. The Docker executor needs no
  bootstrap-side change.
- Removing `bootstrap-script`.
- Full hostile-build isolation. Docker still shares the kernel, and the MVP
  uses Docker's default bridge network unless the operator picks another.
- Changing `--kubernetes-exec` config or the `kubernetes-bootstrap` protocol.
- Per-step images chosen in pipeline YAML. The design leaves room for them
  (see Follow-ups).

## Proposal

### Interface

Rename `jobProcess` to `JobExecution`, add `Cleanup`, and add a factory:

```go
// JobExecutor chooses where and how bootstrap runs. One is built at agent
// start and shared by every job the agent runs.
type JobExecutor interface {
	New(ctx context.Context, req JobExecutionRequest) (JobExecution, error)
}

// JobExecution is one running job. It is the existing jobProcess plus Cleanup.
type JobExecution interface {
	Started() <-chan struct{}
	Done() <-chan struct{}
	Run(ctx context.Context) error
	Interrupt() error
	Terminate() error
	WaitStatus() process.WaitStatus
	Cleanup(ctx context.Context) error
}
```

`JobExecutor` is a factory rather than a function because some executors carry
state from agent start. The Docker executor holds its API client, the resolved
image ID, and the agent binary path, all established once by the start-time
preflight (below).

`JobExecutionRequest` carries what `NewJobRunner` already computes:

- `Job` and `AgentConfiguration`
- the job env slice (the output of `createEnvironment`), *without* the host
  `os.Environ()`. Today `NewJobRunner` computes
  `processEnv := append(os.Environ(), env...)` and hands it to both the exec
  and Kubernetes branches. Those two executions keep doing that prepend
  themselves. Docker does not.
- the job context directory (`jobContextDir(conf)`), which holds
  `BUILDKITE_ENV_FILE`, `BUILDKITE_ENV_JSON_FILE`, and the job-timeout marker
- the job log tmpfile path, when `enable-job-log-tmpfile` is on
- the stdout/stderr writer (`r.jobLogs`)
- `CancelSignal` and `CancelSignalTimeout`

`Run` returning an error means "the executor failed and there is no
trustworthy job result". `runJob` already maps that to exit -1 with
`SignalReasonProcessRunError`. Backends must use that channel for launch and
control-plane failures rather than inventing an exit code. It does not
guarantee the job had no side effects: the Kubernetes runner already returns
errors after containers have started, and Docker can lose its connection to
the daemon after bootstrap has started.

`runJob` type-asserts `r.process.(*kubernetes.Runner)` to print "unknown
container exit status" diagnostics, gated on `r.cancelled` and
`r.agentStopping`. Slice 1 leaves that assertion alone. Hiding it behind an
optional interface is possible later, but the method needs the runner's
cancelled and stopping state as input, so it is not a pure win and is not
required for a third backend.

The interface lives in `agent/`, not `internal/job`, because it decides how
bootstrap is launched. Bootstrap only exists after that decision.

### Selection

Validate the resolved configuration (flags, env vars, and `buildkite-agent.cfg`
combined) at agent start, then select:

```text
--kubernetes-exec and executor unset         -> executor = kubernetes
--kubernetes-exec and executor != kubernetes -> agent start fails: conflicting executors
executor not in {exec, docker, kubernetes}   -> agent start fails: unknown executor
executor=kubernetes                          -> kubernetes execution (wraps kubernetes.Runner)
executor=docker                              -> docker preflight, then docker execution
executor=exec (or unset)                     -> exec execution
```

`executor` is the single canonical value. `--kubernetes-exec` is kept so
`agent-stack-k8s` users see no change, but it is normalised into
`executor=kubernetes` while the configuration is resolved, the same way
`--debug` is folded into the log level in `HandleGlobalFlags`. Nothing after
the selection step looks at the boolean. That includes the places that read it
today: Kubernetes tag discovery and graceful-shutdown handling in
`agent_start.go`, and `jobContextDir` and the Kubernetes plugin filter in
`job_runner.go`. Unknown values are rejected before anything else is
considered, so a typo such as `executor=dokcer` cannot fail open to running
jobs on the host.

### Config surface

New `buildkite-agent start` settings, available as flags, env vars, and
`buildkite-agent.cfg` entries like every other agent option:

| Setting | Values | Notes |
|---|---|---|
| `executor` | `exec` (default), `docker`, `kubernetes` | `--kubernetes-exec` is an alias for `kubernetes` |
| `executor-docker-image` | image ref | required when `executor=docker` |
| `executor-docker-mount` | repeatable, `src:dst[:ro]` | extra bind mounts |
| `executor-docker-env` | repeatable, `KEY=VALUE` or `KEY` | extra container env |
| `executor-docker-network` | network name | default: Docker's default bridge network |

Rules:

- `bootstrap-script` is used only by `exec`. If `executor=docker` and
  `bootstrap-script` was set explicitly, log a warning and ignore it.
- `executor-docker-mount`: both paths absolute. The source must exist at agent
  start, so Docker never creates a missing source as root. The destination may
  not equal or sit inside a path the executor mounts itself (the table under
  Mounts), with one exception: destinations *inside* `/tmp/buildkite-home` are
  allowed, so you can mount ssh keys or git config into `HOME`.
- `executor-docker-env`: `KEY` alone copies the value from the agent's own
  environment at agent start, and agent start fails if it is unset. This is
  how you pass host settings such as `HTTP_PROXY` into the container. `HOME`
  and `BUILDKITE_BIN_PATH` are owned by the executor and rejected. Precedence
  is described under Environment.
- `executor-docker-network` is passed through as the container's network mode.
  `host` is allowed and is the operator's call.
- The image is an agent-pool decision. Users who need different images run
  different queues. The executor is an operator control: the container holds
  the job token and the operator's chosen mounts, so a pipeline-chosen image
  needs an operator allowlist first (see Follow-ups).

## `exec` execution

A straight extraction of the current `else` branch: `shellwords.Split` the
bootstrap script, build `process.Config` with the same `Dir`, `Env`, `PTY`,
`Stdout`, `Stderr`, `InterruptSignal`, and `SignalGracePeriod` as today,
including the SIGKILL→SIGTERM downgrade of the interrupt signal. `Cleanup` is a
no-op. This must be behaviour-preserving and lands first.

## `docker` execution

### Daemon client

The executor talks to the Docker Engine API through the Go client
(`github.com/moby/moby/client`), not by running the `docker` CLI. The client
reads `DOCKER_HOST` and the TLS settings from the agent's environment, and
negotiates the API version with the daemon on first use. It adds about ten
small modules to `go.mod` (`moby/moby/api`, `moby/moby/client`,
`containerd/errdefs`, `distribution/reference`, `docker/go-connections`,
`docker/go-units`, `opencontainers/go-digest`, `opencontainers/image-spec`,
and a few others).

Using the API rather than the CLI matters for correctness, not just style:

- `ContainerWait` reports the container's exit code from the daemon. The CLI
  reports its own exit status, which cannot tell a non-zero container exit
  from a lost daemon connection, and adds its own 125/126/127 codes that
  collide with `job.ExitCodeSetupFailure` and with user command failures.
- Container env travels in the API request body. With the CLI, values on
  `docker create`'s argv are world-readable through `/proc/<pid>/cmdline`,
  `--env-file` cannot carry multi-line values such as `BUILDKITE_COMMAND`, and
  name-only `--env KEY` reads values from the CLI's own environment.
- Errors are typed (`errdefs.IsNotFound` and friends), and every call takes a
  context, so per-call timeouts are ordinary context deadlines.

The client does not read `docker context` settings. The MVP supports a daemon
reachable through `DOCKER_HOST` or the default socket.

### Start-time preflight

When `executor=docker`, `agent start` runs these steps once, after config
validation and before registering. Any failure stops agent start with a
message that names the problem.

1. **Platform.** Linux only. Other platforms fail with an error.
2. **Agent binary.** Resolve `os.Executable()` through symlinks and check with
   `debug/elf` that it has no dynamic loader (`PT_INTERP`). Official builds
   are static (`CGO_ENABLED=0` in `scripts/build-binary.sh`), so this only
   catches custom or distro builds made with cgo, which would fail inside an
   arbitrary image.
3. **Daemon.** `Ping` the daemon, which also negotiates the API version.
4. **Operator mounts and env.** Check that every `executor-docker-mount`
   source exists, and resolve every name-only `executor-docker-env` entry.
5. **Image.** `ImagePull` for platform `linux/<GOARCH>`, then `ImageInspect`
   to resolve the reference to an image ID and confirm its OS and
   architecture. Every `ContainerCreate` uses that ID, not the reference, so a
   tag that moves while the agent runs cannot swap in an image the preflight
   never saw. If the pull fails but the image is already present locally,
   log a warning and use the local image. This covers air-gapped hosts and
   registries the agent cannot authenticate to. The whole step is bounded at
   10 minutes.

Registry credentials come from the agent user's Docker config
(`$DOCKER_CONFIG/config.json`, default `~/.docker/config.json`). The MVP reads
static entries under `auths` only. Credential helpers (`credsStore`,
`credHelpers`) are not supported: an image that needs one must be pulled
ahead of time, and the preflight's local-image fallback picks it up.

The preflight keeps Docker calls out of `New` (which would orphan work if
`StartJob` failed) and out of `Run`'s cancellation path. It also means the pull
that would otherwise happen on the first job happens before the agent accepts
one.

### Agent binary

The container runs the host agent's own binary, not one from the image. The
executor bind-mounts the binary from the preflight read-only at
`/buildkite-agent/bin/buildkite-agent` and makes it the container's
entrypoint, with `bootstrap` as the only argument. This has three effects:

- Bootstrap is always exactly the agent's version. There is no image version
  check and no compatibility matrix.
- Any Linux image that has the job's tools works. You do not have to build an
  image that bundles the agent.
- The executor owns the container's entrypoint, so image entrypoint scripts
  do not run (see Image contract).

The path is resolved once at agent start. After an in-place package upgrade
that replaces the binary on disk, later jobs mount the new binary while the
old agent process keeps running until it restarts. `exec` has the same skew
today, because `bootstrap-script` resolves `buildkite-agent` the same way.

`BUILDKITE_BIN_PATH` is set to `/buildkite-agent/bin`. Bootstrap appends it to
the end of `PATH`, so hooks and commands that call `buildkite-agent` find the
mounted binary. If the image has its own `buildkite-agent` earlier on `PATH`,
those calls use the image's copy instead. That matches `exec`, where an
earlier `buildkite-agent` on the host's `PATH` wins, and the Kubernetes stack,
which also appends its binary directory.

### Lifecycle

```text
New():        build the container spec (no daemon calls)
Run():        close Started()
              ContainerCreate  name=buildkite-job-<jobid>-<random>
                               labels com.buildkite.job-id, com.buildkite.agent-run   [60 s]
              ContainerAttach  stdout+stderr stream -> jobLogs                        [lives with the job]
              ContainerWait    condition next-exit                                    [lives with the job]
              if a cancel was recorded: remove the container, return an error
              ContainerStart                                                          [60 s]
              -> error: launch failed, return the daemon's error
              mark running, deliver any pending signal
              wait for the ContainerWait result
              -> status: drain the attach stream [10 s], WaitStatus = StatusCode
              -> wait error: ContainerInspect [10 s]
                   exited  -> WaitStatus = State.ExitCode
                   running -> attach was lost: ContainerKill, return an error
Interrupt():  record "interrupt", then if running, ContainerKill <cancel-signal> [10 s]
Terminate():  record "terminate", then if running, ContainerKill SIGKILL [10 s]
              if that fails, cancel Run's context so Run returns
Cleanup():    ContainerRemove force=true, volumes=true [10 s], ignoring not-found
```

- `New` makes no daemon calls. `NewJobRunner` calls it before `StartJob`, and
  if `StartJob` fails the runner returns without running `cleanup`, so
  anything created in `New` would be orphaned. Create and start both happen
  inside `Run`, where `runJob` maps an error to exit -1 with
  `SignalReasonProcessRunError` and `cleanup` always runs afterwards.
- Attach and wait are both set up before `ContainerStart`, the same order the
  `docker run` CLI uses. Attaching first means no early output is lost.
  Waiting with `next-exit` registered against a `created` container means the
  exit cannot be missed however fast the job is.
- `ContainerStart` returns once the container is running or has failed to
  start. An OCI or runtime failure (a bad user, a missing mount source, a
  seccomp error) comes back as an error from that call, so there is no state
  polling and no "created but not running" limbo to detect.
- The execution only ever kills or removes the container ID that its own
  `ContainerCreate` returned. The name carries a random suffix so a stale
  container from a crashed agent cannot make create fail with a name clash,
  and removal by name cannot hit something this execution did not create. The
  labels are for operators sweeping leftovers.
- A `ContainerWait` error (for example, the daemon connection dropped) is not
  a job result. `Run` inspects the container: an exited container still has a
  trustworthy exit code, and a running one means the agent lost track of it,
  so `Run` kills it and returns an error.
- Exit status is the container's exit code with `Signaled() == false`, the
  same shape `kubernetes.Runner` uses. Docker does not report the signal
  separately, and inferring one from `128+n` would misreport a command that
  exits 137 on purpose. `exit.Signal` is therefore empty for Docker jobs where
  `exec` reports `SIGTERM` or `SIGKILL`. `SignalReason` (cancel, agent stop) is
  unaffected because `runJob` derives it from runner state. A bootstrap that
  exits 125 still reaches `runJob` and is mapped to -1 as today.
- `Started()` closes at the top of `Run`, before create, not when the
  container is observed running. It means "the launch attempt has begun".
  `jobCancellationChecker` and the log processor in `JobRunner` both block on
  `Started()`, so if it closed any later, a server-side cancel or job timeout
  arriving while create was slow would go unnoticed until create returned.
- Output: when `run-in-pty` is off, the attach stream multiplexes stdout and
  stderr and is split with `stdcopy`. When it is on, the container is created
  with `Tty` and the stream is raw. Both go to the shared `jobLogs` writer.
  After the wait result arrives, `Run` waits up to 10 s for the attach stream
  to reach EOF, then closes it, so the copy goroutine never outlives `Run`.
- Every call except attach and wait has a timeout in addition to `Run`'s
  context. A create that times out is handled like a cancelled create (below).
  An inspect that times out is an executor failure: `Run` attempts a kill,
  then returns an error.
- `Cleanup` passes `volumes=true` because an image `VOLUME` makes every create
  allocate an anonymous volume that a plain forced removal leaves behind.
  Named volumes and bind mounts are unaffected.
- `JobRunner.cleanup` must call `Cleanup` before it removes the env files and
  before `FinishJob` (which can hand the agent its next job), with a bounded
  context derived from `context.WithoutCancel` so a cancelled job still gets
  its container removed. Cleanup failures are logged at error level: a leaked
  container holds the build directory.

### Cancellation

`Interrupt` and `Terminate` can be called at any point. `JobRunner.Cancel`
calls them whenever it runs, including while `Run` is mid-create or before the
container is running. Killing a container that has not started fails, and
`Cancel` returns early on an `Interrupt` error without ever reaching
`Terminate`. So the execution keeps a mutex-protected pending state
(`none → interrupt → terminate`, monotonic) and records the request first.
Then:

- If create is in flight, cancel its context, remove the container by its
  unique name in case the daemon created it before the call was abandoned
  (no ID was returned), and return an error from `Run`. A cancelled create is
  never followed by a start.
- If the container exists but `ContainerStart` has not returned, do nothing
  more. `Run` checks the pending state under the same mutex once start returns
  and delivers the recorded signal. If the request was recorded before start
  was called, `Run` removes the container and returns an error instead of
  starting it.
- If the container is running, kill it with the recorded signal.

Further rules:

- Interrupt succeeds at most once per container. Bootstrap removes its signal
  handler after the first signal, so a repeated SIGTERM would bypass its
  cleanup.
- `Interrupt` returns promptly. A graceful stop with a timeout would block and
  then SIGKILL, duplicating the grace period that `JobRunner.Cancel` already
  enforces before calling `Terminate`. The agent, not Docker, owns the timing.
- A failed kill in `Interrupt` must not abort cancellation. `Interrupt` logs
  the failure at error level and returns nil. The pending state is already
  recorded, so `Cancel` proceeds to the grace period and `Terminate`, which
  retries with SIGKILL, and `Cleanup` finishes with a forced removal
  regardless. If `Terminate`'s kill also fails, `Terminate` cancels the
  context `Run` uses for attach, wait, and inspect, so `Run` returns an error
  instead of waiting on a daemon that is not responding. The container may be
  leaked at that point (see Risks).
- Apply the same SIGKILL→SIGTERM downgrade as `exec`. A SIGKILL to bootstrap
  skips pre-exit hooks and loses the command's exit status.
- Job-level timeouts work unchanged: `Cancel` writes the marker file into the
  job context directory before signalling, and bootstrap reads it via
  `BUILDKITE_AGENT_JOB_TIMEOUT_FILE`. The context directory is a live bind
  mount, so the container sees the file as soon as it is written.

### Container spec

Everything the executor sets on create, in one place:

| Field | Value |
|---|---|
| `Image` | the image ID from the preflight |
| `Entrypoint`, `Cmd` | `/buildkite-agent/bin/buildkite-agent`, `bootstrap` |
| `Env` | see Environment |
| `User` | `<uid>:<gid>` of the agent process |
| `GroupAdd` | the agent process's supplementary groups |
| `WorkingDir` | `build-path` |
| `Tty` | `run-in-pty` |
| `Init` | true |
| `SecurityOpt` | `no-new-privileges` |
| `Tmpfs` | `/tmp/buildkite-home` with `uid=<uid>,gid=<gid>,mode=0700` |
| `Mounts` | see Mounts |
| `NetworkMode` | `executor-docker-network`, when set |
| `Platform` | `linux/<GOARCH>` |

- `Init` makes `docker-init` PID 1, so something reaps orphaned grandchildren
  left behind by hooks and shells. Go does not. `docker-init` forwards
  signals and maps a signalled child to `128+signal`.
- `WorkingDir` matters because `exec` sets `process.Config.Dir` to `BuildPath`
  and bootstrap's shell starts in the process working directory. A bind mount
  alone does not set it.
- `Tty` allocates the PTY inside the container. It is not identical to the
  host PTY `process` sets up (`TERM`, fixed window size). Bootstrap's own
  `BUILDKITE_PTY` for the commands it runs is unaffected.

### Environment

The container's environment is built in layers. Later layers win, and the
executor emits each key once:

1. The image's `ENV` (Docker applies it underneath `Config.Env`). This is where
   `PATH` and toolchain variables usually come from.
2. `executor-docker-env`, resolved at agent start.
3. The job env slice from `createEnvironment`: job env plus the agent's
   `BUILDKITE_*` additions, including the access token, the control-plane OTLP
   exporter variables when delivered, and `BUILDKITE_JOB_LOG_TMPFILE` when
   enabled.
4. Executor-owned values: `HOME=/tmp/buildkite-home` and
   `BUILDKITE_BIN_PATH=/buildkite-agent/bin`.

Two entries from the job env slice are dropped: `BUILDKITE_AGENT_PID` (nothing
in the job uses it, and `buildkite-agent lock` talks to the leader socket, not
the PID) and `BUILDKITE_CONFIG_PATH` (the agent config file is not mounted).

The agent's own `os.Environ()` never reaches the container. That keeps the
host environment out of jobs, and it keeps a job that sets `DOCKER_HOST`,
`DOCKER_CONFIG`, or `HOME` from redirecting the executor's own daemon client,
because job env only ever travels as data in the create request. Anything you
want in the container beyond the job env goes through `executor-docker-env`.

Operator env sits below job env for the same reason `os.Environ()` sits below
job env in `exec` today: the job can override a default the host provides.

The env is delivered in the create request's `Config.Env`. The control-plane
OTLP exporter rule in `api.TracingExporter` (never written to the persisted
job-env files, but delivered through the bootstrap process environment) holds:
`Config.Env` is the process environment. The values do sit in the daemon's
stored container config until `Cleanup` removes the container. See Risks.

### Mounts

Preserve host paths inside the container so that nothing in bootstrap, hooks,
or plugins needs to learn a new filesystem layout:

| Host path (from `AgentConfiguration`) | Container path | Mode |
|---|---|---|
| `build-path` | same | rw |
| `plugins-path` | same | rw |
| `git-mirrors-path` (when set) | same | rw |
| `sockets-path` | same | rw |
| job context dir | same | rw |
| `hooks-path`, each `additional-hooks-paths` entry | same | ro |
| `signing-jwks-file` (when set) | same | ro |
| the job log tmpfile (when `enable-job-log-tmpfile`) | same | ro |
| `/etc/passwd`, `/etc/group` | same | ro |
| the agent binary | `/buildkite-agent/bin/buildkite-agent` | ro |
| each `executor-docker-mount` | as configured | as configured |

Before create, the execution creates every rw directory that does not yet
exist, as the agent user, and checks that every file source exists. Agent
start only guarantees `build-path` today. If Docker is left to create a
missing bind source, it does so as root, and a root-owned `plugins-path` or
mirrors directory then fails the first job. All paths are passed absolute.

`config-path` is deliberately not mounted. It is the agent's own
configuration, which commonly holds the registration token, and bootstrap
does not read it: everything it needs arrives through the environment. Hooks
that read `BUILDKITE_CONFIG_PATH` on the host will not find it in the
container. If you need it there, add it with `executor-docker-mount`.

The MVP assumes the Docker daemon sees the same filesystem and uid space as
the agent process. An agent running in a container of its own with the
daemon socket mounted, or a daemon with user namespace remapping, breaks the
path-preserving and uid-preserving assumptions and is out of scope.

The job context directory already exists as a concept:
`--job-context-dir` / `BUILDKITE_JOB_CONTEXT_DIR` (added for the Kubernetes
stack in [535aeaf2](https://github.com/buildkite/agent/commit/535aeaf2)) is
where the agent puts `BUILDKITE_ENV_FILE`, `BUILDKITE_ENV_JSON_FILE`, and the
job-timeout marker. Bind-mounting that directory at the same path makes all
three visible in the container with no path rewriting. Mounting the default
`os.TempDir()` (usually `/tmp`) into the container is more than we want, so
when `executor=docker` and the operator has not set `job-context-dir`, agent
start defaults it to a dedicated directory such as
`/var/lib/buildkite-agent/job-context`. This has to happen at config
resolution time, before `NewJobRunner` creates the env files there.

`sockets-path` is mounted because the host agent's local API socket lives
there (`agentapi.DefaultSocketPath`), and `buildkite-agent lock` inside the
container needs to reach it. Its default is under the host `$HOME`
(`~/.buildkite-agent/sockets`), which is fine: the path is passed explicitly
via `BUILDKITE_SOCKETS_PATH` and does not depend on `$HOME` in the container.

Mirror locking uses `gofrs/flock`, which works across bind mounts on the same
kernel, so `git-mirrors-path` can be shared between host and container jobs on
one agent.

### User and privilege model

- Always run as the agent process's uid and gid, including `0:0` when the
  agent runs as root, and mount `/etc/passwd` and `/etc/group` read-only so
  the uid resolves to a name. Never inherit the image's `USER`: one that runs
  as root would litter the build path with root-owned files, and one that
  drops to another user could not write to it.
- Add every supplementary group of the agent process (`os.Getgroups()`).
  Setting the user sets only the primary group, and mounting `/etc/group`
  provides names, not memberships. Without this, anything the agent reaches
  through a supplementary group breaks in the container, such as a
  group-writable build, plugin, or mirror directory.
- Always set `no-new-privileges`.
- Do not mount the Docker socket.

Running as root inside a bind-mounted build path creates root-owned files on
the host that break later cleanup, checkout reuse, and mixed host and
container agents. Inheriting the agent uid is the default. An explicit
`executor-docker-user` override can come later.

Mounting the host `/etc/passwd` has a side effect the docker-bootstrap-example
ignores: `$HOME` resolves to the host user's home directory, which does not
exist in the image. The agent itself depends on a usable `$HOME`: the cache
feature reads `os.UserHomeDir()` for its default store and for `~` expansion
in cache paths, and git and ssh want somewhere writable. So the executor sets
`HOME=/tmp/buildkite-home` on a tmpfs owned by the agent uid. Without the
ownership options the tmpfs is root-owned and `HOME` is not writable for a
non-root uid. If jobs need ssh keys or git config inside the container, mount
them under that path with `executor-docker-mount`.

### Image contract

The image supplies the job's **filesystem and `ENV`**. The executor owns the
entrypoint, command, user, working directory, and init, and ignores the
image's `ENTRYPOINT`, `CMD`, `USER`, and `WORKDIR`.

The image must provide:

- Linux, with a variant for the agent host's architecture.
- The configured `shell` (default `/bin/bash -e -c` on Linux, which bare
  `alpine` does not have).
- `git`, and `ssh` if repositories use it.
- CA certificates. The agent binary uses the system certificate pool for API
  calls from hooks and commands (artifact upload, meta-data, pipeline upload),
  and git over HTTPS needs them too.
- Whatever hooks, plugins, and the job's commands call.

Image entrypoint scripts do not run. Official language images set their
toolchains up through `ENV` (`PATH`, `GEM_HOME`, and so on), which still
applies. Setup that lives in an entrypoint (activating a virtualenv,
`conda init`, `nvm`) belongs in an agent `environment` hook. Entrypoints that
switch user with `gosu` or `su-exec` would conflict with the user model anyway.

A container that started does not prove bootstrap ran to completion
correctly: a missing shell or `git` fails inside bootstrap and surfaces as an
ordinary job failure in the log.

### Relationship to the docker-buildkite-plugin

The plugin runs one step's command in a container chosen in pipeline YAML.
Checkout, hooks, and plugin setup still run on the host. The executor is agent
configuration and runs all of bootstrap in the container. They compose only if
the job can reach a Docker daemon, which the MVP does not provide (see
Follow-ups).

## Kubernetes

`kubernetes.Runner` already implements the interface. Wrapping it changes no
behaviour. The Kubernetes stack is not a generic sandbox launcher: the pod
shape is created outside the agent, the agent coordinates several containers
over a socket, and `kubernetes-bootstrap` is the container-side protocol. Keep
calling it what it is.

If Kubernetes later becomes a first-class executor that the agent drives
itself, sandbox choice belongs in `runtimeClassName` and pod settings, not in
separate top-level executors per runtime.

## Delivery slices

Each slice is split into subslices that land as separate, independently
validated commits.

0. **Job log tmpfile env (separate PR, ahead of this work).** `NewJobRunner`
   sets `BUILDKITE_JOB_LOG_TMPFILE` with `os.Setenv`, which mutates the whole
   agent process. With `--spawn` greater than 1 a job can receive another
   job's path. Deliver it in the job env slice instead. This fixes the bug
   for every agent and lets the Docker executor treat it as ordinary job env.
1. **Extract the interface.**
   - 1a: `JobExecutor`, `JobExecution`, and `JobExecutionRequest` in `agent/`,
     the exec execution, the Kubernetes wrapper, and the `Cleanup` call in
     `JobRunner.cleanup`. Both existing executions keep prepending
     `os.Environ()` exactly as today.
   - 1b: the `executor` setting with values `exec` and `kubernetes` only,
     `--kubernetes-exec` normalised into it, and every reader of the boolean
     switched to the executor value. `docker` is rejected until slice 2.
2. **Docker, minimum viable.**
   - 2a: Docker config settings and their validation.
   - 2b: the start-time preflight (platform, static binary, daemon ping,
     mount and env resolution, platform-pinned pull with static registry
     credentials, image ID resolution).
   - 2c: the container spec and environment builder, as a pure function from
     configuration and request to the create request.
   - 2d: the execution: `Run`, `Interrupt`, `Terminate`, and `Cleanup` with
     the pending-signal state.
   - 2e: wiring into selection, `docker` accepted as an `executor` value,
     user-facing documentation, and the real-daemon tests.

## Testing

Slice 0:

- Bootstrap receives `BUILDKITE_JOB_LOG_TMPFILE` and the agent's own
  environment does not contain it after `NewJobRunner`.

Slice 1:

- Table test for executor selection, including rejection of unknown values,
  `--kubernetes-exec` normalising to `executor=kubernetes`, and rejection of
  `--kubernetes-exec` combined with `executor=docker` or `executor=exec`.
- Parity test: the exec execution produces the same `process.Config` as the
  current code for a fixed `JobRunnerConfig`, including the host env prepend.

Slice 2 runs against a fake Docker Engine API (an `httptest` server that
records requests and scripts responses), plus a few real-daemon tests:

- Config validation: `executor-docker-mount` rejects relative paths and
  destinations inside executor-owned mounts, and accepts destinations inside
  `/tmp/buildkite-home`. `executor-docker-env` rejects `HOME` and
  `BUILDKITE_BIN_PATH`, and a name-only entry that is unset fails start.
- Preflight: requests arrive in order (ping, pull, inspect). The pull carries
  `linux/<GOARCH>` and the registry auth from a fixture Docker config. A pull
  failure with the image present locally succeeds with a warning, and without
  it fails start. An image with the wrong OS or architecture fails start. A
  dynamically linked binary fixture fails the ELF check.
- Spec builder: the create request has the image ID (not the reference), the
  entrypoint and command, user, supplementary groups, working dir, `Tty` only
  with `run-in-pty`, `Init`, `no-new-privileges`, the `HOME` tmpfs options,
  and the full mount table. Env precedence follows the four layers, with each
  key once, `BUILDKITE_AGENT_PID` and `BUILDKITE_CONFIG_PATH` absent, and a job
  `DOCKER_HOST` present in the container env but never used by the client.
- Lifecycle:
  - `Started()` closes before the create request.
  - Attach and wait are requested before start.
  - A start error makes `Run` return an error.
  - A wait result's status code becomes `WaitStatus`. A container that exits
    immediately is still mapped normally.
  - A wait error followed by an inspect showing `exited` maps the exit code,
    and one showing `running` produces a kill and a `Run` error.
  - A hanging inspect makes `Run` return an error within the per-call timeout.
- Cancellation:
  - An interrupt during a slow create abandons it, removes by name, never
    starts, and makes `Run` return an error.
  - An interrupt recorded before start is never followed by a start.
  - An interrupt recorded while start is in flight is delivered once start
    returns, exactly once.
  - The SIGKILL downgrade is applied.
  - A failing or hanging kill makes `Interrupt` return nil, and a following
    `Terminate` whose kill also fails makes `Run` return promptly.
  - `Cleanup` removes by ID with force and volumes, and a not-found response
    is not an error.

Real-daemon tests, gated behind a build tag or an env var, because a fake
cannot check these:

- A job runs in a stock image (for example `debian`) with the mounted agent
  binary, as a non-root uid that can write to `HOME` and the build path.
- Cancellation while the container is starting still stops the job.
- Removal leaves no anonymous volume behind for an image with `VOLUME`.

## Risks

- Docker is a weaker boundary than a VM. Present it as better than host
  `exec`, not as safe for untrusted builds.
- Path-preserving bind mounts leak host layout into the container. Accepted
  for compatibility with existing hooks and plugin caches.
- The job env, including `BUILDKITE_AGENT_ACCESS_TOKEN` and the OTLP exporter
  headers, sits in the daemon's stored container config
  (`/var/lib/docker/containers/<id>/config.v2.json`, root-only on disk, and
  visible to `docker inspect`) until `Cleanup` removes the container. Anyone
  who can read either already has root-level access to the host. An agent
  that dies mid-job leaves the container, and its config, behind until an
  operator sweeps it by label. The job token is short-lived.
- `moby/moby/client` is pre-1.0, so its API can change between releases. The
  version is pinned in `go.mod` and upgraded deliberately.
- Exit metadata differs from `exec`: no `exit.Signal` on cancellation, only
  the `128+n` code. Anything keying on the signal name will not see it for
  Docker jobs.
- A bad `executor-docker-image` (missing shell, git, or CA certificates)
  fails every job on the agent. The preflight catches only a missing image or
  a wrong platform. Because create runs by image ID, a `docker image prune`
  while the agent is running also fails every job until the agent restarts.
  The error message names the fix.
- Credential helpers are unsupported in the MVP. Private images behind a
  helper must be pulled ahead of time.
- An unresponsive Docker daemon can leak a container. Every call except
  attach and wait has a short timeout, and `Terminate` can cancel `Run`'s
  context, so `Run` always returns and the agent slot is freed, but the
  container itself may keep running until the daemon recovers. The labels
  exist so an operator can sweep these. `exec` has no equivalent failure mode.
- Naming: `internal/job.Executor` already exists and means "bootstrap runtime".
  Use `JobExecutor` and `JobExecution` at the agent layer and do not shorten
  them.

## Follow-ups

- `executor-docker-expose-socket`: mount the Docker socket and add the socket's
  group, so jobs can use the docker-buildkite-plugin or run `docker` inside
  the container. It hands jobs root on the host, so it stays opt-in.
- `executor-docker-user`, and resource limits (memory, CPUs) if anyone needs
  them.
- Per-step images chosen in pipeline YAML, behind an operator allowlist. The
  mounted agent binary already makes any compatible image usable, so this is
  a policy and validation question, not a mechanism one.
- Docker credential helpers for registry auth.
- Putting `BUILDKITE_BIN_PATH` first on `PATH` for Docker, if images with
  their own older `buildkite-agent` turn out to cause problems.
