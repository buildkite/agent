package agent

import (
	"context"
)

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
func (a *AgentWorker) runDebouncer(ctx context.Context, bat *baton, outCh chan<- actionMessage, inCh <-chan actionMessage) error {
	a.logger.Debugf("[runDebouncer] Starting")
	defer a.logger.Debugf("[runDebouncer] Exiting")

	// When the debouncer returns, close the output channel to let the next
	// loop know to stop listening to it.
	defer close(outCh)

	// We begin not running an action.
	actionInProgress := false

	// We begin holding the baton, ensure it is released when we exit.
	defer func() {
		a.logger.Debugf("[runDebouncer] Releasing the baton")
		bat.Release(actorDebouncer)
	}()

	// lastActionResult is closed when the action handler is done handling the
	// last action we sent.
	// It starts nil because at the beginning, there is no previous action.
	var lastActionResult chan error

	// pendingJobs holds jobs to start, when able. This normally holds no more
	// than 1 job ID, because the backend should never try to dispatch more jobs
	// before the existing ones have run, but handing this is basically no cost.
	var pendingJobs []string
	// pendingAction can be "disconnect", "pause", or "" (resume / next job).
	// Choosing the next action is the main function of the debouncer.
	var pendingAction string
	// pending is true when the action is pending, false otherwise
	var pending bool

	// lastActionWasJob is true when the most recently sent action included a
	// job to run. Used for disconnect-after-job, below.
	var lastActionWasJob bool

	// Is the stream healthy?
	// If so, take the baton (which blocks the ping loop).
	// If not, return the baton (unblocking the ping loop).
	// Returning the baton may have to wait for the current action to complete.
	healthy := true

	for {
		select {
		case <-a.stop:
			a.logger.Debugf("[runDebouncer] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debugf("[runDebouncer] Stopping due to context cancel")
			return ctx.Err()

		case <-iif(healthy, bat.Acquire()): // if the stream is healthy, take the baton if available
			bat.Acquired(actorDebouncer)
			a.logger.Debugf("[runDebouncer] Took the baton")
			// We now have the baton!
			// continue below to send any pending message, if able

		case msg, open := <-inCh: // streaming loop has produced an event
			if !open {
				a.logger.Debugf("[runDebouncer] Stopping due to input channel closing")
				return nil
			}

			healthy = !msg.unhealthy

			if !healthy {
				a.logger.Debugf("[runDebouncer] Streaming loop is unhealthy")

				// It is not healthy, so release the baton as soon as we can
				// (when the current action is done).
				if !actionInProgress {
					// We can release the baton now.
					a.logger.Debugf("[runDebouncer] Releasing the baton")
					bat.Release(actorDebouncer)
				}
				break // out of the select
			}

			// If nothing's pending, make the new message the pending action.
			if !pending {
				pending = true
				pendingAction = msg.action
				if msg.jobID != "" {
					pendingJobs = append(pendingJobs, msg.jobID)
				}
				break // out of the select
			}

			// Something is already pending; figure out how to debounce.

			// Apply disconnect ASAP.
			if msg.action == "disconnect" {
				pendingAction = "disconnect"
			}
			// Don't debounce over the top of an existing "disconnect".
			if pendingAction == "disconnect" {
				break // out of the select
			}

			// Replace the pending action with the new action, debouncing it.
			// This makes sense in all remaining cases (having dealt with
			// disconnect above):
			// pause  -> pause:  no change (probably shouldn't happen though)
			// pause  -> resume: resume as though we never received the pause
			// resume -> pause:  pause as though we never received the resume
			// resume -> resume: no change (probably a job dispatch)
			pendingAction = msg.action

			// If there's a job ID (msg.action should be ""), enqueue it to run
			// once resumed.
			if msg.jobID != "" {
				pendingJobs = append(pendingJobs, msg.jobID)
			}

			// continue below to send it

		case err := <-lastActionResult: // most recent action has completed
			a.logger.Debugf("[runDebouncer] Last action has completed")
			// Set the channel variable to nil so we don't spinloop.
			// (Operations on a nil channel block forever.)
			lastActionResult = nil
			actionInProgress = false

			// In disconnect-after-job mode, the action handler loop only
			// disconnects when it handles the next action after running a job
			// (this gives a pending "pause" the chance to keep the agent
			// alive). The ping loop pings the backend immediately after a job,
			// so it produces that next action promptly, but the stream might
			// not deliver another message for a while. So if a job just ran
			// and nothing else is pending, synthesise an idle action to let
			// the action handler loop disconnect promptly.
			if a.agentConfiguration.DisconnectAfterJob && lastActionWasJob && err == nil && !pending {
				a.logger.Debugf("[runDebouncer] Job ran in disconnect-after-job mode; enqueueing a synthetic idle action")
				pending = true
				pendingAction = ""
			}

			// continue below to send a pending message
		}

		// If we're healthy, have the baton, there's no action in progress,
		// and there's a pending action, then send the pending action.
		if !healthy || !bat.HeldBy(actorDebouncer) || actionInProgress || !pending {
			continue
		}

		lastActionResult = make(chan error)
		newMsg := actionMessage{
			action: pendingAction,
			errCh:  lastActionResult,
		}
		// If we're resumed, include the next job if there is one.
		if pendingAction == "" && len(pendingJobs) > 0 {
			newMsg.jobID = pendingJobs[0]
		}
		a.logger.Debugf("[runDebouncer] Sending action %q, next jobID %q", newMsg.action, newMsg.jobID)
		select {
		case outCh <- newMsg:
			// sent!
			pending = false
			lastActionWasJob = newMsg.jobID != ""
			if newMsg.jobID != "" {
				pendingJobs = pendingJobs[1:]
				pending = len(pendingJobs) > 0
			}
			actionInProgress = true
		case <-a.stop:
			a.logger.Debugf("[runDebouncer] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debugf("[runDebouncer] Stopping due to context cancel")
			return ctx.Err()
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
