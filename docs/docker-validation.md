# Docker bootstrap manual validation

Use [docker-validation.yml](docker-validation.yml) in the YAML editor of a
dedicated development pipeline. The steps use the `docker-bootstrap-dev` queue
and the default Ubuntu image's Python interpreter. No repository scripts or
hook changes are required.

## 1. Prepare the development agent

Run these tests with only the development agent consuming the test queue, and
no other Docker bootstrap builds running on its machine. Keep the agent terminal
open so you can see its cancellation messages.

For the latest diagnostics, install the newly built Linux ARM64 binary inside
the Orb machine while the development agent is stopped. Replace the example
machine name `docker-test-vm` below with your own. From macOS:

```sh
orb push -m docker-test-vm /tmp/buildkite-agent-docker-bootstrap-linux-arm64
orb -m docker-test-vm
```

In the Linux shell:

```sh
sudo install -m 755 ~/buildkite-agent-docker-bootstrap-linux-arm64 /usr/local/bin/buildkite-agent
```

Restart your existing agent command with these additional settings:

```text
--cancel-signal=SIGTERM
--cancel-signal-timeout=20s
```

Keep `--job-context-dir` and the `docker-bootstrap` bootstrap-script setting.
Keep the supervisor's default `--cleanup-margin=5s`. That gives the inner
bootstrap a 15-second grace period and reserves time for supervisor cleanup.
Do not change these values through pipeline environment variables.

## 2. Install the validation pipeline

Paste the entire contents of [docker-validation.yml](docker-validation.yml)
into the pipeline's YAML editor and save it. Change every queue value if your
development agent uses a different queue. Do not add a step-level `image`.

Disable any pipeline-level policy that automatically cancels an older build
when you create another build. Complete one test build before starting the next.

The `BOOTSTRAP_TEST` build variable selects the test. Use that exact name:
job-defined `DOCKER_*` variables are rejected by this prototype. The conditionals
are evaluated when the pipeline is uploaded, so set the variable when creating
the build, not within a command.

## 3. Validate ordinary exit codes

Create a new build and enter this under Environment Variables:

```text
BOOTSTRAP_TEST=exits
```

Wait for all six command jobs to finish. Inspect each job's reported exit status
in its job details or final command log; do not rely on build colour alone.

| Step | Expected exit status | Expected behavior |
| --- | --- | --- |
| Exit 0 | 0 | Pass |
| Exit 1 | 1 | Expected soft failure |
| Exit 42 | 42 | Expected soft failure |
| Explicit exit 137 | 137 | Expected soft failure; no signal was sent |
| Explicit exit 143 | 143 | Expected soft failure; no signal was sent |
| Exit 255 | 255 | Expected soft failure |

Each nonzero step only soft-fails its expected code. Still check the numeric
status: an incorrect zero would also appear successful. Explicit exits 137 and
143 must not be treated as evidence of an OOM or signal termination.

Code 125 is deliberately excluded: this agent reserves it for bootstrap setup
failure and maps it to reported status -1 with reason `process_run_error`. It is
not an ordinary exit-code round-trip test.

From a second macOS terminal, check that all test containers have been removed:

```sh
orb -m docker-test-vm docker ps -a \
  --filter label=com.buildkite.bootstrap=docker \
  --format 'table {{.ID}}\t{{.Status}}\t{{.Names}}'
```

Expected: only the table header. Use `-a` so stopped-but-not-removed containers
are visible too.

## 4. Validate graceful cancellation

Create a separate build with:

```text
BOOTSTRAP_TEST=graceful
```

1. Open the running job and wait for `READY: cancel this job now` in its log.
2. Run the Docker listing above and record the running container ID.
3. Use the running job's **Cancel** action in Buildkite. Leave the agent running.
4. Look for `GRACEFUL: received signal 15` and `GRACEFUL: cleanup complete`.
5. Wait for the job to finish, then repeat the Docker listing.

Pass criteria: the signal handler runs, its cleanup message reaches the job log,
the command exits 143, Buildkite records cancellation rather than a normal pass,
and the container is removed. The agent remains connected and can accept another
job. The supervisor should not need to print its forced-removal message.

The handler sleeps for one second, so this should finish well before the grace
deadline after the agent receives cancellation. Allow for polling between the UI
click and the agent receiving the request.

This exercises application cleanup in a signal handler. It does not yet validate
post-command/pre-exit hooks or artifact uploads during cancellation.

## 5. Validate cancellation of an uncooperative command

Create another build with:

```text
BOOTSTRAP_TEST=forced
```

1. Wait for `READY: ignoring SIGTERM and SIGINT; cancel this job now`.
2. Record the running container ID using the Docker listing.
3. Cancel the running job in Buildkite, leaving the agent running.
4. Observe the job and agent logs until cancellation completes.
5. Repeat the Docker listing, including `-a`.

The command ignores graceful cancellation. Escalation should occur near the
15-second inner grace deadline, measured from the agent receiving cancellation,
and cleanup should complete within its 20-second outer budget.

If the supervisor performs escalation, expect:

```text
Docker bootstrap cancellation grace expired; forcing container removal
```

That supervisor path returns 137. The inner bootstrap can also kill the command
at its own deadline and finish first, so absence of that exact log message does
not by itself mean escalation failed. Record the actual exit status and logs;
do not infer which process sent SIGKILL from exit status 137 alone.

Pass criteria: the job terminates despite ignoring SIGTERM, Buildkite records
cancellation, no container remains, and the agent stays usable. A remaining
running or stopped container, a supervisor killed before cleanup, or a cleanup
failure diagnostic is a failure to investigate.

## 6. Confirm recovery and record results

Run `BOOTSTRAP_TEST=exits` again after the cancellation tests. This checks that
the same agent can accept and complete subsequent jobs.

Record the build URL, test mode, reported exit statuses, cancellation log
messages, approximate duration, and whether the final Docker listing was empty.
If a container remains, preserve its ID and inspect its state before removing it:

```sh
orb -m docker-test-vm docker inspect --format '{{json .State}}' CONTAINER_ID
```

These tests validate exit reporting, command cancellation, and container cleanup.
Checkout failures, cancellation during pull/start, hooks, artifacts, lock
compatibility, and backend delivery of step images remain separate tests.

## Recorded validation

Manual validation on ARM64 Ubuntu Noble under OrbStack used a 20-second
cancellation timeout and the default 5-second supervisor cleanup margin.

| Test | Observed result |
| --- | --- |
| Graceful cancellation | Process exited 143 approximately two seconds after cancellation, with no process signal reported. |
| Forced cancellation | Process exited 137 approximately 16 seconds after cancellation. The job log explicitly confirmed supervisor-forced container removal. No stopped containers remained. |
| Recovery on the same agent | All six jobs returned exactly 0, 1, 42, 137, 143, and 255, with `Signal: nil`. The final filtered `docker ps -a` output was empty. |

These results establish command exit reporting, graceful termination timing,
supervisor escalation, container removal, and continued operation after
cancellation. They do not yet establish hook or artifact behavior during
cancellation.

References: [Buildkite conditionals](https://buildkite.com/docs/pipelines/configure/conditionals),
[soft failures](https://buildkite.com/docs/pipelines/configure/soft-fail), and
[canceling jobs](https://buildkite.com/docs/pipelines/configure/canceling-builds).

## Prototype image refresh and crash recovery

The prototype pulls an image only when it is missing locally. A cached mutable
tag is reused until the operator refreshes it. Set `--pull-policy=always` in the
bootstrap-script to refresh before each job, or `--pull-policy=never` to prohibit
pulls. A failed `always` pull fails the job even when the image is cached.
Between jobs, refresh the default image on the Docker host with:

```sh
docker pull buildkite/agent-base:ubuntu-noble-hosted
```

The next job resolves the refreshed tag to an immutable image ID. Existing jobs
continue using their already-selected image.

SIGKILL of the supervisor bypasses cleanup. Its separate Docker CLI process group
and running container may survive. Automatic reconciliation is deferred. Inspect
leftovers using `docker ps -a --filter label=com.buildkite.bootstrap=docker` and
the job/agent labels; confirm the job is no longer active before removing its
specific container with `docker rm --force CONTAINER_ID`.

## Pull policy, registry credentials, and container user

Configure operator flags in the bootstrap-script, for example:

```sh
buildkite-agent start \
  --build-path=/var/lib/buildkite-agent/builds \
  --plugins-path=/var/lib/buildkite-agent/plugins \
  --job-context-dir=/var/lib/buildkite-agent/docker-context \
  --bootstrap-script='/usr/bin/buildkite-agent docker-bootstrap --pull-policy=always --docker-config=/var/lib/buildkite-agent/registry'
```

Provision the dedicated registry directory with mode 0700 and use
`docker --config /var/lib/buildkite-agent/registry login REGISTRY --username USER --password-stdin`
to supply credentials through stdin. Keep `config.json` mode 0600, outside all
job-mounted paths. Container operations cannot access this configuration.

For credential helpers, install the helper on the host and configure `credHelpers`
or `credsStore` in that file. If needed, add `--docker-helper-path` with absolute
PATH entries and `--docker-helper-env-file` pointing to a mode 0600 JSON object:

```json
{"HOME":"/var/lib/buildkite-agent/registry-helper","AWS_PROFILE":"buildkite-pull"}
```

These values go only to pull helpers. Provision referenced credential files on
the host outside job mounts. Helpers must not include credentials in error text.

The default container identity now matches the host agent UID:GID. Use fresh
build/plugin directories when validating migration from root execution; the
supervisor does not repair existing file ownership. `--user=0:0` opts into root.
A different non-root UID requires a root host agent and pre-provisioned writable
build/plugin paths. Each container receives a private writable home and minimal
user/group lookup files; arbitrary image accounts and supplementary groups are
not preserved.

Linux-only integration tests are opt-in and require the local Docker socket:

```sh
CGO_ENABLED=0 go build -o /tmp/buildkite-agent-test .
DOCKER_BOOTSTRAP_TEST_BINARY=/tmp/buildkite-agent-test \
  go test ./internal/dockerbootstrap -run TestDockerUserIntegration -v
```

The hosted image must already be cached for the user test. It checks non-root
and explicit root execution, repository hook environment propagation, writable
home, user/group lookup, repeated workspace use, file ownership, and cleanup.
Run as an unprivileged agent user to exercise the non-root default.

Private-registry integration tests also accept `DOCKER_BOOTSTRAP_TEST_IMAGE` and
`DOCKER_BOOTSTRAP_TEST_AUTH` (a private Docker config directory). Use a disposable
registry requiring basic authentication and seed the hosted image under a unique
test tag. These tests remove that tag from the local image cache to exercise
missing-image behavior; do not point them at an image used by other workloads.
The helper test requires the fixture's inline basic-auth credentials.

```sh
DOCKER_BOOTSTRAP_TEST_BINARY=/tmp/buildkite-agent-test \
DOCKER_BOOTSTRAP_TEST_IMAGE=127.0.0.1:5000/bootstrap:validation \
DOCKER_BOOTSTRAP_TEST_AUTH=/private/test-registry-config \
  go test ./internal/dockerbootstrap -run 'TestDocker(Registry|CredentialHelper)Integration' -v
```

Live ARM64 OrbStack validation confirmed authenticated pulls, cached policies,
credential rejection, host credential helper execution, and cancellation during
helper resolution. Default non-root execution also passed the real pipeline's
checkout/artifact round trip and metahook ordering/failure tests. Build and plugin
files belonged to the host agent UID:GID. Graceful cancellation completed with
exit 143; forced cancellation logged supervisor-forced removal. No bootstrap
containers remained. The source and generated credential
configurations were not mounted into jobs. Rootless/user-namespace mappings and
migration of existing root-owned workspaces remain outside this validation.
A root-host test also verified a distinct non-root UID:GID with pre-provisioned
workspace ownership, including private-context access and cleanup. An unwritable
workspace failed with a user-permission diagnostic before bootstrap started.

## Per-job networks

Each execution creates `buildkite_job_<random-id>` and
`buildkite_network_<random-id>` with the same suffix. The job log includes the
network name. Inspect resources on the agent's Docker host:

```sh
docker ps -a --filter label=com.buildkite.bootstrap=docker
docker network ls --filter label=com.buildkite.bootstrap=docker
```

After all test jobs finish, both lists should be empty. While jobs are running,
each container should belong to exactly one dedicated bridge, with no published
ports.

With the hosted image cached, run the opt-in Linux integration test:

```sh
CGO_ENABLED=0 go build -o /tmp/buildkite-agent-test .
DOCKER_BOOTSTRAP_TEST_BINARY=/tmp/buildkite-agent-test \
  go test ./internal/dockerbootstrap -run 'TestDocker(Network|User)Integration' -v
```

The network test starts two concurrent jobs and requires outbound HTTPS access
to `https://example.com`. Both jobs start TCP listeners. The test confirms that
each listener is reachable locally, but connections to the other job's container
IP fail in both directions. It then cancels one job gracefully and forces the
other to stop, checking that both containers and networks disappear. The user
test also exercises successful exits and permission failures with network cleanup.

Live ARM64 OrbStack validation passed these checks. Unit tests cover partial
network creation, cancellation during creation, missing resources, cleanup
failures, and reserved time for network removal.

These checks do not establish isolation from host services, cloud metadata, or
internal networks. DNS/proxy overrides, custom CAs, VPN routes, IPv6, and MTU
compatibility remain unvalidated. Abrupt supervisor termination can leave networks
behind; stale-resource reconciliation remains deferred.
