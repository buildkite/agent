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

After connecting, `AgentWorker` runs two main goroutines: one periodically 
calls `Heartbeat`, the other more frequently calls `Ping`. `Ping` is how the 
worker discovers work from the API.

## Ping Interval

The agent polls for jobs using a ping interval specified by the Buildkite server
during agent registration (typically 10 seconds). To prevent thundering herd 
problems, each ping includes random jitter (0 to ping-interval seconds), meaning 
jobs may take 10-20 seconds to be picked up with default settings.

For performance-sensitive workloads (like dynamic pipelines), you can override 
the server-specified interval:

```bash
# Override to ping every 5 seconds (plus 0-5s jitter = 5-10s total)
# Only integer values are supported (e.g., 2, 5, 10), not decimals
buildkite-agent start --ping-interval 5

# Or via environment variable
export BUILDKITE_AGENT_PING_INTERVAL=5
buildkite-agent start
```

Setting `--ping-interval 0` or omitting it uses the server-provided interval.
Values below 2 seconds are automatically clamped to 2 seconds with a warning.
Float values like `2.5` are not supported and will cause an error.

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

