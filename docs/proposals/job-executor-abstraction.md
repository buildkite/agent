# Job Executor Abstraction

> Drafted with GPT-5.4 and revised with Amp, with human input and review.

## Summary

Give the agent a typed, tested abstraction for *how the job-level bootstrap
process is launched*, and add Docker as the first non-local backend.

- `exec` (default): today's behaviour. The agent starts `bootstrap-script` as a
  local subprocess.
- `docker`: the agent runs `buildkite-agent bootstrap` inside an ephemeral
  container on the same host.
- `kubernetes-exec`: today's Kubernetes stack support, unchanged in behaviour
  but expressed as one more implementation of the same interface.

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
   container name, no signal handling, no cleanup.

The deprecated in-bootstrap Docker integration (`BUILDKITE_DOCKER*`,
`BUILDKITE_DOCKER_COMPOSE_*`) was removed in
[17f5fb47](https://github.com/buildkite/agent/commit/17f5fb47). It only wrapped
the command phase and is not related to this proposal.

## Goals

- Preserve current behaviour by default.
- Keep `buildkite-agent bootstrap` as the unit that runs job phases.
- Let each backend own launch, cancellation, exit-status mapping, and cleanup.
- Land Docker as an opt-in Linux backend.

## Non-goals

- Changing bootstrap phases in `internal/job`. The only bootstrap-side change
  is a new way to receive its environment (`BUILDKITE_BOOTSTRAP_ENV_JSON_FILE`).
- Removing `bootstrap-script`.
- Full hostile-build isolation. Docker still shares the kernel, and the MVP
  uses Docker's default bridge network.
- Changing `--kubernetes-exec` config or the `kubernetes-bootstrap` protocol.

## Proposal

### Interface

Rename `jobProcess` to `JobExecution`, add `Cleanup`, and add a factory:

```go
// JobExecutor chooses where and how bootstrap runs.
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

`JobExecutionRequest` carries what `NewJobRunner` already computes:

- `Job` and `AgentConfiguration`
- the job env slice (the output of `createEnvironment`), *without* the host
  `os.Environ()`. Today `NewJobRunner` computes
  `processEnv := append(os.Environ(), env...)` and hands it to both the exec
  and Kubernetes branches; those two executions keep doing that prepend
  themselves. Docker does not.
- the names of env entries that must not be persisted: today only the
  control-plane OTLP exporter variables, when `createEnvironment` delivered
  them. `exec` and Kubernetes ignore the set; Docker uses it to decide what
  stays out of the env file (see Environment).
- the job context directory (`jobContextDir(conf)`), which holds
  `BUILDKITE_ENV_FILE`, `BUILDKITE_ENV_JSON_FILE`, and the job-timeout marker
- the stdout/stderr writer (`r.jobLogs`)
- `CancelSignal` and `CancelSignalTimeout`

`Run` returning an error means "the executor failed and there is no
trustworthy job result"; `runJob` already maps that to exit -1 with
`SignalReasonProcessRunError`. Backends must use that channel for launch and
control-plane failures rather than inventing an exit code. It does not
guarantee the job had no side effects: the Kubernetes runner already returns
errors after containers have started, and Docker can lose its attach after
bootstrap has started.

`runJob` type-asserts `r.process.(*kubernetes.Runner)` to print "unknown
container exit status" diagnostics, gated on `r.cancelled` and
`r.agentStopping`. Slice 1 leaves that assertion alone. Hiding it behind an
optional interface is possible later, but the method needs the runner's
cancelled/stopping state as input, so it is not a pure win and is not required
for a third backend.

The interface lives in `agent/`, not `internal/job`, because it decides how
bootstrap is launched; bootstrap only exists after that decision.

### Selection

Validate the resolved configuration (flags, env vars, and `buildkite-agent.cfg`
combined) at agent start, then select:

```text
executor not in {unset, exec, docker}       -> agent start fails: unknown executor
--kubernetes-exec and executor=docker        -> agent start fails: conflicting executors
--kubernetes-exec                            -> kubernetes execution (wraps kubernetes.Runner)
executor=docker                              -> docker execution
otherwise                                    -> exec execution
```

An unset `executor` means `exec`. Any other value is rejected before anything
else is considered, so a typo such as `executor=dokcer` cannot fail open to
running jobs on the host, with or without `--kubernetes-exec`.

`--kubernetes-exec` stays a separate flag so `agent-stack-k8s` users see no
change. Folding it into `executor=kubernetes-exec` as an alias is a later,
optional step.

### Config surface

New `buildkite-agent start` settings, available as flags, env vars, and
`buildkite-agent.cfg` entries like every other agent option:

| Setting | Values | Notes |
|---|---|---|
| `executor` | `exec` (default), `docker` | |
| `executor-docker-image` | image ref | required when `executor=docker` |
| `executor-docker-arg` | repeatable | extra `docker create` flags; escape hatch |
| `executor-docker-expose-socket` | bool, default `false` | mounts `/var/run/docker.sock`; follow-up, not MVP |

Rules:

- `bootstrap-script` is used only by `exec`. If `executor=docker` and
  `bootstrap-script` was set explicitly, log a warning and ignore it.
- `executor-docker-arg` may not override what the executor owns. Reject
  `--rm` (defeats `inspect`), `--restart` other than `no` (defeats "one
  ephemeral execution"), and every singleton flag the executor sets itself:
  `--name`, `--user`/`-u`, `--workdir`/`-w`, `--init`, `--tty`/`-t`, plus
  `--env BUILDKITE_BOOTSTRAP_ENV_JSON_FILE` and mounts targeting the job
  context directory. Match all spellings
  (`--workdir=/x`, `--workdir /x`, `-w /x`). Docker's parser is last-value-wins
  for singletons, so as a second line of defence the executor's own flags are
  appended after the user's args; the reject list exists because relying on
  ordering alone leaves `--rm`-style flags with no safe later value.
- `$HOME` in the container is fixed at `/tmp/buildkite-home` for the MVP (see
  the user model below). A setting can follow if anyone needs to move it.
- The image is an agent-pool decision. Users who need different images run
  different queues.

## `exec` execution

A straight extraction of the current `else` branch: `shellwords.Split` the
bootstrap script, build `process.Config` with the same `Dir`, `Env`, `PTY`,
`Stdout`, `Stderr`, `InterruptSignal`, and `SignalGracePeriod` as today,
including the SIGKILL→SIGTERM downgrade of the interrupt signal. `Cleanup` is a
no-op. This must be behaviour-preserving and lands first.

## `docker` execution

### Lifecycle

Use `docker create` + `docker start --attach` rather than a single
`docker run`, so the container exists with a known identity before it runs and
so launch failures are separable from bootstrap exit codes.

```text
New():        validate config; no docker calls
Run():        write the env file (below); close Started()
              docker create --name buildkite-job-<jobid>-<random> \
                --label com.buildkite.job-id=<jobid> --label com.buildkite.agent-run=<run-id> \
                <flags> <image> bootstrap          (cancellable: Interrupt/Terminate abort it)
              -> record the container ID printed by create; every later call uses the ID
              spawn docker start --attach <id>   (stdout/stderr -> jobLogs)
              poll docker inspect <id> until State.Status is running or exited,
              State.Error is set, or docker start has exited
              -> running: deliver any pending signal, then wait for docker start to exit
              -> exited (fast job): fall through to the final inspect
              -> still created (start failed / State.Error): launch failed; return error
              docker inspect <id> -> State
              -> if State is terminal: WaitStatus = State.ExitCode
              -> if the container is still running: attach was lost; docker kill, return error
Interrupt():  record "interrupt"; docker kill --signal <cancel-signal> <id> if running
Terminate():  record "terminate";  docker kill <id> if running;
              if that fails, cancel Run's subprocess context (start + any inspect)
              so Run returns
(every inspect/kill/rm call has its own 10 s timeout; start --attach does not)
Cleanup():    docker rm --force --volumes <id>   (ignore "no such container")
              remove the env file
```

- `New` makes no docker calls. `NewJobRunner` calls it before `StartJob`, and
  if `StartJob` fails the runner returns without running `cleanup`, so
  anything created in `New` would be orphaned. Both `docker create` and
  `docker start` happen inside `Run`, where `runJob` maps an error to exit -1
  with `SignalReasonProcessRunError` and `cleanup` always runs afterwards.
- The execution only ever kills or removes the container ID that its own
  `docker create` returned. The name carries a random suffix so a stale
  container from a crashed agent cannot make `create` fail with a name clash,
  and cleanup-by-name cannot remove something this execution did not create.
  The labels are for operators sweeping leftovers.
- `docker start --attach` exiting is not the same as the container exiting.
  The CLI returns non-zero both for a non-zero container exit and for losing
  its connection to the daemon while the container keeps running. Always
  `inspect` after `start` returns and branch on `State.Status`, not on the
  CLI's exit code. `WaitStatus` comes only from a container in a terminal
  state. A still-running container after `start` returns is an executor
  failure: kill it and return an error from `Run`.
- The readiness poll must not wait only for `running` or a terminal state. A
  `docker start` that fails in OCI/runtime setup (bad `--user`, missing mount
  source, seccomp error) leaves the container in `created` with `State.Error`
  set, and the CLI exits non-zero; neither is `running` or `exited`. The poll
  therefore also ends when `State.Error` is non-empty or when the `start`
  subprocess has exited, and a container still in `created` at that point is
  a launch failure: `Run` returns an error carrying `State.Error` and the
  CLI's stderr. A container observed already `exited` is a fast job, not a
  failure, and falls through to the normal exit mapping.
- Do not surface the docker CLI's own 125/126/127 exit codes as the job exit
  status: 125 collides with `job.ExitCodeSetupFailure`, which `runJob` already
  rewrites to -1, and 126/127 would look like the user's command failing. A
  bootstrap that itself exits 125 inside the container still reaches `runJob`
  via `inspect` and is mapped to -1 as today.
- `Started()` closes at the top of `Run`, before `docker create`, not when the
  container is observed running. It means "the launch attempt has begun".
  `jobCancellationChecker` and the log processor in `JobRunner` both block on
  `Started()`, so if it closed any later a server-side cancel or job timeout
  arriving while `create` was slow (a busy daemon, or the image ID gone
  after a prune) would go unnoticed until `create` returned. Container
  readiness is tracked privately for signal delivery.
- `Interrupt` and `Terminate` can be called at any point: `JobRunner.Cancel`
  calls them whenever it runs, including while `Run` is mid-`docker create`
  or before the container is running. `docker kill` on a container that is
  created but not yet running fails, and `Cancel` returns early on an
  `Interrupt` error without ever reaching `Terminate`. So the execution keeps
  a mutex-protected pending state (`none → interrupt → terminate`,
  monotonic) and records the request first. Then:
  - If `docker create` is in flight, cancel its context so the CLI exits,
    `docker rm --force --volumes` by the execution's
    unique name in case the daemon created the container before the CLI
    died (no ID was returned), and return an error from `Run`. A cancelled
    create is never followed by a start.
  - If the container exists but is not yet observed running, do nothing more;
    `Run` re-checks the state once the readiness poll sees `running` and
    delivers the recorded signal (or, if `create` returned after the request
    was recorded, removes the container and returns an error).
  - If the container is running, `docker kill` it with the recorded signal.
  Interrupt must succeed at most once per container: bootstrap removes its
  signal handler after the first signal, so a repeated SIGTERM would bypass
  its cleanup. Cancel before `Run` is already handled by `JobRunner.Run`
  returning "job already cancelled before running".
- `Interrupt` must return promptly. `docker stop --time N` is wrong here: it
  blocks for up to N seconds and then SIGKILLs, duplicating the grace period
  that `JobRunner.Cancel` already enforces before calling `Terminate`. The
  agent, not Docker, owns the timing. Run `docker kill` with a short timeout
  so a hung daemon cannot wedge `Cancel`.
- A failed `docker kill` in `Interrupt` must not abort cancellation.
  `JobRunner.Cancel` returns as soon as `Interrupt` returns an error and never
  reaches the grace period or `Terminate`, so a timed-out or refused kill
  would leave the container running with nothing left to escalate. `Interrupt`
  logs the failure at error level and returns nil; the pending state is
  already recorded, so `Cancel` proceeds to the grace period and `Terminate`
  retries with `docker kill` (SIGKILL), and `Cleanup` finishes with
  `rm --force --volumes` regardless. If `Terminate`'s `docker kill` also
  fails, `Terminate` cancels the context that `Run` derives for all of its
  subprocesses, so `docker start --attach` and any in-flight `inspect` exit
  and `Run` returns an error instead of waiting on a daemon that is not
  responding; the container may be leaked at that point and `Cleanup` reports
  it if `rm --force` fails too (see Risks).
- Every control command other than `start --attach` runs under its own short
  timeout (10 s) in addition to `Run`'s context: the readiness-poll
  `inspect`s, the final `inspect`, `kill`, and `rm`. Only `start --attach` is
  unbounded, because it lives as long as the job. An `inspect` that times out
  is an executor failure: `Run` attempts `docker kill`, then returns an error.
  Without this, a daemon that stops answering mid-poll would leave `Run`
  blocked in `inspect` where killing `start` alone would not free it.
- Apply the same SIGKILL→SIGTERM downgrade as `exec`. A SIGKILL to bootstrap
  skips pre-exit hooks and loses the command's exit status.
- Pass `--init` so something reaps orphaned grandchildren left behind by
  hooks and shells; Go does not. Bootstrap is then a child of `docker-init`,
  not PID 1. `docker-init` forwards signals and maps a signalled child to
  `128+signal`.
- Pass `--workdir <build-path>`. `exec` sets `process.Config.Dir` to
  `BuildPath` and bootstrap's shell starts in the process cwd; a bind mount
  alone does not set it.
- Pass `--tty` when `run-in-pty` is on. `docker create --tty` followed by
  `docker start --attach` (no `--interactive`) works with a non-TTY agent
  stdin and merges stdout/stderr, which matches the shared `jobLogs` writer.
  This allocates the PTY inside the container; it is not identical to the
  host PTY `process` sets up (`TERM`, fixed window size). Bootstrap's own
  `BUILDKITE_PTY` for the commands it runs is unaffected.
- Exit status is the raw container exit code with `Signaled() == false`, the
  same shape `kubernetes.Runner` uses. Docker does not report the signal
  separately, and inferring one from `128+n` would misreport a command that
  exits 137 on purpose. `exit.Signal` is therefore empty for Docker jobs where
  `exec` reports `SIGTERM`/`SIGKILL`; `SignalReason` (cancel, agent stop) is
  unaffected because `runJob` derives it from runner state.
- Job-level timeouts work unchanged: `Cancel` writes the marker file into the
  job context directory before signalling, and bootstrap reads it via
  `BUILDKITE_AGENT_JOB_TIMEOUT_FILE`. This requires the context directory to
  be a live bind mount (below), not a copy taken at `docker create` time.
- `Cleanup` uses `--volumes`: the official image declares `VOLUME /buildkite`,
  so every `create` allocates an anonymous volume that `docker rm --force`
  alone leaves behind. Named volumes and bind mounts are unaffected.
- `JobRunner.cleanup` must call `Cleanup` before it removes the env files and
  before `FinishJob` (which can hand the agent its next job), with a bounded
  context derived from `context.WithoutCancel` so a cancelled job still gets
  its container removed. Cleanup failures are logged at error level: a leaked
  container holds the build directory and the env file.

### Environment

Two environments are in play and they must not mix:

- The **control process** environment: what the `docker create`, `start`,
  `kill`, `rm`, and `inspect` subprocesses run with. This is the agent's own
  `os.Environ()`, unmodified. Job env never enters it. A job that sets
  `DOCKER_HOST`, `DOCKER_CONTEXT`, `DOCKER_CONFIG`, or `HOME` must not be able
  to redirect the executor to another daemon or hide the agent user's
  registry credentials. The one addition is agent-owned, not job-owned: the
  control-plane OTLP exporter variables, for the `create` call only (see
  below).
- The **container** environment: the resolved job env slice from
  `createEnvironment` (job env plus the agent's `BUILDKITE_*` additions), with
  the adjustments below. It deliberately excludes the host's `os.Environ()`:
  `PATH`, `HOME`, and the like come from the image, not the host. So do proxy
  settings: an operator whose host agent relies on `HTTP_PROXY` passes it via
  `executor-docker-arg --env HTTP_PROXY=...`.

Transport the container environment through a file, not through `docker`'s
env flags:

- `Run` writes `<job-context-dir>/job-exec-env-<jobid>.json` (mode 0600, a
  single JSON object) before `docker create`, and `Cleanup` removes it. Create
  it with `O_CREATE|O_EXCL` (or `os.CreateTemp`, as `createJobEnvFiles`
  already does) so a pre-existing path or symlink is never followed. The job
  context dir is already bind-mounted, so the path is the same on both sides.
- `docker create` passes exactly one env var:
  `--env BUILDKITE_BOOTSTRAP_ENV_JSON_FILE=<path>`.
- `buildkite-agent bootstrap` honours that variable. The loading has to
  happen before urfave/cli parses flags: `cli.EnvVars` sources are read
  during parsing, ahead of any `Before` hook or the `Action`, so nothing
  inside the `bootstrap` command runs early enough. `main` therefore calls a
  small `clicommand` helper before `app.Run` that, when the variable is set,
  reads the whole JSON object, applies it over the process environment with
  `os.Setenv`, and
  unsets `BUILDKITE_BOOTSTRAP_ENV_JSON_FILE` so hooks and the
  `buildkite-agent` subcommands they run never see it (the JSON may not set
  that key). Explicit flags still win over env, as they do today. A missing,
  unreadable, or malformed file exits with `job.ExitCodeSetupFailure` (125),
  which `runJob` maps to -1. This is the only bootstrap-side change. Options
  considered and rejected: re-`exec`ing bootstrap after loading (a second
  CLI parse, and it cannot rescue a first parse that already failed on a bad
  image env), and a `docker-bootstrap` launcher command like
  `kubernetes-bootstrap` (a second process plus signal forwarding for no
  protocol gain).
- Anything the image runs before `buildkite-agent bootstrap`, such as the
  official image's `/docker-entrypoint.d` scripts and `ssh-env-config.sh`,
  sees only the container environment above, not the job env. Setup that
  depends on job variables belongs in `environment` or `pre-checkout` hooks.

Why not the docker flags:

- `--env KEY` (name only) makes Docker read values from the client process,
  which means the job env would have to be in the control process
  environment. That is the leak above.
- `--env KEY=VALUE` puts every value, including
  `BUILDKITE_AGENT_ACCESS_TOKEN`, in `docker create`'s argv, which is
  world-readable via `/proc/<pid>/cmdline` for the life of the call.
- `--env-file` cannot carry multi-line values, and `BUILDKITE_COMMAND` is
  routinely multi-line. Reusing `BUILDKITE_ENV_FILE` is also out: it is
  `%q`-quoted for the pre-bootstrap hook, Docker's parser does not unquote,
  and it intentionally omits agent-added variables such as the access token
  and the OTLP exporter credentials.

Adjust these before writing the file:

- `BUILDKITE_BIN_PATH`: set to empty. Bootstrap appends it to `PATH`; the
  host path is meaningless in the container and the image must already have
  `buildkite-agent` on `PATH`. (`kubernetes-bootstrap` resolves the same
  problem by recomputing it from `os.Executable`.)
- `BUILDKITE_AGENT_PID`: omit. Nothing in the job uses it; `buildkite-agent
  lock` talks to the leader socket, not the PID.
- `BUILDKITE_CONFIG_PATH`: omit. The agent config file is not mounted (below).
- `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`/`_PROTOCOL`/`_HEADERS` when they came
  from the control-plane exporter: omit from the file. `api.TracingExporter`
  requires that these (the headers can carry vendor credentials) never reach
  a persisted env file, which is why `createEnvironment` adds them only after
  writing today's env files. The runner tells the execution which keys are
  exporter-supplied (`controlPlaneOTLPEnv` produces them, so the set is
  known). The docker execution passes each as a name-only `--env KEY` and puts
  the values in the environment of the `docker create` subprocess only: the
  CLI reads a name-only `--env` from its own environment, so the values cross
  the daemon socket without touching the agent's disk or `create`'s argv.
  They are then held in the daemon's container config until `Cleanup`
  removes the container, root-readable only, which is the same exposure any
  `--env` gives. A job that set its own `OTEL_EXPORTER_OTLP_*` destination
  is unaffected: `createEnvironment` does not deliver the control-plane
  exporter in that case, and the job's own values are job env and go in the
  file as usual.
- `BUILDKITE_JOB_LOG_TMPFILE`: add it when `enable-job-log-tmpfile` is on.
  `NewJobRunner` sets it with `os.Setenv` after `createEnvironment` returns,
  so today it reaches bootstrap only via the host-env prepend that Docker
  drops. The executor takes the path from the runner, puts it in the env
  file, and bind-mounts the log file itself read-only (below); the agent
  appends to the same inode on the host, so the container sees the live log.
- `HOME`: set to `/tmp/buildkite-home`. See the user model below.

### Mounts

Preserve host paths inside the container so that nothing in bootstrap, hooks,
or plugins needs to learn a new filesystem layout:

| Host path (from `AgentConfiguration`) | Mode |
|---|---|
| `build-path` | rw |
| `plugins-path` | rw |
| `git-mirrors-path` (when set) | rw |
| `sockets-path` | rw |
| job context dir (`--job-context-dir`, default `os.TempDir()`) | rw |
| `hooks-path`, each `additional-hooks-paths` entry | ro |
| `signing-jwks-file` (when set) | ro |
| the job log tmpfile (when `enable-job-log-tmpfile`) | ro |
| `/etc/passwd`, `/etc/group` | ro |
| `/var/run/docker.sock` (only when `executor-docker-expose-socket`) | rw |

Before `docker create`, the execution creates every rw directory that does
not yet exist, as the agent user, and checks that every file source exists.
Agent start only guarantees `build-path` today; if Docker is left to create a
missing bind source it does so as root, and a root-owned `plugins-path` or
mirrors directory then fails the first job. All paths are passed absolute.

`config-path` is deliberately not mounted. It is the agent's own
configuration, which commonly holds the registration token, and bootstrap
does not read it: everything it needs arrives through the environment. Hooks
that read `BUILDKITE_CONFIG_PATH` on the host will not find it in the
container; an operator who needs that mounts it via `executor-docker-arg`.

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
start should default it to a dedicated directory such as
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

- Always pass `--user <uid>:<gid>` matching the agent process, including
  `0:0` when the agent runs as root, and mount `/etc/passwd` and `/etc/group`
  read-only so the uid resolves to a name. Never inherit the image's `USER`:
  an image that drops to a non-root user could not read the root-owned 0600
  env file, and one that runs as root would litter the build path.
- Pass `--group-add <gid>` for every supplementary group of the agent process
  (`os.Getgroups()`). `--user` sets only the primary group, and mounting
  `/etc/group` provides names, not memberships. Without this, anything the
  agent reaches through a supplementary group breaks in the container: most
  visibly `/var/run/docker.sock` via the `docker` group when
  `executor-docker-expose-socket` is on, and any group-writable build, plugin,
  or mirror directory.
- Always set `--security-opt no-new-privileges`.
- Do not mount the Docker socket by default.

Running as root inside a bind-mounted build path creates root-owned files on
the host that break later cleanup, checkout reuse, and mixed host/container
agents. Inheriting the agent uid is the default; an explicit
`executor-docker-user` override can come later.

Mounting the host `/etc/passwd` has a side effect the docker-bootstrap-example
ignores: `$HOME` resolves to the host user's home directory, which does not
exist in the image. The agent itself depends on a usable `$HOME`: the cache
feature reads `os.UserHomeDir()` for its default store and for `~` expansion
in cache paths, and git and ssh want somewhere writable. So the executor sets
`HOME=/tmp/buildkite-home` and passes
`--tmpfs /tmp/buildkite-home:uid=<uid>,gid=<gid>,mode=0700`; without the
ownership options the tmpfs is root-owned and `HOME` is not writable for a
non-root uid. Users who need ssh keys or git config inside the container mount
them under that path via `executor-docker-arg`.

### Image contract

- Linux image.
- `buildkite-agent` on `PATH`, at a version that understands
  `BUILDKITE_BOOTSTRAP_ENV_JSON_FILE`. A major-version match is not enough:
  an older image of the same major would ignore the variable and run
  bootstrap with no job configuration. `agent start` enforces this once,
  after config validation and before registering, under a single bounded
  context (2 minutes; `docker pull` dominates): `docker pull <image>`, then
  `docker image inspect --format '{{.Id}}' <image>` to resolve the tag to an
  ID exactly once, then `docker run --rm --entrypoint buildkite-agent <id>
  --version` against that ID. Agent start fails on a version below the
  minimum, a non-zero exit, or timeout. The checked ID is retained and every
  `docker create` uses it rather than the tag, so a tag moving under a
  running agent, or between the check and the first job, cannot swap in an
  unchecked image. This keeps Docker calls
  out of `New` (which would orphan work if `StartJob` failed) and out of
  `Run`'s cancellation path, and means the pull that would otherwise happen
  on the first job happens before the agent accepts one.
- The image entrypoint accepts `bootstrap` as its arguments and ends up
  running `buildkite-agent bootstrap`. The official `buildkite/agent` image
  does: `buildkite-agent-entrypoint` runs `/docker-entrypoint.d`, then
  `exec tini -- ssh-env-config.sh buildkite-agent "$@"`. The executor does not
  override the entrypoint for the job container, so those wrappers keep
  working, but they run under the executor's `--user` with the container
  environment only (see above). With `--init` the image's tini is no longer
  PID 1 and logs a warning to that effect; it still forwards signals.
- A container that started does not prove bootstrap started: an entrypoint
  script failing exits the container with that script's status, and the
  executor cannot tell the two apart.
- Contains everything bootstrap needs for the whole job: git, bash (or the
  configured `shell`), ssh if repositories use it, plus whatever hooks and
  plugins call.

### Relationship to the docker-buildkite-plugin

The plugin runs one step's command in a container chosen in pipeline YAML;
checkout, hooks, and plugin setup still run on the host. The executor is agent
configuration and runs all of bootstrap in the container. They compose: a step
may still use the plugin inside an agent that uses the Docker executor,
provided the socket is exposed.

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

1. **Extract the interface.** Introduce `JobExecutor`/`JobExecution` in
   `agent/`, implement `execExecution` and wrap `kubernetes.Runner`, replace
   the branch in `NewJobRunner` with a selector, add the `Cleanup` call to
   `JobRunner.cleanup`. Both existing executions keep prepending
   `os.Environ()` exactly as today. No config changes; existing tests pass
   unchanged; add a selection test.
2. **Bootstrap env file.** `main` loads `BUILDKITE_BOOTSTRAP_ENV_JSON_FILE`
   before `app.Run`. Small, independently testable, and useful on its own for
   anyone wrapping bootstrap.
3. **Docker, minimum viable.** `executor` with strict value validation,
   `executor-docker-image`, `executor-docker-arg` with the reserved-flag
   check, start-time pull + version check recording the image ID.
   `create`/`start`/`inspect` lifecycle inside `Run` with the pending-signal
   state and readiness poll, env file transport with the OTLP exporter
   carried via name-only `--env`, mount table and source provisioning,
   explicit `--user` with
   `--group-add`, fixed `HOME` on a tmpfs, `--workdir`, `--tty`, `--init`,
   `no-new-privileges`, `rm --force --volumes`. Linux only; error out on other
   platforms.
4. **Follow-ups if needed.** `executor-docker-expose-socket`,
   `executor-docker-user`, `executor-docker-home`, `executor=kubernetes-exec`
   alias.

## Testing

Slice 1:

- Table test for executor selection, including rejection of unknown values
  and of `--kubernetes-exec` combined with `executor=docker`.
- Parity test: `execExecution` produces the same `process.Config` as the
  current code for a fixed `JobRunnerConfig`, including the host env prepend.

Slice 2:

- Bootstrap loads a JSON env file with multi-line values and a value
  containing `=`; an explicit flag still beats a value from the file; the
  variable is absent from the environment bootstrap hands to hooks; a
  missing, unreadable, or malformed file exits 125.

Slice 3, with a fake `docker` binary on `PATH` that records its argv and env
and exits with a scripted status, in the style of the existing
`internal/job/integration` fakes:

- `docker create` argv construction from `AgentConfiguration`: the single
  `--env`, mount table, `--user`, `--group-add`, `--workdir`, `--tty` when
  `run-in-pty` is on, `--init`, `--tmpfs` for `HOME`; rejection of reserved
  `executor-docker-arg` flags in every spelling (`--workdir=/x`,
  `--workdir /x`, `-w /x`); accepted user args appear before the executor's
  own flags in argv.
- The control process env passed to the fake `docker` is exactly the agent's
  environment: a job env `DOCKER_HOST` must not appear in it, and must appear
  in the env file.
- With a control-plane exporter configured, the `OTEL_EXPORTER_OTLP_TRACES_*`
  values are absent from the env file and from `create`'s argv, appear as
  name-only `--env` flags, and are present in the fake `docker create`'s
  environment but not in `start`/`kill`/`rm`/`inspect`'s. With a job-supplied
  OTLP destination they are in the env file and not in the control env.
- `agent start` with `executor=docker` runs `pull`, then `image inspect`,
  then `run --version` against the fake, in that order; the ID from `image
  inspect` is what `run --version` and later `docker create` receive, not the
  tag. Start fails on a too-old version, a non-zero exit, or a fake that
  sleeps past the timeout.
- A fake `docker inspect` that hangs makes `Run` return an error within the
  per-call timeout (after a `docker kill` attempt); `Terminate` during a
  hanging `inspect` or `start` makes `Run` return promptly.
- `Interrupt`/`Terminate`/`Cleanup` issue the expected `docker kill`/`docker
  rm --force --volumes` invocations against the container ID, the SIGKILL
  downgrade is applied, an interrupt during a (fake, slow) `docker create`
  kills the CLI, removes the container by name and makes `Run` return an
  error, `Started()` is closed before `create` is invoked, and an interrupt
  recorded before the container is running is delivered once the readiness
  poll sees it.
- A failing `docker kill` (fake exits non-zero or hangs past the timeout)
  makes `Interrupt` return nil, and a subsequent `Terminate` kills the fake
  `docker start` so `Run` returns an error rather than blocking.
- With `enable-job-log-tmpfile`, `BUILDKITE_JOB_LOG_TMPFILE` is in the env
  file and the file is in the mount list.
- Exit mapping: a `docker create` or `docker start` failure yields a `Run`
  error, including a `start` that exits while `inspect` still reports
  `created` with `State.Error` set (the poll must not wait forever); a
  container already `exited` at the first poll is mapped normally; a
  terminal `State.ExitCode` from `inspect` becomes `WaitStatus`;
  `docker`'s own 125 is never reported as the job exit status; `start`
  returning while `inspect` still shows `running` yields a `Run` error and a
  `docker kill`.

Slice 3 also needs a small set of tests against a real Docker daemon, gated
behind a build tag or an env var, because a scripted fake cannot check these:
a non-root agent uid can read the env file and write to `HOME` and the build
path; cancellation while the container is starting still stops the job;
`docker rm --force --volumes` leaves no anonymous volume behind for an image
with `VOLUME`.

## Risks

- Docker is a weaker boundary than a VM. Present it as better than host
  `exec`, not as safe for untrusted builds.
- Path-preserving bind mounts leak host layout into the container. Accepted
  for compatibility with existing hooks and plugin caches.
- The env file is the first on-disk copy of `BUILDKITE_AGENT_ACCESS_TOKEN`
  the agent writes (the existing env files omit agent-added variables). It is
  0600, lives for one job, and is removed in `Cleanup`; an agent that dies
  mid-job leaves it behind. The job token is short-lived, so this is accepted
  for the MVP; a sweep of stale `job-exec-env-*.json` files at agent start is
  cheap if it turns out to matter.
- Exit metadata differs from `exec`: no `exit.Signal` on cancellation, only
  the `128+n` code. Anything keying on the signal name will not see it for
  Docker jobs.
- A bad `executor-docker-image` (missing tools, entrypoint failing under the
  agent uid) fails every job on the agent. The start-time version check
  catches only an image that is too old. Because `create` runs by image ID,
  a `docker image prune` while the agent is running also fails every job
  ("No such image") until the agent is restarted; the error message names
  the fix.
- An unresponsive Docker daemon can leak a container. Every control command
  except `start --attach` has a short timeout, and `Terminate` can cancel
  `Run`'s subprocess context, so `Run` always returns and the agent slot is
  freed, but the container itself may keep running until the daemon recovers.
  The labels exist so an operator can sweep these; `exec` has no equivalent
  failure mode.
- Naming: `internal/job.Executor` already exists and means "bootstrap runtime".
  Use `JobExecutor`/`JobExecution` at the agent layer and do not shorten them.
