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
	initialMaxJitter := 1 * time.Second
	attempts := 0

	var skipWait chan struct{}
	if a.noWaitBetweenPingsForTesting {
		// a closed channel will unblock the select instantly, for zero-delay loop testing.
		skipWait = make(chan struct{})
		close(skipWait)
	}

	for {
		// Backoff exponentially, up to initialMaxJitter * 2^6.
		// (Repeated failures may jitter up to 64 seconds between attempts.)
		maxJitter := initialMaxJitter << min(attempts, 6)
		attempts++

		// Within the interval, wait a random amount of time to avoid
		// spontaneous synchronisation across agents.
		jitter := rand.N(maxJitter)
		setStat(fmt.Sprintf("🫨 Jittering for %v (max %v)", jitter, maxJitter))
		a.logger.Debug("[runStreamingPingLoop] Waiting for jitter %v (max %v)", jitter, maxJitter)
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

		setStat(fmt.Sprintf("📱 Connecting to ping stream (attempt %d)...", attempts))
		a.logger.Debug("[runStreamingPingLoop] Connecting (attempt %d)", attempts)
		stream, err := a.apiClient.StreamPings(streamCtx, a.agent.UUID)
		if err != nil {
			a.logger.Error("Connection to ping stream failed: %v", err)
			if isUnrecoverable(err) {
				a.logger.Error("Stopping ping stream because the error is unrecoverable")
				return err
			}
			// Fast fallback to the ping loop
			a.logger.Debug("[runStreamingPingLoop] Becoming unhealthy")
			select {
			case outCh <- actionMessage{unhealthy: true}:
				a.logger.Debug("[runStreamingPingLoop] Unhealthy message sent to debouncer")
				// sent!
			case <-a.stop:
				a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
				return nil
			case <-ctx.Done():
				a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
				return ctx.Err()
			}
			continue
		}

		firstMsg := true // used for the "connection established" log

		setStat("🏞️ Streaming actions from Buildkite")
		a.logger.Debug("[runStreamingPingLoop] Waiting for a message...")
	streamLoop:
		for msg, err := range stream {
			a.logger.Debug("[runStreamingPingLoop] Received msg %v, err %v", msg, err)

			var amsg actionMessage
			switch {
			case err != nil:
				a.logger.Debug("[runStreamingPingLoop] Connection to ping stream failed or ended: %v", err)
				if isUnrecoverable(err) {
					a.logger.Error("Stopping ping stream loop because the error is unrecoverable: %v", err)
					return err
				}
				// Go unhealthy, unless the error is deadline-exceeded.
				// (The connection timed out, which we want to happen every so often).
				if connect.CodeOf(err) == connect.CodeDeadlineExceeded {
					a.logger.Debug("[runStreamingPingLoop] Breaking streamLoop to reconnect after deadline-exceeded")
					break streamLoop
				}
				a.logger.Debug("[runStreamingPingLoop] Becoming unhealthy")
				amsg.unhealthy = true

			case msg == nil:
				a.logger.Error("Ping stream yielded a nil message, so assuming the stream is broken")
				a.logger.Debug("[runStreamingPingLoop] Becoming unhealthy")
				amsg.unhealthy = true

			default:
				if firstMsg {
					a.logger.Info("Ping stream connection established")
					firstMsg = false
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
			case outCh <- amsg:
				a.logger.Debug("[runStreamingPingLoop] Message sent to debouncer")
				// sent!
			case <-a.stop:
				a.logger.Debug("[runStreamingPingLoop] Stopping due to agent stop")
				return nil
			case <-ctx.Done():
				a.logger.Debug("[runStreamingPingLoop] Stopping due to context cancel")
				return ctx.Err()
			}

			// In case the server sends a disconnect but doesn't close the
			// stream, be sure to exit.
			if amsg.action == "disconnect" {
				a.logger.Debug("[runStreamingPingLoop] Stopping due to disconnect action")
				a.internalStop()
				return nil
			}

			if amsg.unhealthy {
				a.logger.Debug("[runStreamingPingLoop] Breaking streamLoop to reconnect because the stream is unhealthy")
				break streamLoop
			} else {
				// Stream is healthy, reset the retry counter
				attempts = 0
			}
		}
	}
}
