package agent

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/status"
)

// runPingLoop runs the (classical) loop that pings Buildkite for work.
func (a *AgentWorker) runPingLoop(ctx context.Context, bat *baton, outCh chan<- actionMessage) error {
	a.logger.Debug("[runPingLoop] Starting")
	defer a.logger.Debug("[runPingLoop] Exiting")

	// When this loop returns, close the channel to let the action handler loop
	// stop listening for actions from it.
	defer close(outCh)

	ctx, setStat, _ := status.AddSimpleItem(ctx, "Ping loop")
	defer setStat("🛑 Ping loop stopped!")
	setStat("🏃 Starting...")

	pingInterval := time.Second * time.Duration(a.agent.PingInterval)
	state := &pingLoopState{
		AgentWorker:  a,
		bat:          bat,
		outCh:        outCh,
		pingInterval: pingInterval,
		pingTicker:   time.NewTicker(pingInterval),
		skipWait:     make(chan struct{}, 1),
		setStat:      setStat,
	}
	defer state.pingTicker.Stop()

	// On the first iteration, skip waiting for the pingTicker.
	// One buffered value won't skip the jitter, though.
	state.skipWait <- struct{}{}
	if a.noWaitBetweenPingsForTesting {
		// a closed channel will unblock the for/select instantly, for zero-delay ping loop testing.
		close(state.skipWait)
	}

	a.logger.Info("Waiting for instructions...")

	for {
		startWait := time.Now()
		a.logger.Debug("[runPingLoop] Waiting for pingTicker")
		setStat("😴 Waiting until next ping interval tick")
		select {
		case <-state.skipWait:
			// continue below
		case <-state.pingTicker.C:
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
		a.logger.Debug("[runPingLoop] Waiting for jitter %v", jitter)
		setStat(fmt.Sprintf("🫨 Jittering for %v", jitter))
		select {
		case <-state.skipWait:
			// continue below
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

		err := state.pingLoopInner(ctx)
		if err == internalStop {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// pingLoopState exists to pass parameters to pingLoopInner.
type pingLoopState struct {
	*AgentWorker
	bat          *baton
	outCh        chan<- actionMessage
	setStat      func(string)
	pingTicker   *time.Ticker
	pingInterval time.Duration
	skipWait     chan struct{}
}

func (a *pingLoopState) pingLoopInner(ctx context.Context) error {
	// Wait until the baton is available. If this takes forever, that's
	// a good thing because it should mean the streaming loop is
	// healthy.
	// Once acquired, only release the baton after any work is complete,
	// to prevent the streaming loop from resuming control until then,
	// but we always release the baton, because the streaming loop is
	// preferred.
	a.logger.Debug("[runPingLoop] Waiting for baton")
	select {
	case <-a.bat.Acquire(): // the baton is ours!
		a.bat.Acquired(actorPingLoop)
		a.logger.Debug("[runPingLoop] Acquired the baton")
		defer func() { // <- this is why the ping loop body is in a func
			a.logger.Debug("[runPingLoop] Releasing the baton")
			a.bat.Release(actorPingLoop)
		}()

	case <-a.stop:
		a.logger.Debug("[runPingLoop] Stopping due to agent stop")
		return internalStop
	case <-ctx.Done():
		a.logger.Debug("[runPingLoop] Stopping due to context cancel")
		return ctx.Err()
	}

	a.logger.Debug("[runPingLoop] Pinging buildkite for instructions")
	a.setStat("📡 Pinging Buildkite for instructions")
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
	errCh := make(chan error)
	msg := actionMessage{
		action: action,
		jobID:  jobID,
		errCh:  errCh,
	}
	select {
	case a.outCh <- msg:
		// sent!
	case <-a.stop:
		a.logger.Debug("[runPingLoop] Stopping due to agent stop")
		return internalStop
	case <-ctx.Done():
		a.logger.Debug("[runPingLoop] Stopping due to context cancel")
		return ctx.Err()
	}

	// Wait for completion
	select {
	case err := <-errCh:
		if err != nil || jobID == "" {
			// We don't terminate the ping loop just because the
			// action (usually a job) has failed.
			return nil
		}
		if a.noWaitBetweenPingsForTesting {
			// Don't bother resetting the ticker,
			// don't try to send on a closed channel (skipWait).
			return nil
		}
		// A job ran (or was at least started) successfully.
		// Observation: jobs are rarely the last within a pipeline,
		// thus if this worker just completed a job,
		// there is likely another immediately available.
		// Skip waiting for the ping interval until
		// a ping without a job has occurred,
		// but in exchange, ensure the next ping must wait at least a full
		// pingInterval to avoid too much server load.
		a.pingTicker.Reset(a.pingInterval)
		select {
		case a.skipWait <- struct{}{}:
			// Ticker will be skipped
		default:
			// We're already skipping the ticker, don't block.
		}
		return nil
	case <-a.stop:
		a.logger.Debug("[runPingLoop] Stopping due to agent stop")
		return internalStop
	case <-ctx.Done():
		a.logger.Debug("[runPingLoop] Stopping due to context cancel")
		return ctx.Err()
	}
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
