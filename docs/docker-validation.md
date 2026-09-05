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
