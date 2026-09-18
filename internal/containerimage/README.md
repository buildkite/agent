# Container environment token tests

The six official entrypoints put plaintext `BUILDKITE_AGENT_TOKEN` values in a
pipe, wait for its writer to exit, and re-exec Bash before running startup
scripts. Tini remains PID 1. The agent drains and closes the dedicated handoff
at process startup, before command parsing, even when a CLI token overrides it.
The value stays in agent memory for token resolution; it is never restored to
the environment. Parent processes retain only the empty read end by job-ready
time, including when running `bootstrap` directly.

Plaintext environment tokens are limited to 4096 bytes so waiting for the writer
cannot deadlock on pipe capacity. This also applies to help and invalid commands.
Process substitution is intentional: older Bash versions can back here-strings
with recoverable temporary files.

Startup scripts receive an `fd://` reference instead of the plaintext token.
The reference is single-use: scripts must not drain it or persist it as a token
file. The dedicated consumption marker is withheld from `run-parts`, so startup
scripts can still invoke commands such as `buildkite-agent --version` without
consuming the final agent's handoff. Scripts that need their own registration
token must supply it separately.

Raw `--token` arguments remain visible in tini's command line. Operator-provided
`file://` and `fd://` references retain their existing behavior. Persistent token
files are not removed or protected by this change. These tests deliberately
check that an overriding raw CLI token is still present while the environment
token is absent and its handoff pipe is empty.

Run from the repository root against a built image:

```sh
BUILDKITE_TEST_CONTAINER_IMAGE=local-agent-image go test -count=1 -v ./internal/containerimage
```

For cached fixtures, `BUILDKITE_TEST_CONTAINER_BINARY` and
`BUILDKITE_TEST_CONTAINER_ENTRYPOINT` accept absolute paths to a Linux agent
binary and the real entrypoint script. `BUILDKITE_TEST_CONTAINER_ARCH` selects
the container architecture, defaulting to the host architecture.

The suite uses dummy tokens and a loopback fake API with `--network=none`.
It checks authenticated registration and polling, startup-process environments,
recoverable regular descriptors, the dedicated parent pipe, SSH setup, orphan
reaping, SIGTERM forwarding, CLI precedence, help/error exits, size limits, and
direct bootstrap execution. Inspection failures terminate the fixture immediately:
a failed read must not drain the pipe and allow a later poll to pass.

Image CI builds a test-only executable alongside the Linux agent artifacts and
runs it on each official image. The executable is not packaged into the images.
