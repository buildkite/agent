# Control-plane OTLP exporter configuration (agent side)

Companion to buildkite/buildkite#31880. Supersedes the implementation approach in
agent PRs #4149, #4150, and #4155. Reviewed against the codebase by Oracle
2026-08-03; this revision incorporates that review.

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
environment variables on the bootstrap process, with a restore step so
colliding pipeline values survive.

1. **Advertise the capability.** Add `control-plane-otlp-tracing` to the
   features sent at registration (unless feature reporting is disabled).

2. **Parse the response.** Add `Tracing` to `AgentRegisterResponse` with the
   types above. No validation beyond "endpoint non-empty"; trust the control
   plane the same way we trust it for everything else in registration.

3. **Merge with local config, local wins.** Applied per registration, to that
   worker's `AgentConfiguration` copy (`--spawn N` performs N independent
   registrations; merge inside the registration callback, never mutate the
   shared config). The effective-backend rules:

   - Local non-OTel backend (e.g. `--tracing-backend datadog`): preserved.
     No server exporter, no server propagation. Server tracing config is
     ignored entirely.
   - Local `--tracing-backend opentelemetry` with no local destination: local
     backend stands; the server exporter may fill in the missing destination.
   - No local backend: apply the server's backend and exporter.
   - Any local OTLP destination in the host environment
     (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, or
     either `_HEADERS` variant, including explicitly-empty values): the
     server's `exporter` is ignored — the operator baked in their own config
     and we must not clobber it. Protocol/cert/timeout-only local vars do not
     count as a destination.
   - Server `propagate_traceparent` applies only when the effective backend is
     OTel and the local value was not explicitly set. An explicit local
     `false` — via CLI flag, config file key, or
     `BUILDKITE_TRACING_PROPAGATE_TRACEPARENT` — wins. Capture explicitness
     right after `setupLoggerAndConfig` and before `UnsetConfigFromEnvironment`
     removes the env vars, following the existing `no-plugins`
     `c.IsSet` + `configFile.Config` pattern in `clicommand/agent_start.go`.
     (Losing this was a real bug in the previous attempt.)
   - Log one `Infof` line **per registration/worker** stating what was applied
     or ignored, with the exporter endpoint reduced to scheme+host (paths and
     queries can carry ingest keys; headers never logged; if the endpoint URL
     fails to parse, log nothing of it rather than falling back to the raw
     string). `Infof` is correct for this repo's unusual level ordering
     (NOTICE < INFO).

4. **Deliver via standard OTel env vars on the bootstrap process.** When a
   server exporter applies, the job runner — *after* env files are written, so
   nothing here reaches `job.env` or `BUILDKITE_ENV_FILE` — sets on the
   bootstrap process environment:

   ```
   OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
   OTEL_EXPORTER_OTLP_TRACES_PROTOCOL
   OTEL_EXPORTER_OTLP_TRACES_HEADERS      # see header encoding below
   BUILDKITE_CONTROL_PLANE_OTLP=true      # marker: "these were injected by us"
   BUILDKITE_CONTROL_PLANE_OTLP_RESTORE   # snapshot of displaced values, see below
   ```

   Traces-specific (not generic) variables, so the credential cannot be picked
   up by other signals (e.g. a logs exporter pointed at a pipeline-chosen
   endpoint) via the generic-variable fallback.

   **Header encoding:** values are `url.PathEscape`'d (the OTel SDK
   `url.PathUnescape`s them); keys are not escaped (the SDK validates them as
   HTTP tokens without unescaping). Map keys are sorted before serializing so
   output is deterministic.

   **Restore snapshot:** before overwriting, the job runner records the
   original state (present-with-value / present-empty / absent) of the three
   `OTEL_EXPORTER_OTLP_TRACES_*` names from the effective bootstrap
   environment into `BUILDKITE_CONTROL_PLANE_OTLP_RESTORE` (JSON). This is how
   a pipeline that sets its own trace exporter vars keeps them for its own
   tools (step 6).

   **Marker authenticity:** a pipeline could set the marker or restore
   variable in `Job.Env` to trick bootstrap into stripping an operator's
   baked-in vars. Defenses: (a) the job runner deletes both names from the
   cloned job environment before env files are written, so a spoof never
   reaches `BUILDKITE_ENV_FILE` or the bootstrap env; (b) the real marker is
   set only when the server exporter is actually injected; (c) both names are
   added to `protectedEnv` so hooks, plugins, and the Job API cannot set them;
   (d) cleanup requires the marker to parse as exactly `true`, not mere
   presence.

5. **Bootstrap consumes them via the existing exporter-selection code.** The
   OTel SDK constructors consume the traces-specific endpoint and headers
   natively. Protocol is *not* SDK-side: `InitOTelTracerProvider` in
   `internal/job/tracing.go` chooses which exporter package (gRPC vs HTTP) to
   instantiate. The one required change: check
   `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` before the generic
   `OTEL_EXPORTER_OTLP_PROTOCOL`, per OTel signal-specific precedence rules.

6. **One cleanup point, marker-gated.** In `Executor.Run`, immediately after
   optional OTLP job-log setup and *before* `startJobAPI` (the Job API serves
   `e.shell.Env`, so cleanup must precede it). At that point the trace
   provider and root span already exist (constructed in CLI setup), the log
   exporter is constructed, and no hook has run. When (and only when) the
   authentic marker is present, remove the three injected variables, the
   marker, and the restore variable from both `e.shell.Env` and the process
   env, then re-apply the restore snapshot (restoring present/empty/absent
   states exactly). All hooks (env, plugin, checkout, command) and the job
   command are built from `e.shell.Env` via `Shell.buildCommand`, so this one
   point covers everything. Without the marker, nothing is removed —
   operator-baked OTEL vars are untouched. Env access goes through the
   `env.Environment` abstraction, which is case-insensitive on Windows.

No broad pipeline-env scrubbing. To be precise about why that's safe: pipeline
`Job.Env` *is* present in the bootstrap environment when tracing initializes
(it's copied in before exec; only env-hook changes come too late). But the
injected traces-specific endpoint/headers supersede generic variables in the
OTel SDK, and the traces-specific headers *replace* (never merge with) generic
ones — so a pipeline cannot redirect the credentialed export or append to its
headers. What a pipeline *can* still influence is non-owned transport settings
(proxy, CA, timeout, compression) via standard env vars, which is exactly the
accepted baked-in-image baseline. And thanks to the restore snapshot, pipeline
trace-exporter vars still reach the job's own tools unchanged.

## Security model (vs. the baked-in-image baseline)

| Exposure | Baked-in image | This design |
|---|---|---|
| Credential resident in long-running agent (env vs. config memory) | yes, process env | yes, registration/worker config in memory |
| Credential in bootstrap process env | yes, whole job | yes, until tracing init, then cleared |
| Credential visible to hooks and job commands | **yes** | **no** (stripped via marker) |
| Credential visible via Job API | yes | no (cleanup precedes `startJobAPI`) |
| Same-UID process reads env via /proc | yes | yes (accepted) |
| `--debug-http` dumps credential | n/a | yes — accepted; it already dumps the agent access token |
| Credential rotation without agent restart | no | no (accepted; registration-scoped) |
| Pipeline env influences transport (proxy/CA/timeout) at exporter construction | yes | yes (accepted; standard SDK behavior, same as baseline) |
| Pipeline env redirects the credentialed export destination or headers | yes (can override baked generic vars) | **no** (traces-specific vars supersede and replace) |
| Custom bootstrap script sees credential | yes | yes (operator-controlled, accepted) |

Net: strictly better than the baseline on hook/command/Job API exposure and on
destination integrity, equal everywhere else.

## Accepted issues (do not "fix" these in this PR)

- **Same-UID inspection.** Another process running as the agent's user can read
  bootstrap's environment while it exists. Equivalent to existing exposure of
  `BUILDKITE_AGENT_ACCESS_TOKEN`.
- **No live refresh/revocation.** Rotating the credential or disabling the
  service takes effect on agent re-registration (restart). Same as an image
  rebuild, but cheaper.
- **`--debug --debug-http` logs the headers.** It already logs the registration
  token and access token. General HTTP-dump redaction is a separate concern.
- **No transport pinning.** Pipeline job env can influence proxy/CA/timeout of
  the exporter at construction time via standard env vars; we do not add
  custom proxy/CA handling. Standard Go and OTel SDK behavior applies, same as
  the baseline.
- **Custom bootstraps.** A `--bootstrap-script` operator owns their env
  handling; we don't gate the feature on it.
- **Protocol/cert/timeout-only local OTEL vars don't block the server
  exporter.** Only a local endpoint or headers count as "operator configured
  their own destination".
- **`--job-logs-otlp` composition.** Server config is traces-scoped, so job
  logs without an explicit logs endpoint fall back to SDK defaults. The log
  exporter is constructed before cleanup and reads logs-specific then generic
  settings, so existing `--job-logs-otlp` users are not regressed; richer
  composition is follow-up, not this PR.

## Non-goals

- Per-job exporter configuration or credentials.
- File-descriptor or any non-environment secret channel to bootstrap.
- Debug-output redaction infrastructure.
- Broad scrubbing of pipeline-provided `OTEL_*` from job env (only the two
  reserved `BUILDKITE_CONTROL_PLANE_OTLP*` names are scrubbed, as spoofing
  defense).
- Kubernetes-stack-specific handling. The generated env is transported
  wholesale through the runner and merged into the bootstrap container
  (`clicommand/kubernetes_bootstrap.go`); the OTel vars are not among the
  container-priority exceptions, so env delivery works there as-is.

## Implementation sketch

- `api/agents.go` + new `api/tracing.go`: response types.
- `clicommand/agent_start.go`: capability advertisement; capture explicit
  `propagate_traceparent` provenance (CLI/config-file/env) before
  `UnsetConfigFromEnvironment`; merge server policy into the per-worker config
  copy inside the registration callback.
- `agent/agent_configuration.go`: hold the merged exporter.
- `agent/job_runner.go`: scrub the two reserved names from the cloned job env
  before env files are written; after env files, snapshot displaced values and
  inject the five env vars into the bootstrap process env.
- `env/protected.go`: protect the marker and restore variable.
- `internal/job/tracing.go`: traces-specific protocol precedence.
- `internal/job/executor.go`: marker-gated cleanup + restore at the single
  point after job-log OTLP setup, before `startJobAPI`.
- Tests:
  - precedence matrix: local datadog / local otel backend without destination /
    local endpoint / local headers / explicitly-empty local vars /
    explicit-false propagation via each of CLI, config file, env.
  - header encoding: values with commas, spaces, `+`, `/`, `=`; keys
    unescaped; deterministic key order.
  - restore: pipeline-set sentinel values for the three names reach the env
    hook and command unchanged while the exporter used server values.
  - spoofing: marker/restore var in `Job.Env` never reaches env files or
    bootstrap; operator-baked vars with no authentic marker are untouched.
  - Job API GET does not expose injected values; post-init hook/Job API writes
    to `OTEL_*` don't change the exporter.
  - `--job-logs-otlp` with generic and logs-specific config unaffected.
  - per-worker merge under `--spawn`.
  - sanitized endpoint logging: path, query, userinfo, IPv6 host, malformed
    URL.
  - Windows case-insensitive removal.

## Definition of done

- An agent with zero local tracing config, registered against an org with the
  feature + unrestricted OTel service, emits job traces to the configured
  endpoint over http/protobuf with the configured headers.
- A `printenv` job command and an environment hook show none of the injected
  variables — and *do* show the pipeline's own `OTEL_EXPORTER_OTLP_TRACES_*`
  values if the pipeline set them.
- Local backend selection is unchanged. A locally configured OTLP endpoint or
  headers means exporter behavior and job environment are exactly as today. A
  local OTel backend without a local destination may consume the server
  exporter.
- One log line per registered worker tells the operator which config won and
  where traces go (scheme+host only).
