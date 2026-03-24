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
	a.logger.Debug("[runDebouncer] Starting")
	defer a.logger.Debug("[runDebouncer] Exiting")

	// When the debouncer returns, close the output channel to let the next
	// loop know to stop listening to it.
	defer close(outCh)

	// We begin not running an action.
	actionInProgress := false

	// We begin holding the baton, ensure it is released when we exit.
	defer func() {
		a.logger.Debug("[runDebouncer] Releasing the baton")
		bat.Release(actorDebouncer)
	}()

	// lastActionResult is closed when the action handler is done handling the
	// last action we sent.
	// It starts nil because at the beginning, there is no previous action.
	var lastActionResult chan error

	// pending is the next message to send, when able.
	var pending *actionMessage

	// Is the stream healthy?
	// If so, take the baton (which blocks the ping loop).
	// If not, return the baton (unblocking the ping loop).
	// Returning the baton may have to wait for the current action to complete.
	healthy := true

	for {
		select {
		case <-a.stop:
			a.logger.Debug("[runDebouncer] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runDebouncer] Stopping due to context cancel")
			return ctx.Err()

		case <-iif(healthy, bat.Acquire()): // if the stream is healthy, take the baton if available
			bat.Acquired(actorDebouncer)
			a.logger.Debug("[runDebouncer] Took the baton")
			// We now have the baton!
			// continue below to send any pending message, if able

		case msg, open := <-inCh: // streaming loop has produced an event
			if !open {
				a.logger.Debug("[runDebouncer] Stopping due to input channel closing")
				return nil
			}

			healthy = !msg.unhealthy

			if !healthy {
				a.logger.Debug("[runDebouncer] Streaming loop is unhealthy")

				// It is not healthy, so release the baton as soon as we can
				// (when the current action is done).
				if !actionInProgress {
					// We can release the baton now.
					a.logger.Debug("[runDebouncer] Releasing the baton")
					bat.Release(actorDebouncer)
				}
				break // out of the select
			}

			// The next message to send is, currently, always the most recent
			// healthy message.
			pending = &msg

			// continue below to send it

		case <-lastActionResult: // most recent action has completed
			a.logger.Debug("[runDebouncer] Last action has completed")
			// Set the channel variable to nil so we don't spinloop.
			// (Operations on a nil channel block forever.)
			lastActionResult = nil
			actionInProgress = false

			// continue below to send a pending message
		}

		// If we're healthy, have the baton, there's no action in progress,
		// and there's a pending message, then send that message.
		if !healthy || !bat.HeldBy(actorDebouncer) || actionInProgress || pending == nil {
			continue
		}
		a.logger.Debug("[runDebouncer] Sending action %q, jobID %q", pending.action, pending.jobID)
		lastActionResult = make(chan error)
		pending.errCh = lastActionResult
		select {
		case outCh <- *pending:
			// sent!
			pending = nil
			actionInProgress = true
		case <-a.stop:
			a.logger.Debug("[runDebouncer] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runDebouncer] Stopping due to context cancel")
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
