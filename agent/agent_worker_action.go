package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/ptr"
	"github.com/buildkite/agent/v3/status"
	"github.com/buildkite/roko"
)

func (a *AgentWorker) runActionLoop(ctx context.Context, idleMon *idleMonitor, fromPingLoop, fromDebouncer <-chan actionMessage) error {
	a.logger.Debug("[runActionLoop] Starting")
	defer a.logger.Debug("[runActionLoop] Exiting")

	// Once this loop terminates, there's no point continuing the others,
	// because nothing remains to execute their actions.
	defer a.internalStop()

	ctx, setStat, _ := status.AddSimpleItem(ctx, "Action loop")
	defer setStat("🛑 Action loop stopped!")
	setStat("🏃 Starting...")

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
		if fromPingLoop == nil && fromDebouncer == nil {
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

		case m, open := <-fromDebouncer:
			if !open {
				fromDebouncer = nil
				continue
			}
			a.logger.Debug("[runActionLoop] Got action %q from streaming loop debouncer", m.action)
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

		// In cases where we need to disconnect, *don't* send on msg.errCh,
		// in order to force the <-a.stop branch in the other loops.
		// Otherwise, be sure to `close(msg.errCh)`!
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
			close(msg.errCh)
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
			close(msg.errCh)
			continue
		}

		setStat("💼 Accepting job")

		// Runs the job, only errors if something goes wrong
		if err := a.AcceptAndRunJob(ctx, msg.jobID, idleMon); err != nil {
			a.logger.Error("%v", err)
			setStat(fmt.Sprintf("✅ Finished job with error: %v", err))
			msg.errCh <- err // so the ping loop can do something special
			close(msg.errCh)
			continue
		}

		ranJob = true
		close(msg.errCh)
	}
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
