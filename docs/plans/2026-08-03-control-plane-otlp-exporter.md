# Control-plane OTLP exporter configuration (agent side)

Companion to buildkite/buildkite#31880. Supersedes the implementation approach in
agent PRs #4149, #4150, and #4155. Reviewed against the codebase by Oracle
2026-08-03; revised 2026-08-04 after product review: exporter configuration is
now **passed through to hooks and the job command** rather than scrubbed after
tracer setup. Scrubbing broke the primary use case (see Goal).

## Goal

Let the Buildkite control plane supply an organization's OpenTelemetry exporter
configuration (endpoint, protocol, auth headers) to the agent at registration
time, so hosted/managed agent images do not need vendor OTLP credentials baked
into them.

This is not just for the agent's own spans: bootstrap propagates the trace
context to hooks and the job command (`TRACEPARENT`, via span-context
injection), and the point of distributing the exporter configuration is that
job-level ("userspace") OTel tooling can attach its spans to the same trace
**and send them to the same collector**. The exporter variables must therefore
reach the job environment — pass-through is the feature, not a leak.

## Design baseline: "no worse than a baked-in image"

Today an operator can bake this into an agent image or systemd unit:

```bash
BUILDKITE_TRACING_BACKEND=opentelemetry
OTEL_EXPORTER_OTLP_ENDPOINT=https://collector.example
OTEL_EXPORTER_OTLP_HEADERS="Authorization=Bearer <token>"
```

In that world the credential lives in the agent process environment for the
life of the process, is inherited by bootstrap, and is inherited by every hook
and job command — which is precisely how userspace tracing works today. That
is our security baseline and our functional baseline. This design must be **no
worse than that**. Anything requiring stronger isolation (file-descriptor
passing, custom transports, debug-output redaction subsystems) is explicitly
out of scope — that machinery is where the previous PR drowned.

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
environment variables on the bootstrap process, inherited by hooks and the job
command.

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
   - A server policy for any backend other than `opentelemetry` is ignored in
     full (with a warning), even when the local backend is already OTel: its
     exporter and propagation settings were meant for that other backend.
   - Local `--tracing-backend opentelemetry` with no local destination: local
     backend stands; the server exporter may fill in the missing destination.
   - No local backend: apply the server's backend and exporter.
   - Any local OTLP destination in the host environment
     (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`, or
     either `_HEADERS` variant, including explicitly-empty values): the
     server's `exporter` is ignored — the operator baked in their own config
     and we must not clobber it. Protocol/cert/timeout-only local vars do not
     count as a destination. The destination-variable list is shared with the
     per-job collision check (step 4) so the two semantics cannot drift.
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
   nothing here reaches `BUILDKITE_ENV_FILE`/`BUILDKITE_ENV_JSON_FILE` — sets
   on the bootstrap process environment:

   ```
   OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
   OTEL_EXPORTER_OTLP_TRACES_PROTOCOL
   OTEL_EXPORTER_OTLP_TRACES_HEADERS      # see header encoding below
   ```

   Traces-specific (not generic) variables, so the credential cannot be picked
   up by other signals (e.g. a logs exporter pointed at a pipeline-chosen
   endpoint) via the generic-variable fallback.

   **Pass-through.** Bootstrap does not remove these variables: hooks,
   plugins, and the job command inherit them, so userspace OTel SDKs export to
   the org's collector with the distributed credential. This matches the
   baked-in-image baseline, where operator env reaches the job the same way.

   **Per-job collision, all-or-nothing.** If the backend job env
   (pipeline/step) already contains *any* OTLP destination variable — generic
   or traces-specific endpoint or headers, presence counting even when empty
   (the same four-variable rule as the local-destination check in step 3) —
   the exporter is not injected for that job at all, and a log line says so.
   Endpoint and headers must travel together: a partial merge could send the
   server credential to a pipeline-chosen endpoint, or a pipeline credential
   to the server's endpoint. Protocol-only presence does not block (the
   injected traces-specific protocol supersedes it). The check runs against
   the pristine backend job env, normalized through `env.Environment` first so
   case-variant names cannot dodge it on Windows.

   **Header encoding:** values are `url.PathEscape`'d (the OTel SDK
   `url.PathUnescape`s them); keys are not escaped (the SDK validates them as
   HTTP tokens without unescaping). Map keys are sorted before serializing so
   output is deterministic.

5. **Bootstrap consumes them via the existing exporter-selection code.** The
   OTel SDK constructors consume the traces-specific endpoint and headers
   natively. Protocol is *not* SDK-side: `InitOTelTracerProvider` in
   `internal/job/tracing.go` chooses which exporter package (gRPC vs HTTP) to
   instantiate. The one required change: check
   `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` before the generic
   `OTEL_EXPORTER_OTLP_PROTOCOL`, per OTel signal-specific precedence rules.

No reserved marker variables, no restore snapshots, no cleanup step: an
earlier revision scrubbed the injected variables after tracer setup, which
required all three of those mechanisms and broke userspace span export. All of
it is gone.

One policy consequence worth stating: when a pipeline supplies its own OTLP
destination, only exporter injection is skipped — the server-applied backend
and traceparent propagation still stand. In that case the agent's own spans
export to wherever the effective (pipeline-influenced) environment points,
which is standard SDK behavior and involves no server credential.

## Security model (vs. the baked-in-image baseline)

| Exposure | Baked-in image | This design |
|---|---|---|
| Credential resident in long-running agent (env vs. config memory) | yes, process env | yes, registration/worker config in memory |
| Credential in bootstrap process env | yes, whole job | yes, whole job |
| Credential visible to hooks and job commands | yes | yes (intentional: userspace span export) |
| Credential visible via Job API | yes | yes (equivalent: those processes already hold it via env) |
| Credential printed by `env`/`printenv` in a step (build logs) | yes | yes (accepted) |
| Credential in `BUILDKITE_ENV_FILE`/`BUILDKITE_ENV_JSON_FILE` on disk | no (baked env is not job env) | no (injected after env files are written) |
| Same-UID process reads env via /proc | yes | yes (accepted) |
| `--debug-http` dumps credential | n/a | yes — accepted; it already dumps the agent access token |
| Credential rotation without agent restart | no | no (accepted; registration-scoped) |
| Pipeline env influences transport (proxy/CA/timeout/insecure) at exporter construction | yes | yes (accepted; standard SDK behavior, same as baseline) |
| Pipeline destination vars mixed with server credential | possible (pipeline can override baked generic vars per signal) | **no** (all-or-nothing skip: pipeline destination means no injection) |
| Custom bootstrap script sees credential | yes | yes (operator-controlled, accepted) |

Net: equal to the baseline everywhere, slightly better on env-file hygiene and
credential/destination mixing.

## Accepted issues (do not "fix" these in this PR)

- **Hooks, commands, and the Job API see the credential.** By design — they
  need it to export spans. A malicious pipeline can read and exfiltrate it,
  exactly as it can a baked-in credential. The org accepted this by enabling
  the feature.
- **`env` in a step prints the credential to the build log.** Same as
  baked-in today. Operators who care should use the existing `--redacted-vars`
  mechanism (`OTEL_EXPORTER_OTLP_TRACES_HEADERS` can be added to it).
- **Same-UID inspection.** Another process running as the agent's user can
  read bootstrap's environment. Equivalent to existing exposure of
  `BUILDKITE_AGENT_ACCESS_TOKEN`.
- **No live refresh/revocation.** Rotating the credential or disabling the
  service takes effect on agent re-registration (restart). Same as an image
  rebuild, but cheaper.
- **`--debug --debug-http` logs the headers.** It already logs the
  registration token and access token. General HTTP-dump redaction is a
  separate concern.
- **No transport pinning.** Job env can influence proxy/CA/timeout/insecure
  settings of exporters at construction time via standard env vars; we do not
  add custom handling. Standard Go and OTel SDK behavior applies, same as the
  baseline — and a process able to set those vars already holds the header.
- **Container-creating plugins don't auto-forward the variables.** The Docker
  and Docker Compose plugins' `propagate-environment` uses `BUILDKITE_ENV_FILE`
  as a name allowlist, and the injected names are deliberately not in that
  file. Workloads inside such containers need explicit `environment`
  propagation in the plugin config. Documented limitation for v1; transparent
  propagation is a cross-repo follow-up, not a reason to write credentials to
  disk.
- **Custom bootstraps.** A `--bootstrap-script` operator owns their env
  handling; we don't gate the feature on it.
- **Protocol/cert/timeout-only vars don't block the exporter.** Locally or
  per-job, only an endpoint or headers variable counts as "somebody configured
  their own destination".
- **`--job-logs-otlp` composition.** Server config is traces-scoped, so job
  logs without an explicit logs endpoint fall back to SDK defaults; the
  traces-specific variables are invisible to the logs exporter. Existing
  `--job-logs-otlp` users are not regressed; richer composition is follow-up.

## Non-goals

- Per-job exporter configuration or credentials.
- File-descriptor or any non-environment secret channel to bootstrap.
- Debug-output redaction infrastructure.
- Scrubbing or protecting `OTEL_*` variables in the job environment.
- Transparent propagation into plugin-created containers (Docker/Compose
  `propagate-environment`); see Accepted issues.
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
- `agent/control_plane_tracing.go`: merge logic, header encoding, sanitized
  endpoint logging.
- `agent/job_runner.go`: normalize the cloned job env (Windows case safety),
  detect a pipeline OTLP destination against the pristine job env, and after
  env files are written inject the three traces variables (or skip, logged).
- `env/control_plane_otlp.go`: the three traces variable names and the shared
  four-name destination list.
- `internal/job/tracing.go`: traces-specific protocol precedence.
- Tests:
  - precedence matrix: local datadog / local otel backend without destination /
    local endpoint / local headers / explicitly-empty local vars / unsupported
    server backend with and without local OTel backend / explicit-false
    propagation via each of CLI, config file, env.
  - header encoding: values with commas, spaces, `+`, `/`, `=`; keys
    unescaped; deterministic key order.
  - `createEnvironment` boundary: injected values reach the bootstrap env and
    stay out of both env files; each of the four destination variables
    (present and present-empty) blocks all injection while pipeline values
    survive; protocol-only and logs/metrics-signal variables don't block;
    case-variant collision on Windows.
  - `--job-logs-otlp` with generic and logs-specific config unaffected.
  - per-worker merge under `--spawn`.
  - sanitized endpoint logging: path, query, userinfo, IPv6 host, malformed
    URL.

## Definition of done

- An agent with zero local tracing config, registered against an org with the
  feature + unrestricted OTel service, emits job traces to the configured
  endpoint over http/protobuf with the configured headers.
- A `printenv` job command and an environment hook show the three injected
  `OTEL_EXPORTER_OTLP_TRACES_*` variables, so userspace tools can join the
  trace via `TRACEPARENT` and export to the same collector.
- A pipeline that sets its own OTLP destination sees exactly its own values —
  no injection, no mixing.
- Local backend selection is unchanged. A locally configured OTLP endpoint or
  headers means exporter behavior and job environment are exactly as today. A
  local OTel backend without a local destination may consume the server
  exporter.
- One log line per registered worker tells the operator which config won and
  where traces go (scheme+host only).
