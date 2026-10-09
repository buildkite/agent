# Docker per job (prototype)

**Status: throwaway prototype, not supported.** It exists to test an idea
against real pipelines before any agent changes. Related, and further along:
[#4328](https://github.com/buildkite/agent/pull/4328) (Docker bootstrap
supervisor) and [#3742](https://github.com/buildkite/agent/pull/3742)
(`executor=` proposal). The cleanup approach here is meant to feed into those.

A long-lived, registered agent runs each job's whole lifecycle (hooks,
checkout, plugins, command, artifact upload) in a fresh container, without
disconnecting or re-registering. The job container gets the host Docker
socket, so jobs can run their own containers. No agent code changes: it uses
`--kubernetes-exec`, which already supports running the bootstrap in another
container over a socket in `--job-context-dir`.

```
┌──────────────── dedicated Docker daemon ─────────────────┐
│ buildkite-agent start           buildkite-job-<id>       │
│  --kubernetes-exec      ◀─sock─▶ kubernetes-bootstrap     │
│  --spawn 1                        └ bootstrap (all phases)│
│  pre-bootstrap:                   docker.sock ──▶ jobs'   │
│   1. reap + confirm                    own containers     │
│   2. docker run job container                             │
└──────────────────────────────────────────────────────────┘
```

## Trust model and isolation

For **trusted** jobs that need a clean environment, not a security boundary.
Any job with the host Docker socket is root on the host: it can run privileged
containers, mount `/`, read the agent's configuration and token, and retag
cached images. This prototype gives each job a fresh filesystem, toolchain
and process tree, and removes everything jobs created before the next job
starts. Nothing more.

## How cleanup works

Docker can't tell which job created a resource when jobs call the socket
directly, so labels and name prefixes can't be trusted. Instead the daemon is
dedicated to **one agent worker** (`spawn=1`) and cleanup works by elimination:
[`bin/docker-per-job-reap`](bin/docker-per-job-reap) removes every container,
network and volume not in [`keep.list`](keep.list), leaves any swarm, deletes
stale files in the job context directory and old per-job directories, then
lists everything again and fails if anything is left. Images and the build
cache are kept so pulls stay warm.

The reap runs:

- in `agent-startup`, so a restart after a crash starts clean;
- in `pre-bootstrap`, before every job, so whatever the previous job left
  (including after a forced cancel) is gone before the next job starts.

If cleanup can't be confirmed, the host is **poisoned**: `pre-bootstrap`
writes `$BK_DPJ_STATE_DIR/POISONED`, fails the job (exit status -1,
`agent_refused`) and stops the agent; `agent-startup` then refuses to start
until an operator investigates and deletes the file.

## Setup

1. A Linux host (or VM) with a Docker daemon used only by this agent.
2. Build the job image: `docker build -t buildkite-docker-per-job -f Dockerfile.job .`
   Any image with bash, git and the docker CLI works.
3. Copy this directory to `/etc/buildkite-agent/docker-per-job/`, make
   `hooks/*` and `bin/*` executable, and edit
   [`docker-per-job.env`](docker-per-job.env) and [`keep.list`](keep.list).
   Requires bash and jq on the host.
4. Configure the agent from [`buildkite-agent.cfg`](buildkite-agent.cfg) and
   set `BUILDKITE_CONTAINER_COUNT=1` in its environment. With systemd:

   ```ini
   [Service]
   Environment=BUILDKITE_CONTAINER_COUNT=1
   # Optional; this is the default:
   Environment=BK_DPJ_CONFIG=/etc/buildkite-agent/docker-per-job/docker-per-job.env
   Restart=on-failure
   RestartSec=30
   ```

### Paths must match everywhere

Jobs pass paths to the host daemon in bind mounts (`docker run -v "$PWD:/src"`),
and the daemon resolves them on the host. So the job directories, job context
directory, hooks directory and agent binary are mounted into the job container
at the **same path** as on the host. If the agent itself runs in a container
on the same daemon, those paths must also be mounted into it at the same
paths, and its container must be in `keep.list`. (Not tested.)

## What was tested

Agent built from this branch, a Docker 29.8.1 daemon, a real
`buildkite-agent start` process using these hooks, and a fake Buildkite
backend (the repo's test server plus control endpoints). **Not tested against
the real Buildkite backend, and not with the agent in a container.**

| Case | Result |
|---|---|
| Agent starts with a stale container, network, volume, socket and job dir | All removed before the first job |
| Normal jobs | Exit 0 and 3 reported; multiline env and access token arrive; Job API works in the container and its socket is visible to sibling containers |
| Job bind-mounts its checkout into a sibling container | Works (same-path job directory) |
| Job leaves a container, network, volume and a `--restart=always` container | All removed before the next job, which sees only its own container |
| Graceful cancel | Container exited before FinishJob |
| Forced cancel (`pre-exit` outlives the grace period) | FinishJob sent while the job container is still running; the next job's reap removes it |
| Agent SIGKILLed mid-job, then restarted | Startup reap removes the job's leftovers, the stale socket and env files; the next job runs |
| Kept container attached to a non-kept network | Job fails `-1`/`agent_refused`; agent stops; restart refused while POISONED |
| Job left a swarm with a service | Reap leaves the swarm and removes its networks |
| Daemon unreachable | Reap fails, so the host is poisoned |

## Known gaps

These are what an agent-level implementation (`executor=docker` or #4328)
should fix:

- **Teardown isn't confirmed before FinishJob, only before the next job.**
  Even on normal completion the job container can still be running when
  FinishJob is sent; after a forced cancel, the job's own containers keep
  running until the next job starts.
- **Lost log tail.** After a forced cancel, the end of the log (for example
  `pre-exit` output) isn't uploaded; it's only in
  `docker logs buildkite-job-<id>` until the next reap.
- **Cancelled jobs report exit status -1**, not the command's status
  (`kubernetes-bootstrap` behaviour).
- **`--kubernetes-exec` side effects.** The first SIGTERM stops the agent
  ungracefully, cancelling the running job (a plain `systemctl stop` doesn't
  wait for the job). The k8s path is documented as not a general container
  launcher, so this is only a stopgap.
- **Poisoning stops the agent by signalling it** (found by walking up the
  process tree). If that fails, a poisoned host keeps rejecting jobs.
- **Rejection reasons are only in the agent log.** The job log just says the
  pre-bootstrap hook rejected the job.
- **Not reaped:** images (a job can retag or delete them), host files written
  through bind mounts, and anything else a job does to the host through the
  socket.
- **One worker per daemon.** Scale with more hosts or VMs, or move to a
  per-job daemon.
