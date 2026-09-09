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

- Changing bootstrap phases in `internal/job`.
- Removing `bootstrap-script`.
- Full hostile-build isolation. Docker still shares the kernel and, by default,
  the host network.
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
- the resolved job env slice (the output of `createEnvironment`)
- the job context directory (`jobContextDir(conf)`), which holds
  `BUILDKITE_ENV_FILE`, `BUILDKITE_ENV_JSON_FILE`, and the job-timeout marker
- the stdout/stderr writer (`r.jobLogs`)
- `CancelSignal` and `CancelSignalTimeout`

`Run` returning an error means "the job never started"; `runJob` already maps
that to exit -1 with `SignalReasonProcessRunError`. Backends must use that
channel for launch failures rather than inventing an exit code.

`runJob` currently type-asserts `r.process.(*kubernetes.Runner)` to print
"unknown container exit status" diagnostics. Move that behind an optional
interface (`ExplainExit(w io.Writer)` or similar) implemented by the
Kubernetes execution so `JobRunner` stops naming a concrete backend.

The interface lives in `agent/`, not `internal/job`, because it decides how
bootstrap is launched; bootstrap only exists after that decision.

### Selection

```text
if --kubernetes-exec      -> kubernetes execution (wraps kubernetes.Runner)
else if executor=docker   -> docker execution
else                      -> exec execution
```

`--kubernetes-exec` stays authoritative so `agent-stack-k8s` users see no
change. Folding it into `executor=kubernetes-exec` as an alias is a later,
optional step.

### Config surface

New `buildkite-agent start` settings, available as flags, env vars, and
`buildkite-agent.cfg` entries like every other agent option:

| Setting | Values | Notes |
|---|---|---|
| `executor` | `exec` (default), `docker` | |
| `executor-docker-image` | image ref | required when `executor=docker` |
| `executor-docker-arg` | repeatable | extra `docker run`/`docker create` flags; escape hatch |
| `executor-docker-expose-socket` | bool, default `false` | mounts `/var/run/docker.sock` |
| `executor-docker-home` | path, default `/tmp/buildkite-home` | `$HOME` inside the container; see below |

Rules:

- `bootstrap-script` is used only by `exec`. If `executor=docker` and
  `bootstrap-script` was set explicitly, log a warning and ignore it.
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
`docker run`, so the container has a name before anything can go wrong and so
launch failures are separable from bootstrap exit codes.

```text
New():        docker create --name buildkite-job-<jobid> <flags> <image> bootstrap
Run():        docker start --attach buildkite-job-<jobid>   (stdout/stderr -> jobLogs)
              on exit: docker inspect --format '{{.State.ExitCode}}' -> WaitStatus
Interrupt():  docker kill --signal <cancel-signal> buildkite-job-<jobid>
Terminate():  docker kill buildkite-job-<jobid>
Cleanup():    docker rm --force buildkite-job-<jobid>   (ignore "no such container")
```

- If `docker create` or `docker start` fails before the container runs, `Run`
  returns an error. Do not surface the docker CLI's own 125/126/127 exit codes
  as the job exit status: 125 collides with `job.ExitCodeSetupFailure`, which
  `runJob` already rewrites to -1, and 126/127 would look like the user's
  command failing.
- `Started()` closes when `docker start` has been spawned. That is early by a
  few hundred milliseconds, which only affects when log streaming and the
  cancellation poller begin; both tolerate that.
- `Interrupt` must be non-blocking. `docker stop --time N` is wrong here: it
  blocks for up to N seconds and then SIGKILLs, duplicating the grace period
  that `JobRunner.Cancel` already enforces before calling `Terminate`. The
  agent, not Docker, owns the timing.
- Apply the same SIGKILL→SIGTERM downgrade as `exec`. A SIGKILL to bootstrap
  skips pre-exit hooks and loses the command's exit status.
- Pass `--init`. Bootstrap is PID 1 in the container and Go does not reap
  orphaned grandchildren left behind by hooks and shells.
- Job-level timeouts work unchanged: `Cancel` writes the marker file into the
  job context directory before signalling, and bootstrap reads it via
  `BUILDKITE_AGENT_JOB_TIMEOUT_FILE`. This requires the context directory to
  be a live bind mount (below), not a copy taken at `docker create` time.

### Environment

Pass the resolved job env as `--env KEY` (name only) for every entry in the
env slice. The `docker` CLI process inherits `os.Environ() + env` exactly as
the `exec` bootstrap does today, so Docker reads the values from there. This
avoids quoting problems with multi-line values and keeps values out of the
`docker create` argv.

`docker --env-file` is not an option: `BUILDKITE_ENV_FILE` is written with Go
`%q` quoting for the pre-bootstrap hook to validate, and Docker's env-file
parser does not unquote.

Override or omit host-specific values before passing them through:

- `BUILDKITE_BIN_PATH`: set to empty. Bootstrap appends it to `PATH`; the
  host path is meaningless in the container and the image must already have
  `buildkite-agent` on `PATH`. (`kubernetes-bootstrap` resolves the same
  problem by recomputing it from `os.Executable`.)
- `BUILDKITE_AGENT_PID`: omit.
- `HOME`: set to `executor-docker-home`. See the user model below.

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
| `config-path`, `signing-jwks-file` (when set) | ro |
| `/etc/passwd`, `/etc/group` (when not running as root) | ro |
| `/var/run/docker.sock` (only when `executor-docker-expose-socket`) | rw |

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

- On Linux, when the agent is not running as root, run the container with
  `--user <uid>:<gid>` matching the agent process and mount `/etc/passwd` and
  `/etc/group` read-only so the uid resolves to a name.
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
`HOME=<executor-docker-home>` and mounts a per-job tmpfs or empty directory
there. Users who need ssh keys or git config inside the container mount them
via `executor-docker-arg`.

### Image contract

- Linux image.
- `buildkite-agent` on `PATH`, of a version compatible with the host agent.
  Enforce at least a major-version match by running
  `docker run --rm --entrypoint buildkite-agent <image> --version` once per
  image per agent process and caching the result.
- The image entrypoint accepts `bootstrap` as its arguments and ends up
  running `buildkite-agent bootstrap`. The official `buildkite/agent` image
  does: `buildkite-agent-entrypoint` runs `/docker-entrypoint.d`, then
  `exec tini -- ssh-env-config.sh buildkite-agent "$@"`. The executor does not
  override the entrypoint for the job container, so those wrappers keep
  working. (`--init` is redundant when the image already runs tini, and
  harmless.)
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
   the branch in `NewJobRunner` with a selector, move the `*kubernetes.Runner`
   assertion behind an optional interface. No config changes; existing tests
   pass unchanged; add a selection test.
2. **Docker, minimum viable.** `executor`, `executor-docker-image`,
   `executor-docker-arg`, `executor-docker-home`. `create`/`start`/`inspect`
   lifecycle, `--env KEY`, the mount table above, uid inheritance, `--init`,
   `no-new-privileges`. Linux only; error out on other platforms.
3. **Follow-ups if needed.** `executor-docker-expose-socket`,
   `executor-docker-user`, version check caching, `executor=kubernetes-exec`
   alias.

## Testing

Slice 1:

- Table test for executor selection.
- Parity test: `execExecution` produces the same `process.Config` as the
  current code for a fixed `JobRunnerConfig`.

Slice 2:

- Unit tests for `docker create` argv construction from `AgentConfiguration`:
  env names, mount table, uid flags, `--init`, `HOME`.
- Unit tests that `Interrupt`/`Terminate`/`Cleanup` issue the expected
  `docker kill`/`docker rm` invocations, and that the SIGKILL downgrade is
  applied.
- Exit mapping tests: a `docker start` failure yields `Run` error; a
  container exit code from `inspect` becomes `WaitStatus`; `docker`'s own 125
  is never reported as the job exit status.
- An integration test with a fake `docker` binary on `PATH` that records its
  argv and exits with a scripted status, in the style of the existing
  `internal/job/integration` fakes.

## Risks

- Docker is a weaker boundary than a VM. Present it as better than host
  `exec`, not as safe for untrusted builds.
- Path-preserving bind mounts leak host layout into the container. Accepted
  for compatibility with existing hooks and plugin caches.
- A bad `executor-docker-image` (missing tools, wrong agent version) fails
  every job on the agent. The version check catches only the second case.
- Naming: `internal/job.Executor` already exists and means "bootstrap runtime".
  Use `JobExecutor`/`JobExecution` at the agent layer and do not shorten them.
