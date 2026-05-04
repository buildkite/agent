package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/buildkite/agent/v4/status"
)

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
					a.logger.Error(fmt.Sprintf("%s", err))
					// unrecoverable heartbeat failure also stops everything else
					a.StopUngracefully()
					return err
				}

				// Get the last heartbeat time to the nearest microsecond
				a.stats.Lock()
				if a.stats.lastHeartbeat.IsZero() {
					a.logger.Error(fmt.Sprintf("Failed to heartbeat %s. Will try again in %v. (No heartbeat yet)",
						err, heartbeatInterval))
				} else {
					a.logger.Error(fmt.Sprintf("Failed to heartbeat %s. Will try again in %v. (Last successful was %v ago)",
						err, heartbeatInterval, time.Since(a.stats.lastHeartbeat)))
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
