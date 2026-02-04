package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
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

// See https://connectrpc.com/docs/protocol/#http-to-error-code
var codeUnrecoverable = map[connect.Code]bool{
	connect.CodeInternal:         true, // 400
	connect.CodeUnauthenticated:  true, // 401
	connect.CodePermissionDenied: true, // 403
	connect.CodeUnimplemented:    true, // 404
	// All other codes are implicitly false, but particularly:
	// Unavailable (429, 502, 503, 504) and Unknown (all other HTTP statuses).
}

func isUnrecoverable(err error) bool {
	var u *errUnrecoverable
	if errors.As(err, &u) {
		return true
	}
	return codeUnrecoverable[connect.CodeOf(err)]
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

// Start starts the agent worker.
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

	// There are as many as 4 different loops that send 1 error here each.
	errCh := make(chan error, 4)

	// Use this context to control the heartbeat loop.
	heartbeatCtx, stopHeartbeats := context.WithCancel(ctx)
	defer stopHeartbeats()

	// Start the heartbeat loop but don't wait for it to return (yet).
	go func() {
		errCh <- a.runHeartbeatLoop(heartbeatCtx)
	}()

	// If the agent is booted in acquisition mode, acquire that particular job
	// before running the ping loop.
	// (Why run a ping loop at all? To find out if the agent is paused, which
	// affects whether it terminates after the job.)
	if a.agentConfiguration.AcquireJob != "" {
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

	// toggle ensures only one of the loops is producing actions.
	//
	// Here's how this works:
	//
	// As a channel, toggle is in one of two states: either it is empty, or
	// contains 1 struct{}{} "value" (it's a buffered channel). But we can
	// think of the system as having three possible states:
	//
	// - the toggle is available
	// - the ping loop has the toggle
	// - the debouncer loop has the toggle
	//
	// If toggle contains a value, then either loop can "take" the toggle
	// immediately by receiving from the channel. Otherwise, it has to wait
	// until a value is available for its receive operation to unblock.
	// Similarly, "relinquishing" the toggle is a matter of sending a struct{}{}
	// value to the channel. (Each side can unblock the other side.)
	//
	// Each loop can be doing other things while waiting to take the toggle,
	// because a select statement will choose any operation that can proceed.
	//
	// Each loop keeps track of whether it has currently "has" the toggle or
	// not, and each has a different policy for taking and relinquishing it.
	//
	// The ping loop waits for the toggle to become available and takes it
	// before pinging, and relinquishes the toggle as soon as the action
	// resulting from a ping has completed. In other words, the ping loop tries
	// not to keep the toggle and politely waits for it.
	//
	// The debouncer (acting for the streaming side) instead holds the toggle
	// as long as possible, until the stream becomes unhealthy. The ping loop
	// is usually already waiting and ready to take the toggle at that point.
	// If the streaming side becomes healthy again, the debouncer will try to
	// take the toggle back, and most of the time this is quick because the ping
	// loop spends most of its time waiting in between pings. But if the ping
	// loop still has the toggle, that must be because the ping loop still has
	// an action in progress (probably a job), so the debouncer must wait.
	//
	// The toggle initially belongs to the streaming/debouncer side, unless the
	// ping mode is ping-only, in which case we seed toggle with a value below
	// so the ping loop can simply take it.
	toggle := make(chan struct{}, 1)

	// More channels to enable communication between the various loops.
	fromPingLoopCh := make(chan actionMessage)      // ping loop to action handler
	fromStreamingLoopCh := make(chan actionMessage) // streaming loop to debouncer
	fromDebouncerCh := make(chan actionMessage)     // debouncer to action handler

	// Start the loops and block until they have all stopped.
	// Based on configuration, we have our choice of ping loop,
	// streaming loop+debouncer loop, or both.
	var wg sync.WaitGroup

	pingLoop := func() {
		defer wg.Done()
		errCh <- a.runPingLoop(ctx, toggle, fromPingLoopCh)
	}
	streamingLoop := func() {
		defer wg.Done()
		errCh <- a.runStreamingPingLoop(ctx, fromStreamingLoopCh)
	}
	debouncerLoop := func() {
		defer wg.Done()
		errCh <- a.runDebouncer(ctx, toggle, fromDebouncerCh, fromStreamingLoopCh)
	}

	var loops []func()
	switch a.agentConfiguration.PingMode {
	case "", "auto":
		loops = []func(){pingLoop, streamingLoop, debouncerLoop}

	case "ping-only":
		// Only add the ping loop, and let it take the toggle.
		loops = []func(){pingLoop}
		toggle <- struct{}{}
		fromDebouncerCh = nil // prevent action loop listening to streaming side

	case "stream-only":
		loops = []func(){streamingLoop, debouncerLoop}
		fromPingLoopCh = nil // prevent action loop listening to ping side
	}

	// There's always an action handler.
	actionLoop := func() {
		defer wg.Done()
		errCh <- a.runActionLoop(ctx, idleMon, fromPingLoopCh, fromDebouncerCh)
	}
	loops = append(loops, actionLoop)

	// Go loops!
	wg.Add(len(loops))
	for _, l := range loops {
		go l()
	}
	wg.Wait()

	// The source loops have ended, so stop the heartbeat loop.
	stopHeartbeats()

	// Block until all loops have returned, then join the errors.
	// (Note that errors.Join does the right thing with nil.)
	// All loops are context aware, so no need to wait on ctx here.
	var err error
	for range len(loops) + 1 { // loops + heartbeat loop
		err = errors.Join(err, <-errCh)
	}
	return err
}

func (a *AgentWorker) internalStop() {
	a.stopOnce.Do(func() {
		// Use the closure of the stop channel as a signal to the main run
		// loop in Start() to stop looping and terminate
		close(a.stop)
	})
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

	a.internalStop()
}

// StopUngracefully stops the agent from accepting new work and cancels any
// existing job. It blocks until the job is cancelled, if there is one.
func (a *AgentWorker) StopUngracefully() {
	a.internalStop()

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

type actionMessage struct {
	// Details of the action to execute
	action, jobID string

	// Results of the action
	errCh chan<- error

	// Secret internal action between the streaming loop and debouncer:
	// set to true when the streaming loop is unhealthy
	// and the toggle should be returned so the ping loop is unblocked
	// (once the current action is completed, if that's the case).
	unhealthy bool
}
