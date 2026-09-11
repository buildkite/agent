# VM sandbox prototype

Experimental, opt-in execution mode for `buildkite-agent start`: one long-running registered agent, but each job's complete `buildkite-agent bootstrap` runs inside a fresh, disposable Firecracker microVM on the same Linux host. The agent keeps registration, job acceptance, log upload and cancellation; the guest does checkout, hooks, plugins, the command and artifact upload, with its own dockerd.

This is a prototype for the buildkite-operator (bko) design work. It's not a production sandbox. See "Limitations" before drawing conclusions from it.

## How it fits together

```
host (Linux, KVM)                              guest (Firecracker microVM, per job)
┌──────────────────────────────────────┐        ┌──────────────────────────────────────┐
│ buildkite-agent start --vm-sandbox   │        │ /sbin/init (init.sh, PID 1)          │
│   AgentWorker: register, accept job  │        │   mounts, /dev symlinks, resolv.conf │
│   JobRunner ── vmsandbox.Runner      │        │   dockerd (data on the job's disk)   │
│     cp rootfs.ext4 -> state/<job>/   │        │   buildkite-agent vm-guest-bootstrap │
│     write vm.json, start firecracker │ vsock  │     <- start{env, env files}         │
│     listen on vsock uds ─────────────┼────────┼──>  ready / output / exit            │
│     stream output -> job log         │        │     runs `buildkite-agent bootstrap` │
│     ctx cancel -> int, then kill     │        │        (checkout, hooks, plugins,    │
│     teardown: off, wait, SIGKILL,    │        │         command, artifacts, Job API) │
│       confirm exit, rm state/<job>   │        │     <- off ; powers the machine off  │
└──────────────────────────────────────┘        └──────────────────────────────────────┘
        tap bko-tap0 172.16.0.1/30  <── eth0 172.16.0.2 (NAT out via the host)
```

Host-side code lives in [`internal/vmsandbox`](..): `sandbox.go` (installation checks, poison marker, stale-state sweep), `runner.go` (the agent's `jobProcess` implementation), `guestenv.go` (host env to guest env rewriting), `protocol.go` (newline-delimited JSON over vsock). The guest helper is `guest_linux.go`, exposed as the internal CLI command `buildkite-agent vm-guest-bootstrap`. The agent wiring is in `agent/job_runner.go` (runner selection) and `clicommand/agent_start.go` (flags and up-front validation).

## Setup

Everything below runs on an aarch64 Linux host with `/dev/kvm`. On an Apple Silicon Mac, that host is a Lima VM with nested virtualisation, which is what this prototype was developed and tested on. Other arrangements (bare-metal Graviton, an EC2 `.metal` instance) haven't been tried.

### Outer Linux VM on a Mac (Lima)

Requirements: Apple M3 or later, macOS with Virtualization.framework nested virtualisation, Lima 2.x (`brew install lima`). OrbStack doesn't expose nested virtualisation, so it can't host this.

```sh
limactl start --name=bko-dev --vm-type=vz --nested-virt \
  --cpus 6 --memory 12 --disk 60 \
  --mount-only "$PWD:w" --mount-type virtiofs \
  --tty=false template:ubuntu-24.04
```

Only the agent checkout is mounted into the VM. Don't mount your home directory: nothing from the Mac needs to reach the outer VM other than the source, and nothing from the outer VM reaches guests other than the built rootfs.

Provision the VM (once):

```sh
limactl shell bko-dev -- sudo apt-get update
limactl shell bko-dev -- sudo apt-get install -y docker.io e2fsprogs python3 curl
limactl shell bko-dev -- sudo usermod -aG docker,kvm "$USER"
# Go: install the version from go.mod / mise.toml under /usr/local/go.
```

Group membership only applies to new login sessions. `limactl shell` reuses an existing session, so either restart the VM or run agent commands under `sudo -iu "$USER"`, and check with `id -nG` that `kvm` and `docker` are present. Without `kvm` the agent refuses to start with `vm sandbox unavailable: ... /dev/kvm: permission denied`.

### Sandbox installation directory

Inside the outer VM, from the agent checkout:

```sh
sudo mkdir -p /opt/bko-sandbox && sudo chown "$USER" /opt/bko-sandbox
./internal/vmsandbox/guest/build-rootfs.sh /opt/bko-sandbox
sudo ./internal/vmsandbox/guest/setup-network.sh
```

`build-rootfs.sh` fetches the pinned Firecracker release (v1.17.0, sha256-checked) and the pinned Firecracker CI guest kernel (6.1.186, sha256 recorded in the script), builds the agent for linux/arm64, builds `Dockerfile` (Ubuntu 24.04 plus git, docker.io, docker-compose-v2, and the agent), and exports the image to a sparse 8 GiB ext4 at `images/rootfs.ext4`. It takes about 1.5 minutes and is safe to rerun; rerun it whenever `init.sh`, the `Dockerfile` or agent code that runs in the guest changes.

`setup-network.sh` creates the `bko-tap0` tap device owned by the agent user, addresses it 172.16.0.1/30, enables forwarding and adds NAT and FORWARD rules. It needs root; the agent never does. It isn't persistent, so rerun it after rebooting the outer VM.

Resulting layout:

```
/opt/bko-sandbox/
  bin/firecracker
  images/vmlinux -> vmlinux-6.1.186     (+ .config)
  images/rootfs.ext4                    base disk, copied per job
  state/<job-id>/                       per-job: rootfs.ext4, vm.json, fc.sock, vsock uds, pidfile, console.log
  state/POISONED                        present only if a teardown failed
```

## Running the agent

```sh
go build -o buildkite-agent .
./buildkite-agent start --token "$BUILDKITE_AGENT_TOKEN" --spawn 1 --vm-sandbox
```

Flags (all also settable as `BUILDKITE_VM_SANDBOX*` env vars or config-file keys):

| Flag | Default | Meaning |
| --- | --- | --- |
| `--vm-sandbox` | off | Enable the mode. Validated at startup; the agent exits rather than falling back to local execution. |
| `--vm-sandbox-dir` | `/opt/bko-sandbox` | Installation directory above. |
| `--vm-sandbox-vcpus` / `--vm-sandbox-memory-mib` | 2 / 2048 | Guest machine size. This bounds what the guest kernel sees; it isn't a host-side resource limit. |
| `--vm-sandbox-tap` | `bko-tap0:172.16.0.1/30:172.16.0.2` | Pre-created tap and addressing, matching `setup-network.sh`. |
| `--vm-sandbox-dns` | `1.1.1.1,8.8.8.8` | Written to the guest's resolv.conf. |
| `--vm-sandbox-boot-timeout` | 120s | Firecracker start to guest `ready`. Nested-virt boots take 40–60 s. |
| `--vm-sandbox-shutdown-timeout` | 10s | Cooperative power-off wait before SIGKILL. |

Constraints enforced at startup: `--spawn 1` (one job slot per process; the tap is single-tenant), no `--kubernetes-exec`, no `--bootstrap-script` (the guest runs its own bundled bootstrap). The agent's `--build-path`, `--hooks-path` and `--plugins-path` don't apply to the guest, which uses the baked-in layout under `/var/lib/buildkite-agent` and `/etc/buildkite-agent/hooks`.

Startup logs `VM sandbox mode enabled: each job's bootstrap will run in a fresh Firecracker microVM`. A job running in a guest has hostname `bko-sandbox`, kernel 6.1.186 and checkout paths under `/var/lib/buildkite-agent/builds`.

## Tests

Unit tests run anywhere: `go test ./internal/vmsandbox/ ./agent/ ./clicommand/`.

Integration tests need the prepared installation and real KVM, and are gated on an env var:

```sh
BUILDKITE_VMSANDBOX_INTEGRATION_DIR=/opt/bko-sandbox \
  go test -count=1 -v -timeout 30m -run 'Integration|FirecrackerConfig' ./internal/vmsandbox/
```

They need no Buildkite token: they drive `vmsandbox.Runner` directly with a bootstrap env for a public repository. What they cover, all observed passing in the Lima VM (full run about 4.5 minutes):

- `TestIntegration_BootstrapInGuest`: real boot and readiness; bootstrap checkout and command in the guest (asserts guest hostname, PID 1, kernel); non-zero exit status (3) reaching the runner; a 3-line secret with spaces, `$` and a tab transferred intact and absent from the job output; `docker run` with a bind mount of the checkout; bash process substitution and `/dev/stdout`; then a second job asserting fresh workspace, empty Docker state and a fresh job-context directory, and a fresh clone.
- `TestIntegration_CancelUncooperativeGuest`: the guest helper is frozen (SIGSTOP) mid-job, the context is cancelled, `Run` returns in about 5 s with a SIGKILL wait status, and the sandbox is torn down.
- `TestIntegration_AgentCrashRecovery`: a separate supervisor process is SIGKILLed mid-job. The guest notices the lost vsock connection and powers itself off (Firecracker gone within about 200 ms); the orphaned state dir remains until the next `vmsandbox.New` sweeps it, and the sandbox isn't poisoned.
- `TestFirecrackerConfigCarriesNoJobEnv`: the Firecracker config file carries network settings but no job environment.

Each test also asserts the job's state dir is gone and no `POISONED` marker exists.

## Execution boundary

What crosses into the guest, and only over the vsock connection after the host has verified the guest connected:

- The bootstrap environment the job runner computed (job env plus agent settings), after `GuestEnv` rewrites it. The agent process's own `os.Environ()` is never sent. `BUILDKITE_AGENT_TOKEN`, `BUILDKITE_BIN_PATH` and `BUILDKITE_AGENT_PID` are stripped; path settings are rewritten to the guest layout; host-only settings (extra hooks dirs, git mirrors, agent config file, JWKS key file) are cleared with a warning in the job log.
- The contents of the two job env files, recreated by the guest under `/var/lib/buildkite-agent/job-context`.
- Interrupt and power-off messages.

What the guest sends back: `ready`, job output bytes, log lines, exit status. There's no host command endpoint; the host acts only on those four message types.

What stays on the host: the agent registration token and session, the API client, the job's log upload and finish reporting, the tap device and NAT, the Firecracker process (owned and killed by the host), and the base image. The guest has no host filesystem: no virtiofs, no 9p, no bind mounts, only its per-job copy of the root disk. The bootstrap's local Job API socket lives inside the guest, so `buildkite-agent env`/`redactor add` etc from hooks work as normal, and containers started by the job that mount the Job API socket are mounting a guest path.

Credentials: the job runner refuses to start a sandboxed job if the accepted job carries no job-scoped token (`Job.Token == ""`), because the fallback in the local path is the agent's registration token. Buildkite's hosted backend supplies job tokens today; a backend that doesn't can't use this mode.

Cleanup ownership: the host runner owns Firecracker. Teardown sends `off`, waits `--vm-sandbox-shutdown-timeout`, SIGKILLs, confirms the process has exited, then removes `state/<job>`. If the kill or removal fails, the runner writes `state/POISONED` and every later `New` refuses to run until an operator removes it. If the agent itself dies, the guest sees the vsock connection drop, cancels bootstrap and powers off; the next agent start sweeps leftover `state/<job>` dirs, killing a pid from the pidfile only if `/proc/<pid>/cmdline` is our Firecracker pointing at that dir.

## Limitations and what still needs validating

Not tested here:

- Hosted job dispatch end to end (a real `agent start` accepting a job from buildkite.com) and artifact upload from inside the guest. Artifact upload uses the job-scoped token over HTTPS from the guest, which is the same path bootstrap uses on a normal host, but it hasn't been observed in this prototype. Run a real pipeline with `--vm-sandbox` to confirm.
- Docker Compose plugin workloads. The compose binary is in the image; only `docker run` with a bind mount was exercised.
- Any EKS or Kubernetes deployment. Running Firecracker inside a Pod needs `/dev/kvm` on the node (bare-metal or nested-virt-capable instances), a privileged or device-plugin arrangement, a per-Pod tap or a CNI that can hand a guest an address, and enough ephemeral storage for the per-job rootfs copies.

Known limitations of the implementation:

- Root everywhere in the guest; no Firecracker jailer, seccomp or cgroup confinement on the host side; Firecracker runs as the agent user. Guest vCPU/RAM settings bound the guest, not Firecracker's host footprint.
- Functional isolation only. Nothing here has been evaluated against malicious job content, and no security or performance claims are made.
- One job slot per agent process (`--spawn 1`), one hard-coded tap.
- Full copy of the 8 GiB sparse rootfs per job (`cp --sparse=always --reflink=auto`; about 600 MiB of real data). Copy-on-write or a shared read-only base plus overlay would cut this.
- Boot to ready is 40–60 s under nested virtualisation (kernel about 14 s, then dockerd). Expect much less on a native KVM host, but that's an assumption.
- Hooks are whatever is baked into the image at `/etc/buildkite-agent/hooks`; the agent's hooks directory isn't visible. Rebuild the rootfs to change them.
- Guest DNS and the tap/NAT arrangement are host-specific and not persistent across host reboots.
- `BUILDKITE_AGENT_JWKS_FILE` signing keys aren't available in the guest; signed-pipeline verification would need the key delivered another way.
- Console output goes to `state/<job>/console.log`, which is removed at teardown. On boot failure, the runner includes its tail in the error.

## Persistent local resources and removal

Created by the setup above, on the Mac:

- Lima VM `bko-dev` (`~/.lima/bko-dev`, 60 GiB sparse disk): `limactl delete -f bko-dev`.
- Lima's Ubuntu image cache (`~/Library/Caches/lima/download/`), shared with any other Lima VMs: remove with `rm -rf ~/Library/Caches/lima` if nothing else uses Lima.

Inside the Lima VM (all gone with the VM):

- `/opt/bko-sandbox/` (Firecracker binary, kernel, base rootfs, per-job state).
- Docker images `bko-sandbox-guest:latest` and `ubuntu:24.04`, and the `alpine:3.20` image pulled by the integration test's guests lives only on disposable guest disks.
- The `bko-tap0` device and iptables rules from `setup-network.sh` (non-persistent anyway).

Nothing is written outside the checkout on the Mac itself.
