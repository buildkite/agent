package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"connectrpc.com/connect"
	"github.com/buildkite/agent/v3/api"
	agentedgev1 "github.com/buildkite/agent/v3/api/proto/gen"
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
	// The toggle initially belongs to the streaming/debouncer side.
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
		loops = append(loops, pingLoop, streamingLoop, debouncerLoop)

	case "ping-only":
		// Only add the ping loop, and let it take the toggle.
		loops = append(loops, pingLoop)
		toggle <- struct{}{}
		fromDebouncerCh = nil // prevent action loop listening to streaming side

	case "stream-only":
		loops = append(loops, streamingLoop, debouncerLoop)
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

func (a *AgentWorker) runHeartbeatLoop(ctx context.Context) error {
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
				if isUnrecoverable(err) {
					a.logger.Error("%s", err)
					// unrecoverable heartbeat failure also stops everything else
					a.StopUngracefully()
					return err
				}

				// Get the last heartbeat time to the nearest microsecond
				a.stats.Lock()
				if a.stats.lastHeartbeat.IsZero() {
					a.logger.Error("Failed to heartbeat %s. Will try again in %v. (No heartbeat yet)",
						err, heartbeatInterval)
				} else {
					a.logger.Error("Failed to heartbeat %s. Will try again in %v. (Last successful was %v ago)",
						err, heartbeatInterval, time.Since(a.stats.lastHeartbeat))
				}
				a.stats.Unlock()
			}

		case <-ctx.Done():
			a.logger.Debug("Stopping heartbeats due to context cancel")
			// An alternative to returning nil would be ctx.Err(), but we use
			// the context for ordinary termination of this loop.
			// A context cancellation from outside the agent worker would still
			// be reflected in the value returned by the ping loop return.
			return nil
		}
	}
}

// runStreamingPingLoop runs the streaming loop. It is best-effort
// (allowed to fail and fall back to the regular ping loop) but when it works
// it is preferred because there is less waiting around.
func (a *AgentWorker) runStreamingPingLoop(ctx context.Context, outCh chan<- actionMessage) error {
	// When this loop returns, close the channel to let the next loop stop
	// listening to it.
	defer close(outCh)

	ctx, setStat, _ := status.AddSimpleItem(ctx, "Streaming ping loop")
	defer setStat("🛑 Ping stream loop stopped!")
	setStat("🏃 Starting...")

	// reconnInterval functions similarly to pingInterval, except we expect
	// the resulting connection to last much longer.
	reconnInterval := 10 * time.Second
	if a.agentConfiguration.PingMode == "stream-only" {
		// If it's only us, then allow reconnecting more frequently.
		reconnInterval = time.Second * time.Duration(a.agent.PingInterval)
	}
	reconnTicker := time.Tick(reconnInterval)

	// On the first iteration, skip waiting for the reconnTicker.
	// This doesn't skip the jitter, though.
	skipTicker := make(chan struct{}, 1)
	skipTicker <- struct{}{}

	for {
		setStat("😴 Waiting to reconnect to stream")
		select {
		case <-skipTicker:
			// continue below
		case <-reconnTicker:
			// continue below
		case <-a.stop:
			a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
			return ctx.Err()
		}

		// Within the interval, wait a random amount of time to avoid
		// spontaneous synchronisation across agents.
		jitter := rand.N(reconnInterval)
		setStat(fmt.Sprintf("🫨 Jittering for %v", jitter))
		select {
		case <-time.After(jitter):
			// continue below
		case <-a.stop:
			a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
			return ctx.Err()
		}

		setStat("📱 Connecting to ping stream...")
		stream, err := a.apiClient.StreamPings(ctx, a.agent.UUID)
		if err != nil {
			a.logger.Error("Connection to ping stream failed: %v", err)
			if isUnrecoverable(err) {
				a.logger.Error("Stopping ping stream because the error is unrecoverable")
				// Streaming is best-effort but preferred, unless we're in
				// stream-only mode, where it's the only available option.
				if a.agentConfiguration.PingMode == "stream-only" {
					return err
				}
				return nil
			}

			continue
		}

		first := true
		setStat("🏞️ Streaming actions from Buildkite")
		for msg, err := range stream {
			var amsg actionMessage
			switch {
			case err != nil:
				a.logger.Error("Connection to ping stream failed or ended: %v", err)
				if isUnrecoverable(err) {
					a.logger.Error("Stopping ping stream loop because the error is unrecoverable")
					// Streaming is "best-effort," unless we're in
					// stream-only mode where it's the only available option.
					if a.agentConfiguration.PingMode == "stream-only" {
						return err
					}
					return nil
				}
				amsg.unhealthy = true

			case msg == nil:
				a.logger.Error("Ping stream yielded a nil message, so assuming the stream is broken")
				amsg.unhealthy = true

			default:
				amsg.first = first
				first = false

				switch act := msg.Action.(type) {
				case *agentedgev1.StreamPingsResponse_Idle:
					// continue below

				case *agentedgev1.StreamPingsResponse_Pause:
					if reason := act.Pause.GetReason(); reason != "" {
						a.logger.Info("%s", reason)
					}
					amsg.action = "pause"

				case *agentedgev1.StreamPingsResponse_Disconnect:
					if reason := act.Disconnect.GetReason(); reason != "" {
						a.logger.Info("%s", reason)
					}
					amsg.action = "disconnect"

				case *agentedgev1.StreamPingsResponse_JobAssigned:
					amsg.jobID = act.JobAssigned.GetJob().GetId()
					if amsg.jobID == "" {
						a.logger.Error("Ping stream yielded a JobAssigned message with nil job or empty job ID, so assuming the stream is broken")
						amsg.unhealthy = true
					}
				}
			}

			// Send the message to the debouncer.
			select {
			case outCh <- amsg:
				// sent!
			case <-a.stop:
				a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
				return nil
			case <-ctx.Done():
				a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
				return ctx.Err()
			}

			if amsg.unhealthy {
				break // and reconnect later
			}
		}
	}
}

// runDebouncer is an event debouncing loop between the streaming loop and the
// action handler loop.
//
// There are two *big* differences between the streaming loop and the
// classical ping loop:
//
//  1. When pings happen, they happen "regularly". Actions are only sent
//     in response. But when the streaming loop receives messages is up to
//     the backend.
//  2. Pings can be put on hold while a job is running. But streaming
//     messages can keep arriving during a job.
//
// Firstly, we want to get back to receiving from the stream
// as soon as possible, rather than blocking until the action is handled,
// so that the stream remains healthy.
// Secondly, we need to reduce consecutive messages down to only 0 or 1 correct
// next action(s) following a job.
// For example, say during a job someone clicks "pause" and "resume"
// and "pause" again on this agent. This may cause three distinct
// events to be sent to the streaming loop. If we pass them all on to the
// action handler directly, then the "resume" may cause the agent to
// exit in a one-shot mode, even though the second "pause" means the
// user actually *did* want the agent to be paused.
func (a *AgentWorker) runDebouncer(ctx context.Context, toggle chan struct{}, outCh chan<- actionMessage, inCh <-chan actionMessage) error {
	// When the debouncer returns, close the output channel to let the next
	// loop know to stop listening to it.
	defer close(outCh)

	// We begin holding the toggle. (The ping loop is prevented from running.)
	haveToggle := true

	// Return the toggle when we're no longer running so that the regular
	// ping loop can have a go (if it hasn't also ended).
	defer func() {
		select {
		case toggle <- struct{}{}:
		default:
		}
	}()

	// This "closed" channel is used for a little hack below.
	// `<-cmp.Or(lastActionDone, closed)` will receive from:
	// - lastActionDone, if lastActionDone is not nil, or
	// - closed (i.e. immediately) if lastActionDone is nil.
	closed := make(chan struct{})
	close(closed)

	// lastActionDone is closed when the action handler is done handling the
	// last action we sent.
	// It starts nil because at the beginning, there is no previous action.
	var lastActionDone chan struct{}

	// nextAction and nextJobID are the next action that should be sent.
	var nextAction string
	var nextJobID string

	// pending tracks whether there is an action to send.
	pending := false

	// Is the stream healthy?
	// If so, take the toggle (which blocks the ping loop).
	// If not, return the toggle (unblocking the ping loop).
	// Returning the toggle may have to wait for the current action to complete.
	healthy := true

	for {
		select {
		case <-a.stop:
			a.logger.Debug("[runDebouncer] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runDebouncer] Stopping due to context cancel")
			return ctx.Err()

		case <-iif(healthy, toggle): // if the stream is healthy, take the toggle
			// We have the toggle again!
			haveToggle = true

			// Do we have a pending message?
			if !pending {
				continue
			}
			// Yes, there is something to send.
			pending = false
			lastActionDone = make(chan struct{})
			msg := actionMessage{
				action: nextAction,
				jobID:  nextJobID,
				done:   lastActionDone,
			}
			select {
			case outCh <- msg:
				// sent!
			case <-a.stop:
				a.logger.Debug("[runDebouncer] Stopping due to agent stop")
				return nil
			case <-ctx.Done():
				a.logger.Debug("[runDebouncer] Stopping due to context cancel")
				return ctx.Err()
			}

		case msg, open := <-inCh: // streaming loop has produced an event
			if !open {
				a.logger.Debug("[runDebouncer] Stopping due to input channel closing")
				return nil
			}

			// Is the streaming side healthy?
			healthy = !msg.unhealthy

			if !healthy {
				// It is not, so unblock the toggle as soon as we can (when the
				// current action is done).
				select {
				case <-cmp.Or(lastActionDone, closed):
					// The last action is done, or there is no last action.
					// Either way, we can return the toggle immediately.
					toggle <- struct{}{}
					haveToggle = false

				default:
					// No, wait until the action is complete.
					// (Logic is in <-lastActionDone branch.)
				}
				continue
			}

			// Ignore the first message from the stream, which is always idle.
			if msg.first {
				continue
			}

			// Yes, we're healthy. Do we have the toggle?
			if !haveToggle {
				// No, the ping loop is currently in possession of the toggle.
				// Debounce messages until we have it.
				nextAction = msg.action
				nextJobID = msg.jobID
				pending = true
				continue
			}

			// Yes, we have the toggle.
			// Can we send this message right away?
			select {
			case <-cmp.Or(lastActionDone, closed):
				// The last action is done, or there is no last action.
				// Either way, we're clear to pass this message on to the
				// action handler right away.
				pending = false
				lastActionDone = make(chan struct{})
				msg.done = lastActionDone
				select {
				case outCh <- msg:
					// sent!
				case <-a.stop:
					a.logger.Debug("[runDebouncer] Stopping due to agent stop")
					return nil
				case <-ctx.Done():
					a.logger.Debug("[runDebouncer] Stopping due to context cancel")
					return ctx.Err()
				}

			default:
				// The current action is ongoing. Debounce until it is complete.
				nextAction = msg.action
				nextJobID = msg.jobID
				pending = true
			}

		case <-lastActionDone: // most recent action has completed
			// First, set it to nil so we don't come back here right
			// away. (Operations on a nil channel block forever.)
			lastActionDone = nil
			// Is the streaming side healthy?
			if !healthy {
				// No, we're not healthy. If we have the toggle, now is the
				// time to give it up, falling back to the ping loop.
				if haveToggle {
					toggle <- struct{}{}
				}
				continue
			}
			// Yes, we're healthy. Is there a pending message to send?
			if !pending {
				// Nothing waiting to be sent.
				continue
			}
			// Yes, there is something to send. Let's send it!
			pending = false
			lastActionDone = make(chan struct{})
			msg := actionMessage{
				action: nextAction,
				jobID:  nextJobID,
				done:   lastActionDone,
			}
			select {
			case outCh <- msg:
				// sent!
			case <-a.stop:
				a.logger.Debug("[runDebouncer] Stopping due to agent stop")
				return nil
			case <-ctx.Done():
				a.logger.Debug("[runDebouncer] Stopping due to context cancel")
				return ctx.Err()
			}
		}
	}
}

// runPingLoop runs the (classical) loop that pings Buildkite for work.
func (a *AgentWorker) runPingLoop(ctx context.Context, toggle chan struct{}, outCh chan<- actionMessage) error {
	// When this loop returns, close the channel to let the action handler loop
	// stop listening for actions from it.
	defer close(outCh)

	ctx, setStat, _ := status.AddSimpleItem(ctx, "Ping loop")
	defer setStat("🛑 Ping loop stopped!")
	setStat("🏃 Starting...")

	// Create the ticker
	pingInterval := time.Second * time.Duration(a.agent.PingInterval)
	pingTicker := time.Tick(pingInterval)

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

	for {
		startWait := time.Now()
		setStat("😴 Waiting until next ping interval tick")
		select {
		case <-testTriggerCh:
			// instant receive from closed chan when noWaitBetweenPingsForTesting is true
		case <-skipTicker:
			// continue below
		case <-pingTicker:
			// continue below
		case <-a.stop:
			a.logger.Debug("[runPingLoop] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runPingLoop] Stopping due to context cancel")
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
			a.logger.Debug("[runPingLoop] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runPingLoop] Stopping due to context cancel")
			return ctx.Err()
		}
		pingWaitDurations.Observe(time.Since(startWait).Seconds())

		// stop is only used internally when stopping.
		stop := errors.New("stop")
		err := func() error {
			// Wait until the ping loop is unblocked.
			// If the streaming loop is working, then this loop
			// (the ping loop) _should_ be blocked from continuing.
			// Return the token after any work is complete, to prevent the
			// streaming loop from taking back over until then.
			select {
			case <-toggle: // the toggle is ours!
				defer func() { // <- this is why the loop body is in a func
					toggle <- struct{}{}
				}()

			case <-a.stop:
				a.logger.Debug("[runPingLoop] Stopping due to agent stop")
				return stop
			case <-ctx.Done():
				a.logger.Debug("[runPingLoop] Stopping due to context cancel")
				return ctx.Err()
			}

			a.logger.Debug("[runPingLoop] Pinging buildkite for instructions")

			setStat("📡 Pinging Buildkite for instructions")
			pingsSent.Inc()
			startPing := time.Now()
			jobID, action, err := a.Ping(ctx)
			if err != nil {
				pingErrors.Inc()
				if isUnrecoverable(err) {
					a.logger.Error("%v", err)
					return err
				}
				a.logger.Warn("%v", err)
			}
			pingDurations.Observe(time.Since(startPing).Seconds())

			a.logger.Debug("[runPingLoop] Sending action")

			// Send the action to the action loop
			done := make(chan struct{})
			msg := actionMessage{
				action: action,
				jobID:  jobID,
				done:   done,
			}
			select {
			case outCh <- msg:
				// sent!
			case <-a.stop:
				a.logger.Debug("[runPingLoop] Stopping due to agent stop")
				return stop
			case <-ctx.Done():
				a.logger.Debug("[runPingLoop] Stopping due to context cancel")
				return ctx.Err()
			}

			// Wait for completion
			select {
			case <-done:
				// Done!
				return nil
			case <-a.stop:
				a.logger.Debug("[runPingLoop] Stopping due to agent stop")
				return stop
			case <-ctx.Done():
				a.logger.Debug("[runPingLoop] Stopping due to context cancel")
				return ctx.Err()
			}
		}()
		if err == stop {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type actionMessage struct {
	// Details of the action to execute
	action, jobID string

	// Closed by the action handler when the action is completed
	done chan<- struct{}

	// Secret internal action between the streaming loop and debouncer:
	// set to true when the streaming loop is unhealthy
	// and the toggle should be returned so the ping loop is unblocked
	// (once the current action is completed, if that's the case).
	unhealthy bool

	// If this is the first message from a stream, we ignore the contents
	// (or lack of contents). The first message is always Idle, and is sent
	// to ensure the headers are sent and the connection is healthy.
	first bool
}

func (a *AgentWorker) runActionLoop(ctx context.Context, idleMon *idleMonitor, fromPingLoop, fromStreamingLoop <-chan actionMessage) error {
	// Once this loop terminates, there's no point continuing the others,
	// because nothing remains to execute their actions.
	defer a.internalStop()

	ctx, setStat, _ := status.AddSimpleItem(ctx, "Action loop")
	defer setStat("🛑 Action loop stopped!")
	setStat("🏃 Starting...")
	a.logger.Debug("[runActionLoop] Starting")
	defer a.logger.Debug("[runActionLoop] Exiting")

	// Start timing disconnect-after-uptime, if configured.
	var disconnectAfterUptime <-chan time.Time
	maxUptime := a.agentConfiguration.DisconnectAfterUptime
	if maxUptime > 0 {
		disconnectAfterUptime = time.After(maxUptime)
	}

	exitWhenNotPaused := false // the next time the action isn't "pause", exit
	ranJob := false
	paused := false

	for {
		// Did both sources of actions terminate? Then we're done too.
		if fromPingLoop == nil && fromStreamingLoop == nil {
			a.logger.Debug("[runActionLoop] All action sources channels are closed, exiting")
			return nil
		}

		// Wait for one of the following:
		// - an action
		// - the context to be cancelled
		// - the agent is stopping (a.stop)
		// - the idle monitor has declared we're all exiting
		//   (if DisconnectAfterIdleTimeout is configured & we're not paused)
		// - disconnect after uptime
		//   (if DisconnectAfterUptime is configured & we're not paused)
		a.logger.Debug("[runActionLoop] Waiting for an action...")
		setStat("⌚️ Waiting for an action...")
		var msg actionMessage
		select {
		case m, open := <-fromPingLoop:
			if !open {
				// Setting to nil prevents this branch of the select from
				// happening again.
				fromPingLoop = nil
				continue
			}
			a.logger.Debug("[runActionLoop] Got action %q from ping loop", m.action)
			msg = m
			// continue below

		case m, open := <-fromStreamingLoop:
			if !open {
				fromStreamingLoop = nil
				continue
			}
			a.logger.Debug("[runActionLoop] Got action %q from streaming loop", m.action)
			msg = m
			// continue below

		case <-ctx.Done():
			a.logger.Debug("[runActionLoop] Stopping due to context cancel")
			return ctx.Err()

		case <-a.stop:
			a.logger.Debug("[runActionLoop] Stopping due to agent stop")
			return nil

		case <-disconnectAfterUptime:
			a.logger.Info("Agent has exceeded max uptime of %v", maxUptime)
			if paused {
				// Wait to be unpaused before exiting
				a.logger.Info("Awaiting resume before disconnecting...")
				exitWhenNotPaused = true
				continue
			}
			a.logger.Info("Disconnecting...")
			return nil

		case <-idleMon.Exiting():
			// This should only happen if the agent isn't paused.
			// (Pausedness is a kind of non-idleness.)
			a.logger.Info("All agents have been idle for at least %v. Disconnecting...", idleMon.idleTimeout)
			return nil
		}

		// Let's handle the action!
		a.logger.Debug("[runActionLoop] Performing %q action", msg.action)
		setStat(fmt.Sprintf("🧑‍🍳 Performing %q action...", msg.action))
		pingActions.WithLabelValues(msg.action).Inc()

		// In cases where we need to disconnect, *don't* close msg.done,
		// in order to force the <-a.stop branch in the other loops.
		// Otherwise, be sure to close(msg.done)!
		switch msg.action {
		case "disconnect":
			a.logger.Debug("[runActionLoop] Stopping action loop due to disconnect action")
			return nil

		case "pause":
			// An agent is not dispatched any jobs while it is paused, but the
			// paused agent is expected to remain alive and pinging for
			// instructions.
			// *This includes acquire-job and disconnect-after-idle-timeout.*
			a.logger.Debug("[runActionLoop] Entering pause state")
			paused = true
			// For the purposes of deciding whether or not to exit,
			// pausedness is a kind of non-idleness.
			// If there's also no job, agent is marked as idle below.
			idleMon.MarkBusy(a)
			close(msg.done)
			continue
		}

		// At this point, action was neither "disconnect" nor "pause".
		if exitWhenNotPaused {
			a.logger.Debug("[runActionLoop] Stopping action loop because exitWhenNotPaused is true")
			return nil
		}
		if paused {
			// We're not paused any more! Log a helpful message.
			a.logger.Info("Agent has resumed after being paused")
			paused = false
		}

		// For acquire-job agents, registration sets ignore-in-dispatches=true,
		// so jobID should be empty. If not, complain.
		if a.agentConfiguration.AcquireJob != "" {
			if msg.jobID != "" {
				a.logger.Error("Agent ping dispatched a job (id %q) but agent is in acquire-job mode! Ignoring the new job", msg.jobID)
			}
			// Disconnect after acquire-job.
			return nil
		}

		// In disconnect-after-job mode, finishing the job sets
		// ignore-in-dispatches=true. So jobID should be empty. If not, complain.
		if ranJob && a.agentConfiguration.DisconnectAfterJob {
			if msg.jobID != "" {
				a.logger.Error("Agent ping dispatched a job (id %q) but agent is in disconnect-after-job mode (and already ran a job)! Ignoring the new job", msg.jobID)
			}
			a.logger.Info("Job ran, and disconnect-after-job is enabled. Disconnecting...")
			return nil
		}

		// If the jobID is empty, then it's an idle message
		if msg.jobID == "" {
			// This ensures agents that never receive a job are still tracked
			// by the idle monitor and can properly trigger disconnect-after-idle-timeout.
			idleMon.MarkIdle(a)
			close(msg.done)
			continue
		}

		setStat("💼 Accepting job")

		// Runs the job, only errors if something goes wrong
		if err := a.AcceptAndRunJob(ctx, msg.jobID, idleMon); err != nil {
			a.logger.Error("%v", err)
			setStat(fmt.Sprintf("✅ Finished job with error: %v", err))
			close(msg.done)
			continue
		}

		ranJob = true
		close(msg.done)
	}
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

// Performs a ping that checks Buildkite for a job or action to take
// Returns a job, or nil if none is found
func (a *AgentWorker) Ping(ctx context.Context) (jobID, action string, err error) {
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
			return "", action, &errUnrecoverable{action: "Ping", response: resp, err: pingErr}
		}

		// Get the last ping time to the nearest microsecond
		a.stats.Lock()
		defer a.stats.Unlock()

		// If a ping fails, we don't really care, because it'll
		// ping again after the interval.
		if a.stats.lastPing.IsZero() {
			return "", action, fmt.Errorf("Failed to ping: %w (No successful ping yet)", pingErr)
		} else {
			return "", action, fmt.Errorf("Failed to ping: %w (Last successful was %v ago)", pingErr, time.Since(a.stats.lastPing))
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
		return "", action, nil
	}

	return ping.Job.ID, action, nil
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
func (a *AgentWorker) AcceptAndRunJob(ctx context.Context, jobID string, idleMon *idleMonitor) error {
	a.logger.Info("Assigned job %s. Accepting...", jobID)

	// An agent is busy during a job, and idle when the job is done.
	idleMon.MarkBusy(a)
	defer idleMon.MarkIdle(a)

	// Accept the job. We'll retry on connection related issues, but if
	// Buildkite returns a 422 or 500 for example, we'll just bail out,
	// re-ping, and try the whole process again.
	r := roko.NewRetrier(
		roko.WithMaxAttempts(30),
		roko.WithStrategy(roko.Constant(5*time.Second)),
	)

	accepted, err := roko.DoFunc(ctx, r, func(r *roko.Retrier) (*api.Job, error) {
		accepted, _, err := a.apiClient.AcceptJob(ctx, jobID)
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

// iif returns t if b is true, otherwise it returns the zero value of T.
// This is useful for enabling or disabling a select case based on a test
// evaluated at the start of the select.
func iif[T any](b bool, t T) T {
	if b {
		return t
	}
	var f T
	return f
}
