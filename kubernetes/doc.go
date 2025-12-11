// Package kubernetes provides coordination between containers in a Kubernetes
// pod when running Buildkite jobs.
//
// # Architecture
//
// In Kubernetes, a job runs across multiple containers within a single pod:
//
//   - Agent container: runs `buildkite-agent start --kubernetes-exec`, receives
//     jobs from Buildkite, and acts as a coordinator.
//   - Checkout container: clones the repository.
//   - Command container(s): execute the build commands.
//
// These containers need coordination because:
//
//  1. Environment sharing: The agent container receives job details (API tokens,
//     build metadata, plugin configs) from Buildkite. Other containers need this
//     information but Kubernetes doesn't share environment variables between
//     containers.
//
//  2. Sequential execution: The checkout container must complete before command
//     containers start. Kubernetes starts all containers simultaneously by default.
//
//  3. Log aggregation: All container output must be collected and streamed to
//     Buildkite as a single job log.
//
//  4. Cancellation: When a job is cancelled in Buildkite, all containers must
//     be notified to gracefully shut down.
//
//  5. Exit status: The agent must collect exit statuses from all containers to
//     report the final job result.
//
// # Components
//
// [Runner] implements the server side, running in the agent container. It
// creates a Unix socket and exposes an RPC API for other containers to connect.
//
// [Client] implements the client side, used by `kubernetes-bootstrap` running
// in checkout and command containers. It connects to the socket, receives
// environment variables, and reports logs and exit status.
//
// # Flow
//
// 1. Agent container starts [Runner], which listens on a Unix socket.
//
// 2. Each container runs `kubernetes-bootstrap`, which creates a [Client] and
// calls [Client.Connect] to register with the runner and receive environment
// variables.
//
// 3. The client calls [Client.StatusLoop], which blocks until the runner
// signals it can start (ensuring sequential execution).
//
// 4. The container executes `buildkite-agent bootstrap`, streaming logs through
// the socket via [Client.Write].
//
// 5. On completion, the container calls [Client.Exit] to report its exit status.
//
// 6. If the job is cancelled, the runner broadcasts an interrupt to all
// connected clients via the status polling mechanism.
package kubernetes
