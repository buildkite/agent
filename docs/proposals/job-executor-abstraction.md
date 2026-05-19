# Job Executor Abstraction

> Drafted with GPT-5.4, with human input and review.

> Note: this proposal is not about reviving or extending the deprecated `BUILDKITE_DOCKER*` / `BUILDKITE_DOCKER_COMPOSE_*` job behavior. That legacy command-phase integration is separate, deprecated, and not the basis for the proposed executor model.

## Summary

Introduce an agent-side executor abstraction for the job-level bootstrap process.

The first two executors would be:

- `exec`: the current behavior, and the default. The agent starts the configured `bootstrap-script` as a local subprocess.
- `docker`: the agent runs `buildkite-agent bootstrap` inside an ephemeral Docker container, following the same overall pattern as [buildkite/docker-bootstrap-example](https://github.com/buildkite/docker-bootstrap-example).

This creates a stable seam for future sandbox backends such as Firecracker, gVisor, or remote worker APIs without changing the bootstrap itself.

It also leaves room for a Kubernetes-native execution path, where the transport is "run this job in a Pod" and the isolation choice is expressed via `runtimeClassName`, user namespaces, and other Pod-level features rather than by inventing a separate top-level executor for each sandbox runtime.

## Why

Today there are three different concepts in the repo that are close to "how a job runs", but none of them is a clean abstraction:

- The agent starts one job-level subprocess from `agent/job_runner.go`.
- The bootstrap runtime lives in `internal/job/executor.go`.
- Kubernetes is already a special execution mode with its own transport in `kubernetes/runner.go` and `clicommand/kubernetes_bootstrap.go`.

That leaves us with two problems:

1. `bootstrap-script` is a powerful escape hatch, but it is just a command string. It is not a typed backend with a lifecycle, cleanup, or tests for backend-specific behavior.
2. The deprecated Docker logic in `internal/job/docker.go` only wraps the command phase inside bootstrap. It does not execute the full job lifecycle in Docker, which is why it is not a substitute for an agent-level executor.

If we want sandboxing to be a first-class capability, the seam should be around the whole bootstrap process, not just the final command phase.

The proposed Docker executor should therefore be treated as a new agent-level execution backend, not an evolution of the deprecated command-phase Docker integration.

## Goals

- Preserve current behavior by default.
- Keep `buildkite-agent bootstrap` as the unit that executes job phases.
- Make the agent choose a job executor in a structured, testable way.
- Allow each executor to own setup, cancellation, exit status mapping, and cleanup.
- Make Docker an opt-in backend now, while keeping the design broad enough for Firecracker and other providers later.

## Non-goals

- Rewriting bootstrap phases in `internal/job`.
- Replacing the existing `bootstrap-script` escape hatch in this change.
- Extending or modernizing the deprecated `BUILDKITE_DOCKER*` / `BUILDKITE_DOCKER_COMPOSE_*` behavior.
- Solving full hostile-build isolation. Docker still has well-known gaps around network access and Docker socket exposure.
- Collapsing Kubernetes into the new config surface immediately.

## Current Architecture

The current execution chain is:

1. `clicommand/agent_start.go` builds `AgentConfiguration`.
2. `agent/job_runner.go` creates job env files, log streaming, and a `jobProcess`.
3. `jobProcess` is either:
   - a local `process.Process` running `bootstrap-script`, or
   - a `kubernetes.Runner`.
4. The subprocess runs `clicommand/bootstrap.go`, which then executes `internal/job.Executor`.

This is already close to the right seam. The missing piece is replacing the `if kubernetes { ... } else { ... }` branch in `NewJobRunner` with a real executor interface.

## Proposal

Add an agent-side executor interface that owns how the bootstrap is launched.

```go
type JobExecutor interface {
	New(ctx context.Context, req JobExecutionRequest) (JobExecution, error)
}

type JobExecution interface {
	Started() <-chan struct{}
	Done() <-chan struct{}
	Run(ctx context.Context) error
	Interrupt() error
	Terminate() error
	WaitStatus() process.WaitStatus
	Cleanup(context.Context) error
}
```

`JobExecution` intentionally looks like the existing `jobProcess` plus explicit cleanup. That keeps `JobRunner.Run`, log streaming, cancellation polling, and finish-job behavior largely unchanged.

`JobExecutionRequest` should include:

- the job and agent configuration
- the full resolved environment slice
- paths to `BUILDKITE_ENV_FILE` and `BUILDKITE_ENV_JSON_FILE`
- stdout and stderr writers
- build path and other filesystem paths from agent config
- cancel signal and signal grace period

This interface should be implemented at the `agent` layer, not in `internal/job`, because it chooses how bootstrap itself is launched.

## Reconciling Current Kubernetes Support

The current Kubernetes path should be treated as one driver implementation, not as a special case outside the model.

Today, `JobRunner` already chooses between two execution mechanisms in `agent/job_runner.go`:

- local `process.Process`
- `kubernetes.Runner`

So the simplest reconciliation is to replace that branch with a driver factory, while keeping the current Kubernetes transport intact.

A useful mental model is:

- `ExecutionDriver`: agent-side choice of where and how bootstrap runs
- `Execution`: the running job lifecycle handle
- bootstrap transport/protocol: backend-specific coordination details used by some drivers

Under that model:

- `exec` wraps the current local subprocess behavior
- `docker` wraps a local Docker-launched bootstrap
- current Kubernetes support becomes a `kubernetes-stack` driver that wraps `kubernetes.Runner` on the agent side and keeps `kubernetes-bootstrap` as the container-side protocol

This is important because the existing Kubernetes code is not generic sandbox launch logic. It is pod coordination logic for the Buildkite Kubernetes stack:

- the pod shape and container layout are created outside the agent
- the agent distributes environment and execution state across containers
- checkout and command containers run `kubernetes-bootstrap`
- the runner aggregates logs, cancellation, and exit status back into one job result

That means current Kubernetes support maps cleanly onto the proposed driver abstraction, but it should not be confused with a generic "sandbox runtime" driver.

### Compatibility path

The existing `--kubernetes-exec` behavior should remain supported.

Internally, resolution would become:

- if `--kubernetes-exec`, select the current Kubernetes-backed driver
- else if `executor=docker`, select Docker
- else select `exec`

This keeps existing `agent-stack-k8s` users unchanged while allowing the rest of the execution model to become structured.

## Config Surface

Add a new agent config option:

- `executor` with values `exec` or `docker`

MVP Docker-specific config:

- `executor-docker-image`
- `executor-docker-arg` as a repeated escape hatch for extra `docker run` flags
- `executor-docker-expose-socket` default `false`

These should be normal agent configuration settings, defined the same way as existing `buildkite-agent start` options:

- CLI flags on `buildkite-agent start`
- environment variables
- `buildkite-agent.cfg`

In practice, the primary configuration point should be `buildkite-agent.cfg`, because the Docker executor is an agent-level execution choice, not a per-step toggle.

Behavior:

- Default `executor` is `exec`.
- `executor-docker-image` is required when `executor=docker`.
- `bootstrap-script` remains supported and is used only by `exec`.
- If `executor=docker`, `bootstrap-script` is ignored and the agent logs a warning if it was explicitly set.
- `kubernetes-exec` keeps its current behavior for now and remains authoritative when enabled.

The reason to keep `bootstrap-script` as an `exec`-only mechanism is that it is already the compatibility surface. We should not overload it further and then try to infer typed Docker behavior from an arbitrary shell string.

For example:

```cfg
executor="docker"
executor-docker-image="ghcr.io/example/buildkite-agent-executor:2026-03-08"
```

The image should be selected per agent or per agent pool, not inferred from each job. If users need different executor images, they should generally run separate agent pools or queues with different agent configuration.

## Exec Executor

The `exec` executor is a straight extraction of current behavior:

- parse `AgentConfiguration.BootstrapScript`
- create a `process.Process`
- wire stdout, stderr, PTY, working dir, env, signals, and grace period exactly as today

This change should be behavior-preserving and land first.

## Docker Executor

The Docker executor should run the full bootstrap in a container, not just the command phase.

Conceptually:

```text
agent -> docker run ... <image> bootstrap
```

This is the same pattern as the docker bootstrap example, but implemented in Go inside the agent rather than as an external shell script.

### Docker execution contract

The Docker executor should:

- run an attached container so stdout and stderr continue to flow through the existing job log pipeline
- pass the resolved job environment into Docker with `--env KEY` entries, using the generated env from `JobRunner`
- make `BUILDKITE_ENV_FILE` and `BUILDKITE_ENV_JSON_FILE` available inside the container
- run `bootstrap` inside the container image
- use a stable container name derived from the job ID so cancellation and cleanup can address it directly
- remove transient resources on normal exit and on agent-side interruption

The executor image contract should be explicit:

- Linux image
- contains a compatible `buildkite-agent` binary
- can run `buildkite-agent bootstrap`
- contains the tooling needed for full bootstrap execution inside the container, not just the user command

### Mounting strategy

The MVP should preserve path semantics rather than invent a new filesystem model.

Auto-mount these configured paths at the same path inside the container when set:

- build path: read-write
- plugins path: read-write
- sockets path: read-write
- git mirrors path: read-write
- hooks path and additional hooks paths: read-only
- signing key files, config files, or similar explicit file paths: read-only

Generated env files need special handling:

- `BUILDKITE_ENV_FILE` and `BUILDKITE_ENV_JSON_FILE` must be visible inside the container
- the safest compatibility path is to create them under a mounted writable path rather than host-only `/tmp`
- `BUILDKITE_ENV_FILE` in particular may need to remain writable for compatibility with hooks or plugins that append to it

This is less isolated than a pure named-volume approach, but it is much simpler and preserves current agent behavior for hooks, plugin cache, mirrors, and sockets.

### User and privilege model

Follow the same ideas as the example where possible:

- if the agent is not running as root on Linux, run the container as the same uid:gid
- mount `/etc/passwd` and `/etc/group` read-only when needed for name resolution
- set `no-new-privileges`
- do not mount the Docker socket by default

File ownership on host-mounted paths is a real concern:

- if the container runs as `root` and writes into bind-mounted host paths, those files will typically be owned by `root` on the host
- that can break later cleanup, checkout reuse, plugin reuse, or mixed host/container execution on the same agent

So for the MVP, Linux Docker execution should default to inheriting the agent process uid:gid when writing to mounted host paths.

Some images will assume `root` or expect a named user and writable home directory, so the design should allow a future explicit override such as `executor-docker-user`, but root should not be the default.

### Cancellation and cleanup

Do not rely on signal proxying from the `docker run` CLI alone.

The Docker executor should own container lifecycle explicitly:

- `Interrupt()` should call `docker stop --signal <cancel-signal> --time <grace-seconds> <name>`
- `Terminate()` should call `docker kill <name>` followed by forced removal if needed
- `Cleanup()` should remove any named container still present

The attached `docker run` process can still be used for log streaming and exit status, but container lifecycle should be controlled directly by the executor.

### How this differs from the Docker Buildkite Plugin

This proposal is also distinct from the `docker-buildkite-plugin`.

That plugin runs a pipeline step command in Docker and provides step-level controls such as:

- required `image`
- environment propagation
- volume mounts
- optional checkout mounting behavior

By contrast, the proposed Docker executor is an agent-level execution backend.

Key differences:

- the plugin is configured in pipeline YAML per step; the executor is configured on the agent in `buildkite-agent.cfg` or equivalent agent config
- the plugin wraps the step command; the executor runs the full `buildkite-agent bootstrap` lifecycle in Docker
- with the plugin, checkout, hooks, plugin setup, and artifact handling still fundamentally belong to the host-side bootstrap flow; with the executor, those phases run inside the selected execution backend
- the plugin is a build-step tool; the executor is part of the agent’s job runtime model

The two solve different problems:

- the plugin is primarily for step-level toolchain and environment control
- the executor is for choosing where a job runs and establishing a future path to broader sandbox providers

## Kubernetes Direction

Keep this brief in the initial proposal:

- current `kubernetes-exec` support already maps naturally to a driver implementation
- if Kubernetes becomes a first-class executor later, it should be modeled as `executor=kubernetes`
- sandbox choice on Kubernetes should usually be expressed through runtime and Pod settings such as `runtimeClassName`, not separate top-level executors for each sandbox runtime

In practice:

- local Docker on a host is a distinct execution backend
- Firecracker or Kata on Kubernetes is usually a Kubernetes backend plus a runtime profile

## Why this belongs in the agent, not inside bootstrap

The bootstrap only knows how to run job phases after it has already started.

The choice between:

- run locally
- run in Docker
- run in Firecracker
- run via a remote provider

has to happen before bootstrap starts, because it changes:

- where stdout and stderr come from
- how signals are delivered
- how filesystem paths are mounted or mapped
- how cleanup is performed
- which process or sandbox exit status we report upstream

That makes `agent/job_runner.go` the correct layer for the abstraction.

## Simplest Path There

### Phase 1: Extract the current behavior behind an interface

- Add the new executor interface in the agent layer.
- Implement `execExecutor`.
- Replace the local `process.New(...)` branch in `NewJobRunner` with the executor.
- Keep Kubernetes behavior as-is or wrap it behind the same interface without changing config semantics.

This is mostly refactoring and should not change behavior.

### Phase 2: Add opt-in Docker support

- Add `executor=docker` and the small Docker config surface.
- Implement `dockerExecutor`.
- Keep `buildkite-agent bootstrap` unchanged.
- Start Linux-only.

This delivers the user-visible feature while keeping the blast radius small.

### Phase 3: Broaden the interface if needed

After `exec` and `docker` exist, decide whether to:

- move `kubernetes-exec` behind the same driver factory while keeping the old flag as a compatibility alias
- add a Kubernetes executor with runtime-class-driven sandbox selection
- add a Firecracker executor only for non-Kubernetes environments where the agent provisions the VM directly
- add a generic provider registry or plugin model

The important point is that phase 1 and phase 2 should not wait on phase 3.

## Testing

### Phase 1

- unit tests for executor selection
- parity tests showing `exec` builds the same process config as today
- existing `agent` and bootstrap tests should continue to pass unchanged

### Phase 2

- unit tests for Docker CLI argument construction
- unit tests for mount generation from `AgentConfiguration`
- tests for cancellation mapping to `docker stop` and `docker kill`
- integration-style tests using mocked `docker` binaries, similar to existing command-phase Docker tests in `internal/job/integration/docker_integration_test.go`

## Risks and tradeoffs

- Docker isolation is incomplete. This should be presented as a better boundary than host `exec`, not as a complete hostile-build solution.
- Path-preserving bind mounts are pragmatic, but they leak more host structure into the container than a stricter sandbox would.
- Requiring an explicit Docker image in the MVP is slightly less convenient, but it avoids hidden version drift rules.
- There is naming collision with `internal/job.Executor`. Internally we should prefer names like `JobExecutor`, `ExecutionBackend`, or `JobExecution` at the agent layer.

## Recommendation

Land this as an agent-side abstraction in two changes:

1. Refactor the current local subprocess path into `execExecutor` with no behavior change.
2. Add an opt-in `dockerExecutor` that runs the full bootstrap inside a container.

Then evaluate Kubernetes as the next top-level executor, with sandbox flavor controlled by runtime and Pod settings.

That gives us a clean, minimal abstraction now, solves the concrete Docker use case, and creates a real extension point for Firecracker and other sandbox providers later without forcing Kubernetes-specific runtime choices into the wrong layer.
