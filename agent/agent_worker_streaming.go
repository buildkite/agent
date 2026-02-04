package agent

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

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

	// reconnInterval functions similarly to pingInterval, except we expect
	// the resulting connection to last much longer. By default, attempt to
	// reconnect no more than once every 10 seconds.
	reconnInterval := time.Second * time.Duration(max(10, a.agent.PingInterval))
	if a.agentConfiguration.PingMode == "stream-only" {
		// If it's only us, then allow reconnecting as though each stream was
		// a ping.
		reconnInterval = time.Second * time.Duration(a.agent.PingInterval)
	}
	reconnTicker := time.Tick(reconnInterval)

	// The first jitter interval is small, to allow fast startup, but still
	// spread connections a bit when lots of agents start at the same time.
	// Later it is set to reconnInterval.
	jitterInterval := 1 * time.Second

	// On the first iteration, skip waiting for the reconnTicker.
	skipWait := make(chan struct{}, 1)
	skipWait <- struct{}{}
	if a.noWaitBetweenPingsForTesting {
		// a closed channel will unblock the select instantly, for zero-delay loop testing.
		close(skipWait)
	}

	for {
		setStat("😴 Waiting to reconnect to stream")
		a.logger.Debug("[runStreamingLoop] Waiting for reconnTicker")
		select {
		case <-skipWait:
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
		jitter := rand.N(jitterInterval)
		setStat(fmt.Sprintf("🫨 Jittering for %v", jitter))
		a.logger.Debug("[runStreamingLoop] Waiting for jitter %v", jitter)
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
		jitterInterval = reconnInterval

		setStat("📱 Connecting to ping stream...")
		a.logger.Debug("[runStreamingLoop] Connecting")
		stream, err := a.apiClient.StreamPings(streamCtx, a.agent.UUID)
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

		setStat("🏞️ Streaming actions from Buildkite")
		a.logger.Debug("[runStreamingLoop] Waiting for a message...")
		for msg, err := range stream {
			a.logger.Debug("[runStreamingLoop] Recieved action %v, %v", msg, err)

			var amsg actionMessage
			switch {
			case err != nil:
				a.logger.Debug("[runStreamingLoop] Connection to ping stream failed or ended: %v", err)
				if isUnrecoverable(err) {
					a.logger.Error("Stopping ping stream loop because the error is unrecoverable: %v", err)
					// Streaming is "best-effort," unless we're in
					// stream-only mode where it's the only available option.
					if a.agentConfiguration.PingMode == "stream-only" {
						return err
					}
					return nil
				}
				a.logger.Debug("[runStreamingLoop] Becoming unhealthy")
				amsg.unhealthy = true

			case msg == nil:
				a.logger.Error("Ping stream yielded a nil message, so assuming the stream is broken")
				a.logger.Debug("[runStreamingLoop] Becoming unhealthy")
				amsg.unhealthy = true

			default:
				switch act := msg.Action.(type) {
				case *agentedgev1.StreamPingsResponse_Resume: // a.k.a. "idle"
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
						a.logger.Debug("[runStreamingLoop] Becoming unhealthy")
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
				a.logger.Debug("[runStreamingPingLoop] Breaking loop because the stream is unhealthy")
				break // and reconnect later
			}
		}
	}
}
