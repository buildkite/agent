package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/core"
	"github.com/buildkite/agent/v3/internal/ptr"
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
	apiClient *api.Client

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

	// Stop controls. Note that Stopping != Cancelling. See the [Stop] method
	// for an explanation.
	stopOnce sync.Once // prevents double-closing the channel
	stop     chan struct{}

	// The index of this agent worker
	spawnIndex int

	// When this worker runs a job, we'll store an instance of the
	// JobRunner here
	jobRunner atomic.Pointer[JobRunner]

	// Stdout of the parent agent process. Used for job log stdout writing arg, for simpler containerized log collection.
	agentStdout io.Writer

	// Are we doing something right now?
	stateMtx     sync.Mutex
	state        agentWorkerState
	currentJobID string

	// The time when this agent worker started
	startTime time.Time

	// disable the delay between pings, to speed up certain testing scenarios
	noWaitBetweenPingsForTesting bool

	// stopErr stores an error that caused the agent to stop, so it can be
	// returned from the main loop even if the stop was triggered asynchronously
	// (e.g., from the heartbeat goroutine).
	stopErr atomic.Pointer[error]
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
func NewAgentWorker(l logger.Logger, reg *api.AgentRegisterResponse, m *metrics.Collector, apiClient *api.Client, c AgentWorkerConfig) *AgentWorker {
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
func (a *AgentWorker) Start(ctx context.Context, idleMon *idleMonitor) (startErr error) {
	// Record the start time for max agent lifetime tracking
	a.startTime = time.Now()

	a.metrics = a.metricsCollector.Scope(metrics.Tags{
		"agent_name": a.agent.Name,
	})

	ctx, _ = status.AddItem(ctx, fmt.Sprintf("Worker %d", a.spawnIndex), workerStatusPart, a.statusCallback)

	// Start running our metrics collector
	if err := a.metricsCollector.Start(); err != nil {
		return err
	}
	defer a.metricsCollector.Stop() //nolint:errcheck // Best-effort cleanup

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
		idleMon.markBusy(a)

		if err := a.AcquireAndRunJob(ctx, a.agentConfiguration.AcquireJob); err != nil {
			// If the job acquisition was rejected, we can exit with an error
			// so that supervisor knows that the job was not acquired due to the job being rejected.
			if errors.Is(err, core.ErrJobAcquisitionRejected) {
				return fmt.Errorf("Failed to acquire job %q: %w", a.agentConfiguration.AcquireJob, err)
			}

			// If the job itself exited with nonzero code, then we want to exit
			// with that code ourselves later on, but need to check if we were
			// paused in the meantime first.
			if exit := new(core.ProcessExit); errors.As(err, exit) && exit.Status != 0 {
				defer func() {
					startErr = errors.Join(err, startErr)
				}()
			}

			a.logger.Error("Failed to acquire and run job: %v", err)
		}
	}

	return a.runPingLoop(ctx, idleMon)
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

// runPingLoop runs the loop that pings Buildkite for work. It does all critical
// agent things.
// The lifetime of an agent is the lifetime of the ping loop.
func (a *AgentWorker) runPingLoop(ctx context.Context, idleMon *idleMonitor) error {
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

	// On the first iteration, skip waiting for the pingTicker.
	// This doesn't skip the jitter, though.
	skipTicker := make(chan struct{}, 1)
	skipTicker <- struct{}{}

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
	// * the agent has exceeded its disconnect-after-uptime and the ping action isn't "pause".
	for {
		startWait := time.Now()
		setStat("😴 Waiting until next ping interval tick")
		select {
		case <-testTriggerCh:
			// instant receive from closed chan when noWaitBetweenPingsForTesting is true
		case <-skipTicker:
			// continue below
		case <-pingTicker.C:
			// continue below
		case <-a.stop:
			return a.getStopErr()
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
			return a.getStopErr()
		case <-ctx.Done():
			return ctx.Err()
		}
		pingWaitDurations.Observe(time.Since(startWait).Seconds())

		setStat("📡 Pinging Buildkite for instructions")
		pingsSent.Inc()
		startPing := time.Now()
		job, action, err := a.Ping(ctx)
		if err != nil {
			pingErrors.Inc()
			if errors.Is(err, &errUnrecoverable{}) {
				a.logger.Error("%v", err)
				pingDurations.Observe(time.Since(startPing).Seconds())
				return err
			}
			a.logger.Warn("%v", err)
		}
		pingDurations.Observe(time.Since(startPing).Seconds())

		pingActions.WithLabelValues(action).Inc()
		switch action {
		case "disconnect":
			a.StopUngracefully()
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
		// For acquire-job agents, registration sets ignore-in-dispatches=true,
		// so job should be nil. If not nil, complain.
		if a.agentConfiguration.AcquireJob != "" {
			if job != nil {
				a.logger.Error("Agent ping dispatched a job (id %q) but agent is in acquire-job mode!", job.ID)
			}
			return nil
		}

		// Exit after disconnect-after-job. Finishing the job sets
		// ignore-in-dispatches=true, so job should be nil. If not, complain.
		if ranJob && a.agentConfiguration.DisconnectAfterJob {
			if job != nil {
				a.logger.Error("Agent ping dispatched a job (id %q) but agent is in disconnect-after-job mode (and already ran a job)!", job.ID)
			}
			a.logger.Info("Job ran, and disconnect-after-job is enabled. Disconnecting...")
			return nil
		}

		// Exit after disconnect-after-uptime is exceeded.
		if maxUptime := a.agentConfiguration.DisconnectAfterUptime; maxUptime > 0 {
			if time.Since(a.startTime) >= maxUptime {
				if job != nil {
					a.logger.Error("Agent ping dispatched a job (id %q) but agent has exceeded max uptime of %v!", job.ID, maxUptime)
				}
				a.logger.Info("Agent has exceeded max uptime of %v. Disconnecting...", maxUptime)
				return nil
			}
		}

		// Note that Ping only returns a job if err == nil.
		if job == nil {
			idleTimeout := a.agentConfiguration.DisconnectAfterIdleTimeout
			if idleTimeout == 0 {
				// No job and no idle timeout.
				continue
			}

			// This ensures agents that never receive a job are still tracked
			// by the idle monitor and can properly trigger disconnect-after-idle-timeout.
			idleMon.markIdle(a)

			// Exit if every agent has been idle for at least the timeout.
			if idleMon.shouldExit(idleTimeout) {
				a.logger.Info("All agents have been idle for at least %v. Disconnecting...", idleTimeout)
				return nil
			}

			// Not idle enough to exit. Wait and ping again.
			continue
		}

		setStat("💼 Accepting job")

		// Runs the job, only errors if something goes wrong
		if err := a.AcceptAndRunJob(ctx, job, idleMon); err != nil {
			a.logger.Error("%v", err)
			setStat(fmt.Sprintf("✅ Finished job with error: %v", err))
			continue
		}

		ranJob = true

		// Observation: jobs are rarely the last within a pipeline,
		// thus if this worker just completed a job,
		// there is likely another immediately available.
		// Skip waiting for the ping interval until
		// a ping without a job has occurred,
		// but in exchange, ensure the next ping must wait at least a full
		// pingInterval to avoid too much server load.
		pingTicker.Reset(pingInterval)
		select {
		case skipTicker <- struct{}{}:
			// Ticker will be skipped
		default:
			// We're already skipping the ticker, don't block.
		}
	}
}

// StopGracefully stops the agent from accepting new work. It allows the current
// job to finish without interruption. Does not block.
func (a *AgentWorker) StopGracefully() {
	select {
	case <-a.stop:
		a.logger.Warn("Agent is already gracefully stopping...")
		return

	default:
		// continue below
	}

	// If we have a job, tell the user that we'll wait for it to finish
	// before disconnecting
	if a.jobRunner.Load() != nil {
		a.logger.Info("Gracefully stopping agent. Waiting for current job to finish before disconnecting...")
	} else {
		a.logger.Info("Gracefully stopping agent. Since there is no job running, the agent will disconnect immediately")
	}

	a.stopOnce.Do(func() {
		// Use the closure of the stop channel as a signal to the main run
		// loop in Start() to stop looping and terminate
		close(a.stop)
	})
}

// getStopErr returns the error that caused the agent to stop, or nil if
// stopped gracefully.
func (a *AgentWorker) getStopErr() error {
	if errPtr := a.stopErr.Load(); errPtr != nil {
		return *errPtr
	}
	return nil
}

// StopUngracefully stops the agent from accepting new work and cancels any
// existing job. It blocks until the job is cancelled, if there is one.
func (a *AgentWorker) StopUngracefully() {
	a.stopUngracefullyWithError(nil)
}

// stopUngracefullyWithError stops the agent ungracefully and stores an error
// that caused the stop. This error will be returned from the main loop.
func (a *AgentWorker) stopUngracefullyWithError(err error) {
	if err != nil {
		a.stopErr.CompareAndSwap(nil, &err)
	}

	a.stopOnce.Do(func() {
		// Use the closure of the stop channel as a signal to the main run
		// loop in Start() to stop looping and terminate
		close(a.stop)
	})

	// If there's a job running, kill it, then disconnect.
	if jr := a.jobRunner.Load(); jr != nil {
		a.logger.Info("Forcefully stopping agent. The current job will be canceled before disconnecting...")

		// Kill the current job. Doesn't do anything if the job
		// is already being killed, so it's safe to call
		// multiple times.
		if err := jr.Cancel(CancelReasonAgentStopping); err != nil {
			a.logger.Error("Unexpected error canceling job (err: %s)", err)
		}
	} else {
		a.logger.Info("Forcefully stopping agent. Since there is no job running, the agent will disconnect immediately")
	}
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
				unrecoverableErr := &errUnrecoverable{action: "Heartbeat", response: resp, err: err}
				a.stopUngracefullyWithError(unrecoverableErr)
				r.Break()
				return nil, unrecoverableErr
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
			unrecoverableErr := &errUnrecoverable{action: "Ping", response: resp, err: pingErr}
			a.stopUngracefullyWithError(unrecoverableErr)
			return nil, action, unrecoverableErr
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

	// Should we switch endpoints?
	if ping.Endpoint != "" && ping.Endpoint != a.agent.Endpoint {
		newAPIClient := a.apiClient.FromPing(ping)

		// Before switching to the new one, do a ping test to make sure it's
		// valid. If it is, switch and carry on, otherwise ignore the switch
		newPing, _, err := newAPIClient.Ping(ctx)
		if err != nil {
			a.logger.Warn("Failed to ping the new endpoint %s - ignoring switch for now (%s)", ping.Endpoint, err)
		} else {
			// Replace the APIClient and process the new ping
			a.apiClient = newAPIClient
			a.agent.Endpoint = ping.Endpoint
			ping = newPing
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
	// Note: Context.Cancel is a blunt instrument. It will (for example)
	// prevent the final API calls to upload remaining logs and mark the job
	// finished.
	// But we do want to abort the retry loop in AcquireJob early if possible.
	// So, use context cancellation to abort AcquireJob on agent stop, but not
	// RunJob.
	// The agent's signal handler handles cancellation after a job has begun.
	acquireCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-a.stop
		cancel()
	}()

	job, err := a.client.AcquireJob(acquireCtx, jobId)
	if err != nil {
		return fmt.Errorf("failed to acquire job: %w", err)
	}

	// Now that we've acquired the job, let's run it.
	return a.RunJob(ctx, job, nil)
}

// Accepts a job and runs it, only returns an error if something goes wrong
func (a *AgentWorker) AcceptAndRunJob(ctx context.Context, job *api.Job, idleMon *idleMonitor) error {
	a.logger.Info("Assigned job %s. Accepting...", job.ID)

	// An agent is busy during a job, and idle when the job is done.
	idleMon.markBusy(a)
	defer idleMon.markIdle(a)

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

	// If we're disconnecting-after-job, signal back to Buildkite that we're not
	// interested in jobs after this one.
	var ignoreAgentInDispatches *bool
	if a.agentConfiguration.DisconnectAfterJob {
		ignoreAgentInDispatches = ptr.To(true)
	}

	// Now that we've accepted the job, let's run it
	return a.RunJob(ctx, accepted, ignoreAgentInDispatches)
}

func (a *AgentWorker) RunJob(ctx context.Context, acceptResponse *api.Job, ignoreAgentInDispatches *bool) error {
	a.setBusy(acceptResponse.ID)
	defer a.setIdle()

	jobsStarted.Inc()
	defer jobsEnded.Inc()

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
	if !a.jobRunner.CompareAndSwap(nil, jr) {
		return fmt.Errorf("Agent worker already has a job running")
	}
	// No more job, no more runner.
	defer a.jobRunner.Store(nil)

	// Start running the job
	if err := jr.Run(ctx, ignoreAgentInDispatches); err != nil {
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
