# Control-plane OTLP exporter configuration (agent side)

Companion to buildkite/buildkite#31880. Supersedes the implementation approach in
agent PRs #4149, #4150, and #4155.

## Goal

Let the Buildkite control plane supply an organization's OpenTelemetry exporter
configuration (endpoint, protocol, auth headers) to the agent at registration
time, so hosted/managed agent images do not need vendor OTLP credentials baked
into them.

## Design baseline: "no worse than a baked-in image"

Today an operator can bake this into an agent image or systemd unit:

```bash
BUILDKITE_TRACING_BACKEND=opentelemetry
OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer <token>"
```

In that world the credential lives in the agent process environment for the
life of the process, is inherited by bootstrap, and is inherited by every hook
and job command. That is our security baseline. This design must be **no worse
than that**, and is allowed to be only modestly better. Anything requiring
stronger isolation (file-descriptor passing, custom transports, debug-output
redaction subsystems) is explicitly out of scope — that machinery is where the
previous PR drowned.

## Wire contract (fixed by the server; already merged)

Registration request advertises a capability:

```json
{ "features": ["control-plane-otlp-tracing"] }
```

Registration response may include:

```json
{
  "tracing": {
    "backend": "opentelemetry",
    "propagate_traceparent": true,
    "exporter": {
      "endpoint": "https://collector.example/v1/traces",
      "protocol": "http/protobuf",
      "headers": { "Authorization": "Bearer …" }
    }
  }
}
```

Notes on the contract:

- The `tracing` key is omitted entirely unless the org has the feature and an
  enabled, unrestricted OpenTelemetry notification service. Absent means "do
  nothing new".
- `endpoint` is a full traces URL (includes `/v1/traces`); `protocol` is
  `http/protobuf`. The agent must honor the protocol, not default to gRPC.
- Auth is generic headers; there is no separate token field.
- Per-job payloads carry only `traceparent`/`tracestate`, as they already do.
  No per-job credentials.

## Agent design

The whole feature is: registration response → agent config → standard OTel
environment variables on the bootstrap process.

1. **Advertise the capability.** Add `control-plane-otlp-tracing` to the
   features sent at registration (unless feature reporting is disabled).

2. **Parse the response.** Add `Tracing` to `AgentRegisterResponse` with the
   types above. No validation beyond "endpoint non-empty"; trust the control
   plane the same way we trust it for everything else in registration.

3. **Merge with local config, local wins.** Applied once, to the worker's
   `AgentConfiguration`:
   - If the operator configured a tracing backend locally
     (`--tracing-backend`), the server's `backend` is ignored.
   - If the host environment already has an OTLP destination
     (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, or
     either `_HEADERS` variant), the server's `exporter` is ignored — the
     operator has baked in their own config and we must not clobber it.
   - An explicit local `propagate_traceparent=false` wins over the server's
     `true`. (Capture explicitness before config-env teardown; this was a real
     bug in the previous attempt.)
   - Log one `Infof` line stating what was applied or ignored, with the
     exporter endpoint reduced to scheme+host (paths/queries can carry ingest
     keys; headers never logged). `Infof` is correct for this repo's unusual
     level ordering (NOTICE < INFO).

4. **Deliver via standard OTel env vars on the bootstrap process.** When a
   server exporter applies, the job runner sets, on the bootstrap process
   environment only (never `job.env`, never `BUILDKITE_ENV_FILE`):

   ```
   OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
   OTEL_EXPORTER_OTLP_TRACES_PROTOCOL
   OTEL_EXPORTER_OTLP_TRACES_HEADERS   # values url.PathEscape'd per OTel spec
   BUILDKITE_CONTROL_PLANE_OTLP=true   # marker: "these were injected by us"
   ```

   Traces-specific (not generic) variables, so the credential cannot be picked
   up by other signals (e.g. a logs exporter pointed at a pipeline-chosen
   endpoint) via the generic-variable fallback.

5. **Bootstrap consumes them via the existing SDK path.** The OTel SDK already
   reads these variables. The only code change in tracing init is precedence:
   check `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` before the generic
   `OTEL_EXPORTER_OTLP_PROTOCOL`, per OTel signal-specific precedence rules.

6. **Strip injected vars before hooks and commands.** After tracing init,
   when (and only when) the marker is present, bootstrap removes the three
   injected variables and the marker from the environment passed to hooks and
   the job command, and from its own process env once tracing (and optional
   OTLP job-log setup) is initialized. Removal is keyed on the marker so that
   operator-baked OTEL vars are untouched — without the marker we remove
   nothing. Case-insensitive on Windows.

That's it. No pipeline-env scrubbing in v1: bootstrap initializes tracing from
its own process env before any hook runs, so pipeline-provided `OTEL_*` job env
cannot redirect the credentialed export; it only affects the job's own tools,
exactly as today.

## Security model (vs. the baked-in-image baseline)

| Exposure | Baked-in image | This design |
|---|---|---|
| Credential in agent process env for process lifetime | yes | yes (agent memory/config) |
| Credential in bootstrap process env | yes, whole job | yes, until tracing init, then cleared |
| Credential visible to hooks and job commands | **yes** | **no** (stripped via marker) |
| Same-UID process reads env via /proc | yes | yes (accepted) |
| `--debug-http` dumps credential | n/a | yes — accepted; it already dumps the agent access token |
| Credential rotation without agent restart | no | no (accepted; registration-scoped) |
| Pipeline redirects credentialed export via proxy/CA env | no meaningful protection | same (accepted; hooks run after tracing init, so job env can't do this anyway) |
| Custom bootstrap script sees credential | yes | yes (operator-controlled, accepted) |

Net: strictly better than the baseline on hook/command exposure, equal
everywhere else.

## Accepted issues (do not "fix" these in this PR)

- **Same-UID inspection.** Another process running as the agent's user can read
  bootstrap's environment while it exists. Equivalent to existing exposure of
  `BUILDKITE_AGENT_ACCESS_TOKEN`.
- **No live refresh/revocation.** Rotating the credential or disabling the
  service takes effect on agent re-registration (restart). Same as an image
  rebuild, but cheaper.
- **`--debug --debug-http` logs the headers.** It already logs the registration
  token and access token. General HTTP-dump redaction is a separate concern.
- **No transport pinning.** We do not add custom proxy/CA handling; standard Go
  and OTel SDK behavior applies.
- **Custom bootstraps.** A `--bootstrap-script` operator owns their env
  handling; we don't gate the feature on it.
- **Protocol/cert/timeout-only local OTEL vars don't block the server
  exporter.** Only a local endpoint or headers count as "operator configured
  their own destination".
- **`--job-logs-otlp` composition.** Server config is traces-scoped, so job
  logs without an explicit logs endpoint fall back to SDK defaults. Follow-up,
  not this PR.

## Non-goals

- Per-job exporter configuration or credentials.
- File-descriptor or any non-environment secret channel to bootstrap.
- Debug-output redaction infrastructure.
- Scrubbing pipeline-provided `OTEL_*` from job env.
- Kubernetes-stack-specific handling (env delivery is transport-agnostic and
  cross-platform, unlike the FD design).

## Implementation sketch

- `api/agents.go` + new `api/tracing.go`: response types.
- `clicommand/agent_start.go`: capability advertisement; capture explicit
  `propagate_traceparent`; merge server policy into per-worker config copy.
- `agent/agent_configuration.go`: hold the merged exporter.
- `agent/job_runner.go`: inject the four env vars into the bootstrap process
  env (late, after env-file writing).
- `internal/job/executor.go` / `internal/job/tracing.go`: traces-specific
  protocol precedence; marker-gated strip before hooks/commands and process-env
  cleanup after tracing init.
- Tests: precedence matrix (local backend / local endpoint / local headers /
  explicit-false propagation), header PathEscape encoding, marker-gated strip
  vs. operator-baked vars untouched, Windows case-insensitivity, sanitized
  logging.

## Definition of done

- An agent with zero local tracing config, registered against an org with the
  feature + unrestricted OTel service, emits job traces to the configured
  endpoint over http/protobuf with the configured headers.
- A `printenv` job command and an environment hook show none of the injected
  variables.
- An agent with `--tracing-backend` or baked-in `OTEL_EXPORTER_OTLP_*` behaves
  exactly as it does today, byte for byte in its job env.
- One log line tells the operator which config won and where traces go
  (scheme+host only).
