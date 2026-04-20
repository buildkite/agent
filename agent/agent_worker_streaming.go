package agent

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"connectrpc.com/connect"
	agentedgev1 "github.com/buildkite/agent/v3/api/proto/gen"
	"github.com/buildkite/agent/v3/status"
)

// runStreamingPingLoop runs the streaming loop. It is best-effort
// (allowed to fail and fall back to the regular ping loop) but when it works
// it is preferred because there is less waiting around.
func (a *AgentWorker) runStreamingPingLoop(ctx context.Context, outCh chan<- actionMessage) error {
	a.logger.Debug("[runStreamingPingLoop] Starting")
	defer a.logger.Debug("[runStreamingPingLoop] Exiting")

	// When this loop returns, close the channel to let the next loop stop
	// listening to it.
	defer close(outCh)

	ctx, setStat, _ := status.AddSimpleItem(ctx, "Streaming ping loop")
	defer setStat("🛑 Ping stream loop stopped!")
	setStat("🏃 Starting...")

	// The stream Receive call blocks until a message is received - we can't
	// select on it. streamCtx exists to end the stream on agent stop.
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	go func() {
		<-a.stop
		cancelStream()
	}()

	// Because we expect the streaming connection to last much longer than a
	// ping, we should use a different doctrine compared with the ping loop.
	//
	// This loop is a repeated fuzzed exponential backoff:
	//
	// If the connection is successful, once it closes, the next connection will
	// begin after a minimal jitter.
	// While the connection fails, each attempt will jitter over double the
	// previous interval before attempting reconnection.
	//
	// Note: This _could_ be implemented with an infinite loop containing a roko
	// retrier, but it looked a bit messier to me.
	initialWindow := 1 * time.Second

	var skipWait chan struct{}
	if a.noWaitBetweenPingsForTesting {
		// a closed channel will unblock the select instantly, for zero-delay loop testing.
		skipWait = make(chan struct{})
		close(skipWait)
	}

	state := &streamLoopState{
		AgentWorker: a,
		outCh:       outCh,
		setStat:     setStat,
	}

	for {
		// Backoff exponentially, up to initialWindow * 2^6.
		// (Repeated failures may jitter up to 64 seconds between attempts.)
		window := initialWindow << min(state.attempts, 6)
		windowEnd := time.After(window)
		state.attempts++

		// Within the interval, wait a random amount of time to avoid
		// spontaneous synchronisation across agents.
		jitter := rand.N(window)
		setStat(fmt.Sprintf("🫨 Jittering for %v (max %v)", jitter, window))
		a.logger.Debug("[runStreamingPingLoop] Waiting for jitter %v (max %v)", jitter, window)
		select {
		case <-skipWait:
			// continue below
		case <-time.After(jitter):
			// continue below
		case <-a.stop:
			a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
			return ctx.Err()
		}

		err := state.startStream(ctx, streamCtx)
		if err == errInternalStop {
			return nil
		}
		if err != nil {
			return err
		}

		// Wait the remainder of the jitter window.
		// Windowing the jitter this way avoids statistical effects.
		// (If we started a new jitter right away, the Nth request would
		// happen at an approximately Normally-distributed time after start,
		// because that's a sum of random variables each with finite variance.
		// Central Limit Theorem! We'd rather have a uniform distribution
		// over a window.)
		setStat("😴 Waiting for remainder of window")
		a.logger.Debug("[runStreamingPingLoop] Waiting for remainder of window")
		select {
		case <-skipWait:
			// continue next iteration
		case <-windowEnd:
			// continue next iteration
		case <-a.stop:
			a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
			return ctx.Err()
		}
	}
}

// streamLoopState holds stream loop specific state for startStream
// and streamLoopInner.
type streamLoopState struct {
	*AgentWorker
	outCh    chan<- actionMessage
	attempts int
	firstMsg bool
	setStat  func(string)
}

// startStream attempts 1 connection to the stream and handles its messages.
func (a *streamLoopState) startStream(ctx, streamCtx context.Context) error {
	a.setStat(fmt.Sprintf("📱 Connecting to ping stream (attempt %d)...", a.attempts))
	a.logger.Debug("[runStreamingPingLoop] Connecting (attempt %d)", a.attempts)
	stream, err := a.apiClient.StreamPings(streamCtx, a.agent.UUID)
	if err != nil {
		// TODO: after we've made streaming endpoints generally available,
		// think about making some of these logs error or warning level.
		a.logger.Debug("[runStreamingPingLoop] Connection to ping stream failed: %v", err)
		if isUnrecoverable(err) {
			a.logger.Debug("[runStreamingPingLoop] Stopping because the error is unrecoverable")
			return err
		}
		// Fast fallback to the ping loop
		a.logger.Debug("[runStreamingPingLoop] Becoming unhealthy")
		select {
		case a.outCh <- actionMessage{unhealthy: true}:
			a.logger.Debug("[runStreamingPingLoop] Unhealthy message sent to debouncer")
			// sent!
		case <-a.stop:
			a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
			return errInternalStop
		case <-ctx.Done():
			a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
			return ctx.Err()
		}
		return nil // continue outer streaming loop
	}

	a.firstMsg = true // used for the "connection established" log

	a.setStat("🏞️ Streaming actions from Buildkite")
	a.logger.Debug("[runStreamingPingLoop] Waiting for a message...")
	for msg, streamErr := range stream {
		err := a.handle(ctx, msg, streamErr)
		if err == errInternalBreak {
			break
		}
		if err == errInternalStop {
			return errInternalStop
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *streamLoopState) handle(ctx context.Context, msg *agentedgev1.StreamPingsResponse, streamErr error) error {
	a.logger.Debug("[runStreamingPingLoop] Received msg %v, err %v", msg, streamErr)

	var amsg actionMessage
	switch {
	case streamErr != nil:
		// TODO: after we've made streaming endpoints generally available,
		// think about making some of these logs error or warning level.
		a.logger.Debug("[runStreamingPingLoop] Connection to ping stream failed or ended: %v", streamErr)
		if isUnrecoverable(streamErr) {
			a.logger.Debug("[runStreamingPingLoop] Stopping because the error is unrecoverable")
			return streamErr
		}
		// Stay healthy if the error is deadline-exceeded.
		// (The connection timed out, which we want to happen every so often).
		if connect.CodeOf(streamErr) == connect.CodeDeadlineExceeded {
			a.logger.Debug("[runStreamingPingLoop] Breaking stream loop to reconnect following deadline-exceeded")
			return errInternalBreak
		}
		// It's some other error. Go unhealthy, which unblocks the ping loop.
		a.logger.Debug("[runStreamingPingLoop] Becoming unhealthy")
		amsg.unhealthy = true

	case msg == nil:
		a.logger.Debug("[runStreamingPingLoop] Ping stream yielded a nil message, so assuming the stream is broken")
		a.logger.Debug("[runStreamingPingLoop] Becoming unhealthy")
		amsg.unhealthy = true

	default:
		if a.firstMsg {
			a.logger.Info("Ping stream connection established")
			a.firstMsg = false
		}

		switch act := msg.Action.(type) {
		case *agentedgev1.StreamPingsResponse_Resume: // a.k.a. "idle"
			// continue below

		case *agentedgev1.StreamPingsResponse_Pause:
			if reason := act.Pause.GetReason(); reason != "" {
				a.logger.Info("Pause reason: %s", reason)
			}
			amsg.action = "pause"

		case *agentedgev1.StreamPingsResponse_Disconnect:
			if reason := act.Disconnect.GetReason(); reason != "" {
				a.logger.Info("Disconnect reason: %s", reason)
			}
			amsg.action = "disconnect"

		case *agentedgev1.StreamPingsResponse_JobAssigned:
			amsg.jobID = act.JobAssigned.GetJob().GetId()
			if amsg.jobID == "" {
				a.logger.Error("Ping stream yielded a JobAssigned message with nil job or empty job ID, so assuming the stream is broken")
				a.logger.Debug("[runStreamingPingLoop] Becoming unhealthy")
				amsg.unhealthy = true
			}
		}
	}

	// Send the message to the debouncer.
	select {
	case a.outCh <- amsg:
		a.logger.Debug("[runStreamingPingLoop] Message sent to debouncer")
		// sent!
	case <-a.stop:
		a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
		return errInternalStop
	case <-ctx.Done():
		a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
		return ctx.Err()
	}

	// In case the server sends a disconnect but doesn't close the
	// stream, be sure to exit.
	if amsg.action == "disconnect" {
		a.logger.Debug("[runStreamingPingLoop] Stopping due to disconnect action")
		a.internalStop()
		return errInternalStop
	}

	if amsg.unhealthy {
		a.logger.Debug("[runStreamingPingLoop] Breaking stream loop to reconnect because the stream is unhealthy")
		return errInternalBreak
	}
	// Stream is healthy, reset the retry counter
	a.attempts = 0
	return nil
}
