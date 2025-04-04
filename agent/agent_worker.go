package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/core"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/status"
	"github.com/buildkite/roko"
)

type AgentWorkerConfig struct {
	// Whether to set debug in the job
	Debug bool

	// Whether to set debugHTTP in the job
	DebugHTTP bool

	// What signal to use for worker cancellation
	CancelSignal process.Signal

	// Time wait between sending the CancelSignal and SIGKILL to the process
	// groups that the executor starts
	SignalGracePeriod time.Duration

	// The index of this agent worker
	SpawnIndex int

	// The configuration of the agent from the CLI
	AgentConfiguration AgentConfiguration

	// Stdout of the parent agent process. Used for job log stdout writing arg, for simpler containerized log collection.
	AgentStdout io.Writer
}

type agentStats struct {
	sync.Mutex

	// Tracks the last successful heartbeat and ping
	lastPing, lastHeartbeat time.Time

	// The last error that occurred during heartbeat, or nil if it was successful
	lastHeartbeatError error
}

type AgentWorker struct {
	stats agentStats

	// The API Client used when this agent is communicating with the API
	apiClient APIClient

	// The core Client is used to drive some APIClient methods
	client *core.Client

	// The logger instance to use
	logger logger.Logger

	// The configuration of the agent from the CLI
	agentConfiguration AgentConfiguration

	// The registered agent API record
	agent *api.AgentRegisterResponse

	// Metric collection for the agent
	metricsCollector *metrics.Collector

	// Metrics scope for the agent
	metrics *metrics.Scope

	// Whether to enable debug
	debug bool

	// Whether to enable debugging of HTTP requests
	debugHTTP bool

	// The signal to use for cancellation
	cancelSig process.Signal

	// Stop controls
	stopOnce sync.Once // prevents double-closing the channel
	stop     chan struct{}

	// The index of this agent worker
	spawnIndex int

	// When this worker runs a job, we'll store an instance of the
	// JobRunner here
	jobRunner jobRunner

	// Stdout of the parent agent process. Used for job log stdout writing arg, for simpler containerized log collection.
	agentStdout io.Writer

	// Are we doing something right now?
	stateMtx     sync.Mutex
	state        agentWorkerState
	currentJobID string

	// disable the delay between pings, to speed up certain testing scenarios
	noWaitBetweenPingsForTesting bool
}

type agentWorkerState string

const (
	agentWorkerStateIdle agentWorkerState = "idle"
	agentWorkerStateBusy agentWorkerState = "busy"
)

func (a *AgentWorker) setBusy(jobID string) {
	a.stateMtx.Lock()
	defer a.stateMtx.Unlock()
	a.state = agentWorkerStateBusy
	a.currentJobID = jobID
}

func (a *AgentWorker) setIdle() {
	a.stateMtx.Lock()
	defer a.stateMtx.Unlock()
	a.state = agentWorkerStateIdle
	a.currentJobID = ""
}

func (a *AgentWorker) getState() agentWorkerState {
	a.stateMtx.Lock()
	defer a.stateMtx.Unlock()
	return a.state
}

func (a *AgentWorker) getCurrentJobID() string {
	a.stateMtx.Lock()
	defer a.stateMtx.Unlock()
	return a.currentJobID
}

type errUnrecoverable struct {
	action   string
	response *api.Response
	err      error
}

func (e *errUnrecoverable) Error() string {
	status := "unknown"
	if e.response != nil {
		status = e.response.Status
	}

	return fmt.Sprintf("%s failed with unrecoverable status: %s, mesage: %q", e.action, status, e.err)
}

func (e *errUnrecoverable) Is(other error) bool {
	_, ok := other.(*errUnrecoverable)
	return ok
}

func (e *errUnrecoverable) Unwrap() error {
	return e.err
}

// Creates the agent worker and initializes its API Client
func NewAgentWorker(l logger.Logger, reg *api.AgentRegisterResponse, m *metrics.Collector, apiClient APIClient, c AgentWorkerConfig) *AgentWorker {
	apiClient = apiClient.FromAgentRegisterResponse(reg)
	return &AgentWorker{
		logger:           l,
		agent:            reg,
		metricsCollector: m,
		apiClient:        apiClient,
		client: &core.Client{
			APIClient: apiClient,
			Logger:    l,
		},
		debug:              c.Debug,
		debugHTTP:          c.DebugHTTP,
		agentConfiguration: c.AgentConfiguration,
		stop:               make(chan struct{}),
		cancelSig:          c.CancelSignal,
		spawnIndex:         c.SpawnIndex,
		agentStdout:        c.AgentStdout,
		state:              agentWorkerStateIdle,
	}
}

const workerStatusPart = `{{if le .LastPing.Seconds 2.0}}✅{{else}}❌{{end}} Last ping: {{.LastPing}} ago <br/>
{{if le .LastHeartbeat.Seconds 60.0}}✅{{else}}❌{{end}} Last heartbeat: {{.LastHeartbeat}} ago<br/>
{{if .LastHeartbeatError}}❌{{else}}✅{{end}} Last heartbeat error: {{printf "%v" .LastHeartbeatError}}`

func (a *AgentWorker) statusCallback(context.Context) (any, error) {
	a.stats.Lock()
	defer a.stats.Unlock()

	return struct {
		SpawnIndex         int
		LastHeartbeat      time.Duration
		LastHeartbeatError error
		LastPing           time.Duration
	}{
		SpawnIndex:         a.spawnIndex,
		LastHeartbeat:      time.Since(a.stats.lastHeartbeat),
		LastHeartbeatError: a.stats.lastHeartbeatError,
		LastPing:           time.Since(a.stats.lastPing),
	}, nil
}

// Starts the agent worker
func (a *AgentWorker) Start(ctx context.Context, idleMonitor *IdleMonitor) error {
	a.metrics = a.metricsCollector.Scope(metrics.Tags{
		"agent_name": a.agent.Name,
	})

	ctx, _ = status.AddItem(ctx, fmt.Sprintf("Worker %d", a.spawnIndex), workerStatusPart, a.statusCallback)

	// Start running our metrics collector
	if err := a.metricsCollector.Start(); err != nil {
		return err
	}
	defer a.metricsCollector.Stop()

	// Use a context to run heartbeats for as long as the ping loop or job runs
	heartbeatCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go a.runHeartbeatLoop(heartbeatCtx)

	// If the agent is booted in acquisition mode, acquire that particular job
	// before running the ping loop.
	// (Why run a ping loop at all? To find out if the agent is paused, which
	// affects whether it terminates after the job.)
	if a.agentConfiguration.AcquireJob != "" {
		// When in acquisition mode, there can't be any agents, so
		// there's really no point in letting the idle monitor know
		// we're busy, but it's probably a good thing to do for good
		// measure.
		idleMonitor.MarkBusy(a.agent.UUID)

		if err := a.AcquireAndRunJob(ctx, a.agentConfiguration.AcquireJob); err != nil {
			a.logger.Error("Failed to acquire and run job: %v", err)
		}
	}

	return a.runPingLoop(ctx, idleMonitor)
}

func (a *AgentWorker) runHeartbeatLoop(ctx context.Context) {
	ctx, setStat, _ := status.AddSimpleItem(ctx, "Heartbeat loop")
	defer setStat("💔 Heartbeat loop stopped!")
	setStat("🏃 Starting...")

	heartbeatInterval := time.Second * time.Duration(a.agent.HeartbeatInterval)
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()
	for {
		setStat("😴 Sleeping for a bit")
		select {
		case <-heartbeatTicker.C:
			setStat("❤️ Sending heartbeat")
			if err := a.Heartbeat(ctx); err != nil {
				if errors.Is(err, &errUnrecoverable{}) {
					a.logger.Error("%s", err)
					return
				}

				// Get the last heartbeat time to the nearest microsecond
				a.stats.Lock()
				if a.stats.lastHeartbeat.IsZero() {
					a.logger.Error("Failed to heartbeat %s. Will try again in %s. (No heartbeat yet)",
						err, heartbeatInterval)
				} else {
					a.logger.Error("Failed to heartbeat %s. Will try again in %s. (Last successful was %v ago)",
						err, heartbeatInterval, time.Since(a.stats.lastHeartbeat))
				}
				a.stats.Unlock()
			}

		case <-ctx.Done():
			a.logger.Debug("Stopping heartbeats")
			return
		}
	}
}

func (a *AgentWorker) runPingLoop(ctx context.Context, idleMonitor *IdleMonitor) error {
	ctx, setStat, _ := status.AddSimpleItem(ctx, "Ping loop")
	defer setStat("🛑 Ping loop stopped!")
	setStat("🏃 Starting...")

	// Create the ticker
	pingInterval := time.Second * time.Duration(a.agent.PingInterval)
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	// testTriggerCh will normally block forever, and so will not affect the for/select loop.
	var testTriggerCh chan struct{}
	if a.noWaitBetweenPingsForTesting {
		// a closed channel will unblock the for/select instantly, for zero-delay ping loop testing.
		testTriggerCh = make(chan struct{})
		close(testTriggerCh)
	}

	first := make(chan struct{}, 1)
	first <- struct{}{}

	lastActionTime := time.Now()
	a.logger.Info("Waiting for instructions...")

	ranJob := false
	wasPaused := false

	// Continue this loop until one of:
	// * the context is cancelled
	// * the stop channel is closed (a.Stop)
	// * the agent is in acquire mode and the ping action isn't "pause"
	// * the agent is in disconnect-after-job mode, the job is finished, and the
	//   ping action isn't "pause",
	// * the agent is in disconnect-after-idle-timeout mode, has been idle for
	//   longer than the idle timeout, and the ping action isn't "pause".
	for {
		setStat("😴 Waiting until next ping interval tick")
		select {
		case <-testTriggerCh:
			// instant receive from closed chan when noWaitBetweenPingsForTesting is true
		case <-first:
			// continue below
		case <-pingTicker.C:
			// continue below
		case <-a.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}

		// Within the interval, wait a random amount of time to avoid
		// spontaneous synchronisation across agents.
		jitter := rand.N(pingInterval)
		setStat(fmt.Sprintf("🫨 Jittering for %v", jitter))
		select {
		case <-testTriggerCh:
			// instant receive from closed chan when noWaitBetweenPingsForTesting is true
		case <-time.After(jitter):
			// continue below
		case <-a.stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}

		setStat("📡 Pinging Buildkite for instructions")
		job, action, err := a.Ping(ctx)
		if err != nil {
			if errors.Is(err, &errUnrecoverable{}) {
				a.logger.Error("%v", err)
			} else {
				a.logger.Warn("%v", err)
			}
		}

		switch action {
		case "disconnect":
			a.Stop(false)
			return nil

		case "pause":
			// An agent is not dispatched any jobs while it is paused, but the
			// paused agent is expected to remain alive and pinging for
			// instructions.
			// *This includes acquire-job and disconnect-after-idle-timeout.*
			wasPaused = true
			continue
		}

		// At this point, action was neither "disconnect" nor "pause".
		if wasPaused {
			a.logger.Info("Agent has resumed after being paused")
			wasPaused = false
		}

		// Exit after acquire-job.
		if a.agentConfiguration.AcquireJob != "" {
			return nil
		}

		// Exit after disconnect-after-job.
		if ranJob && a.agentConfiguration.DisconnectAfterJob {
			a.logger.Info("Job ran, and disconnect-after-job is enabled. Disconnecting...")
			return nil
		}

		// Handle disconnect after idle timeout (and deprecated disconnect-after-job-timeout)
		if a.agentConfiguration.DisconnectAfterIdleTimeout > 0 {
			idleDeadline := lastActionTime.Add(time.Second *
				time.Duration(a.agentConfiguration.DisconnectAfterIdleTimeout))

			if time.Now().After(idleDeadline) {
				// Let other agents know this agent is now idle and termination
				// is possible
				idleMonitor.MarkIdle(a.agent.UUID)

				// But only terminate if everyone else is also idle
				if idleMonitor.Idle() {
					a.logger.Info("All agents have been idle for %d seconds. Disconnecting...",
						a.agentConfiguration.DisconnectAfterIdleTimeout)
					return nil
				} else {
					a.logger.Debug("Agent has been idle for %.f seconds, but other agents haven't",
						time.Since(lastActionTime).Seconds())
				}
			}
		}

		// Note that Ping only returns a job if err == nil.
		if job == nil {
			continue
		}

		// Let other agents know this agent is now busy and
		// not to idle terminate
		idleMonitor.MarkBusy(a.agent.UUID)

		setStat("💼 Accepting job")

		// Runs the job, only errors if something goes wrong
		if err := a.AcceptAndRunJob(ctx, job); err != nil {
			a.logger.Error("%v", err)
			setStat(fmt.Sprintf("✅ Finished job with error: %v", err))
			continue
		}

		ranJob = true
		if a.agentConfiguration.DisconnectAfterJob {
			// Unless paused, this agent disconnects after the next ping.
			continue
		}
		lastActionTime = time.Now()

		// Observation: jobs are rarely the last within a pipeline,
		// thus if this worker just completed a job,
		// there is likely another immediately available.
		// Skip waiting for the ping interval until
		// a ping without a job has occurred,
		// but in exchange, ensure the next ping must wait a full
		// pingInterval to avoid too much server load.

		pingTicker.Reset(pingInterval)
	}
}

// Stops the agent from accepting new work and cancels any current work it's
// running
func (a *AgentWorker) Stop(graceful bool) {
	if graceful {
		select {
		case <-a.stop:
			a.logger.Warn("Agent is already gracefully stopping...")

		default:
			// If we have a job, tell the user that we'll wait for
			// it to finish before disconnecting
			if a.jobRunner != nil {
				a.logger.Info("Gracefully stopping agent. Waiting for current job to finish before disconnecting...")
			} else {
				a.logger.Info("Gracefully stopping agent. Since there is no job running, the agent will disconnect immediately")
			}
		}
	} else {
		// If there's a job running, kill it, then disconnect
		if a.jobRunner != nil {
			a.logger.Info("Forcefully stopping agent. The current job will be canceled before disconnecting...")

			// Kill the current job. Doesn't do anything if the job
			// is already being killed, so it's safe to call
			// multiple times.
			err := a.jobRunner.CancelAndStop()
			if err != nil {
				a.logger.Error("Unexpected error canceling job (err: %s)", err)
			}
		} else {
			a.logger.Info("Forcefully stopping agent. Since there is no job running, the agent will disconnect immediately")
		}
	}

	a.stopOnce.Do(func() {
		// Use the closure of the stop channel as a signal to the main run loop in Start()
		// to stop looping and terminate
		close(a.stop)
	})
}

// Connects the agent to the Buildkite Agent API, retrying up to 10 times if it
// fails.
func (a *AgentWorker) Connect(ctx context.Context) error {
	return a.client.Connect(ctx)
}

// Performs a heatbeat
func (a *AgentWorker) Heartbeat(ctx context.Context) error {

	// Retry the heartbeat a few times
	r := roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	)

	beat, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.Heartbeat, error) {
		b, resp, err := a.apiClient.Heartbeat(ctx)
		if err != nil {
			if resp != nil && !api.IsRetryableStatus(resp) {
				a.Stop(false)
				r.Break()
				return nil, &errUnrecoverable{action: "Heartbeat", response: resp, err: err}
			}

			a.logger.Warn("%s (%s)", err, r)
			return nil, err
		}
		return b, nil
	})

	a.stats.Lock()
	defer a.stats.Unlock()

	a.stats.lastHeartbeatError = err

	if err != nil {
		return err
	}

	// Track a timestamp for the successful heartbeat for better errors
	a.stats.lastHeartbeat = time.Now()

	a.logger.Debug("Heartbeat sent at %s and received at %s", beat.SentAt, beat.ReceivedAt)
	return nil
}

// Performs a ping that checks Buildkite for a job or action to take
// Returns a job, or nil if none is found
func (a *AgentWorker) Ping(ctx context.Context) (job *api.Job, action string, err error) {
	ping, resp, pingErr := a.apiClient.Ping(ctx)
	// wait a minute, where's my if err != nil block? TL;DR look for pingErr ~20 lines down
	// the api client returns an error if the response code isn't a 2xx, but there's still information in resp and ping
	// that we need to check out to do special handling for specific error codes or messages in the response body
	// once we've done that, we can do the error handling for pingErr

	if ping != nil {
		// Is there a message that should be shown in the logs?
		if ping.Message != "" {
			a.logger.Info(ping.Message)
		}

		action = ping.Action
	}

	if pingErr != nil {
		// If the ping has a non-retryable status, we have to kill the agent, there's no way of recovering
		// The reason we do this after the disconnect check is because the backend can (and does) send disconnect actions in
		// responses with non-retryable statuses
		if resp != nil && !api.IsRetryableStatus(resp) {
			a.Stop(false)
			return nil, action, &errUnrecoverable{action: "Ping", response: resp, err: pingErr}
		}

		// Get the last ping time to the nearest microsecond
		a.stats.Lock()
		defer a.stats.Unlock()

		// If a ping fails, we don't really care, because it'll
		// ping again after the interval.
		if a.stats.lastPing.IsZero() {
			return nil, action, fmt.Errorf("Failed to ping: %w (No successful ping yet)", pingErr)
		} else {
			return nil, action, fmt.Errorf("Failed to ping: %w (Last successful was %v ago)", pingErr, time.Since(a.stats.lastPing))
		}
	}

	// Track a timestamp for the successful ping for better errors
	a.stats.Lock()
	a.stats.lastPing = time.Now()
	a.stats.Unlock()

	// check if the ping wants a new API client configuration
	if newAPIClient := a.apiClient.FromPing(ping); newAPIClient != a.apiClient {
		if newAPIClient.Config().Endpoint != a.apiClient.Config().Endpoint {
			// The endpoint has changed; proceed with caution in case customer only had the
			// original endpoint allow-listed.
			// Before switching to a new endpoint, do a ping test to make sure it's
			// valid. If it is, switch and carry on, otherwise ignore the switch
			newPing, _, err := newAPIClient.Ping(ctx)
			if err != nil {
				a.logger.Warn("Failed to ping the new endpoint %s - ignoring switch for now (%s)", ping.Endpoint, err)
				// note this means also ignoring other config change e.g. RequestHeaders
			} else {
				// Replace the APIClient and process the new ping instead of the old one.
				a.apiClient = newAPIClient
				a.agent.Endpoint = ping.Endpoint
				ping = newPing
				action = newPing.Action
			}
		} else {
			// Something other than the endpoint has changed, e.g. RequestHeaders.
			// This lower-risk, and doesn't need to be re-pinged.
			a.apiClient = newAPIClient
		}
	}

	// If we don't have a job, there's nothing to do!
	// If we're paused, job should be nil, but in case it isn't, ignore it.
	if ping.Job == nil || action == "pause" {
		return nil, action, nil
	}

	return ping.Job, action, nil
}

// AcquireAndRunJob attempts to acquire a job an run it. It will retry at after the
// server determined interval (from the Retry-After response header) if the job is in the waiting
// state. If the job is in an unassignable state, it will return an error immediately.
// Otherwise, it will retry every 3s for 30 s. The whole operation will timeout after 5 min.
func (a *AgentWorker) AcquireAndRunJob(ctx context.Context, jobId string) error {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-a.stop
		cancel()
	}()

	job, err := a.client.AcquireJob(ctx, jobId)
	if err != nil {
		return fmt.Errorf("failed to acquire job: %w", err)
	}

	// Now that we've acquired the job, let's run it
	return a.RunJob(ctx, job)
}

// Accepts a job and runs it, only returns an error if something goes wrong
func (a *AgentWorker) AcceptAndRunJob(ctx context.Context, job *api.Job) error {
	a.logger.Info("Assigned job %s. Accepting...", job.ID)

	// Accept the job. We'll retry on connection related issues, but if
	// Buildkite returns a 422 or 500 for example, we'll just bail out,
	// re-ping, and try the whole process again.
	r := roko.NewRetrier(
		roko.WithMaxAttempts(30),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	)

	accepted, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.Job, error) {
		accepted, _, err := a.apiClient.AcceptJob(ctx, job)
		if err != nil {
			if api.IsRetryableError(err) {
				a.logger.Warn("%s (%s)", err, r)
			} else {
				a.logger.Warn("Buildkite rejected the call to accept the job (%s)", err)
				r.Break()
			}
		}
		return accepted, err
	})

	// If `accepted` is nil, then the job was never accepted
	if accepted == nil {
		return fmt.Errorf("Failed to accept job: %w", err)
	}

	// Now that we've accepted the job, let's run it
	return a.RunJob(ctx, accepted)
}

func (a *AgentWorker) RunJob(ctx context.Context, acceptResponse *api.Job) error {
	a.setBusy(acceptResponse.ID)
	defer a.setIdle()

	jobMetricsScope := a.metrics.With(metrics.Tags{
		"pipeline": acceptResponse.Env["BUILDKITE_PIPELINE_SLUG"],
		"org":      acceptResponse.Env["BUILDKITE_ORGANIZATION_SLUG"],
		"branch":   acceptResponse.Env["BUILDKITE_BRANCH"],
		"source":   acceptResponse.Env["BUILDKITE_SOURCE"],
		"queue":    acceptResponse.Env["BUILDKITE_AGENT_META_DATA_QUEUE"],
	})

	// Now that we've got a job to do, we can start it.
	jr, err := NewJobRunner(ctx, a.logger, a.apiClient, JobRunnerConfig{
		Job:                acceptResponse,
		JWKS:               a.agentConfiguration.VerificationJWKS,
		Debug:              a.debug,
		DebugHTTP:          a.debugHTTP,
		CancelSignal:       a.cancelSig,
		MetricsScope:       jobMetricsScope,
		JobStatusInterval:  time.Duration(a.agent.JobStatusInterval) * time.Second,
		AgentConfiguration: a.agentConfiguration,
		AgentStdout:        a.agentStdout,
		KubernetesExec:     a.agentConfiguration.KubernetesExec,
	})
	if err != nil {
		return fmt.Errorf("Failed to initialize job: %w", err)
	}
	a.jobRunner = jr
	defer func() {
		// No more job, no more runner.
		a.jobRunner = nil
	}()

	// Start running the job
	if err := jr.Run(ctx); err != nil {
		return fmt.Errorf("Failed to run job: %w", err)
	}

	return nil
}

// Disconnect notifies the Buildkite API that this agent worker/session is
// permanently disconnecting. Don't spend long retrying, because we want to
// disconnect as fast as possible.
func (a *AgentWorker) Disconnect(ctx context.Context) error {
	return a.client.Disconnect(ctx)
}

func (a *AgentWorker) healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.stats.Lock()
		defer a.stats.Unlock()

		if a.stats.lastHeartbeatError != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "ERROR: last heartbeat failed: %v. last successful was %v ago", a.stats.lastHeartbeatError, time.Since(a.stats.lastHeartbeat))
		} else {
			if a.stats.lastHeartbeat.IsZero() {
				fmt.Fprintf(w, "OK: no heartbeat yet")
			} else {
				fmt.Fprintf(w, "OK: last heartbeat successful %v ago", time.Since(a.stats.lastHeartbeat))
			}
		}
	}
}
