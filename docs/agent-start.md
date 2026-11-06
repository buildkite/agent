# Agent start

`buildkite-agent` does a lot of things, and so it ends up running a lot of 
different goroutines. This is an attempt to document what is going on at the goroutine level.

The diagram below depicts the (incomplete) call graph that runs when running
`buildkite-agent start`. Packages are shown as surrounding rectangles.
Solid arrows denote regular function calls. Dotted arrows (labelled "go") are
goroutine calls. Some anonymous funcs are given names for the purpose of the
diagram (in angle brackets).

![Agent call graph](images/agent-start.svg)

`buildkite-agent start` effectively begins at the `Action` in `AgentStartCommand`. 
Already there are four different goroutines running: one for handling signals,
one that waits to run the shutdown hook, one for the inbuilt HTTP server, and
finally the main goroutine continues to start the `AgentPool`.

`AgentPool` manages `AgentWorker`s - the number of workers is given by the
concurrency config option or flag. `AgentPool` also spins up one other 
goroutine which waits for all the workers to finish, then closes a channel.
The effect is that `AgentPool` returns either `nil` once all workers have 
stopped without error, or the first non-nil error.

Once all workers have registered, the once-per-process `agent-startup` hook runs
before the `AgentPool` starts. The `agent-startup` and `agent-shutdown` hooks
receive `BUILDKITE_AGENT_IDS` and `BUILDKITE_AGENT_NAMES` as comma-separated
lists in spawn order, allowing hook scripts to identify the registered spawned
agents without querying the API.

After connecting, `AgentWorker` runs two main goroutines: one periodically 
calls `Heartbeat`, the other more frequently calls `Ping`. `Ping` is how the 
worker discovers work from the API.

Once a job has been accepted, the `AgentWorker` fires up a `JobRunner` to run
it. Each `JobRunner` starts several goroutines that handle various tasks:

* Processing chunks of log output from the job
* Streaming chunks of logs up to the API
* Streaming header times up to the API
* Periodically checking with the API if the job is cancelled
* Waiting for the job to be over to mark the job as completed

The core of the job runner is running a `Process`, which itself spins up a few
helper goroutines:

* Copying PTY output
* Waiting on context cancellation in order to hard-terminate the process

## Experimental local REAPI cache

`buildkite-agent start --reapi-cache-addr 127.0.0.1:0` starts a cache-only
REAPI gRPC server. The matching config file option is
`reapi-cache-addr="127.0.0.1:0"`, or set `BUILDKITE_AGENT_REAPI_CACHE_ADDR`.
It is disabled by default. Port `0` allocates an available port; a fixed port
can also be used. Only numeric loopback addresses are accepted.

One listener is shared by all spawned workers. Each job receives:

* `BUILDKITE_REAPI_CACHE_ENDPOINT`: the actual `http://host:port` endpoint (h2c).
* `BUILDKITE_REAPI_CACHE_TOKEN`: a job-specific credential. Clients must send
  `authorization: Bearer <token>` in gRPC metadata. Do not print this token.

Credentials are registered when the job runs, revoked when its runner exits,
and are not written to the job environment files. In-flight backend requests are
canceled when the job session ends. Requests use that job's Agent API credentials, so
the registry's save/restore policy still controls access. An `instance_name`
selects a namespace within that policy; it cannot select another job's identity.
This does not isolate mutually untrusted processes running as the same OS user.

Configure `BUILDKITE_AGENT_CACHE_STORE_URL` and, optionally,
`BUILDKITE_AGENT_CACHE_REGISTRY` in the agent process environment. The default
registry is `~`. The server uses the existing S3, NSC, or local file store adapter;
`file://` is for local testing only. Clients in containers must share the agent's
network namespace and explicitly receive the endpoint and token. Kubernetes exec
mode is not supported. Hooks that change cache variables after the job starts
do not reconfigure the server.

Supported services are Capabilities, CAS FindMissingBlobs/BatchReadBlobs/
BatchUpdateBlobs, ByteStream Read/Write, and ActionCache Get/Update. Execution
is disabled and its service is not registered. Only SHA-256 and uncompressed
REAPI transfers are supported. Blobs are limited to 64 MiB, batches to 3 MiB;
GetTree and resumable uploads are not implemented. The implementation buffers
individual objects and is not yet tuned for large builds.

CAS objects and action results are stored as versioned single-file ZIP envelopes
with mandatory exact cache keys, separate from filesystem cache entries. Reads
verify the envelope digest, and CAS reads also verify the requested raw digest.
An action whose referenced output blobs are missing is a cache miss. Opaque
ActionResult metadata is preserved, including Once's native result mapping.

### Tuist Once 0.77.0 compatibility

Once currently uses its Tuist provider for REAPI. A compatibility configuration
in `once.toml` is:

```toml
[infrastructure.cache]
provider = "tuist"
project = "my-project"

[infrastructures.tuist]
kind = "tuist"
account = "buildkite"
url = "http://127.0.0.1:9"
```

The local `url` prevents Once's separate insights discovery from using the local
credential with Tuist's public services. Set `TUIST_CACHE_ENDPOINT` from
`BUILDKITE_REAPI_CACHE_ENDPOINT` and `TUIST_TOKEN` from
`BUILDKITE_REAPI_CACHE_TOKEN` in the job, without printing either token.
This is a compatibility workaround, not a native Buildkite provider in Once.
No Once source changes are needed for this version.

`ONCE_TEST_BINARY=/absolute/path/to/once go test -v ./internal/cache/reapi`
runs a real C build through Once, removes its local cache and output, and checks
remote reuse, output bytes, executable mode, and replayed streams. That test uses
a metadata API fixture and the real file store, **not live Buildkite Cache**.
Live validation additionally requires job credentials with cache permissions and
a configured backing store.
