package agent

import (
	"cmp"
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
func (a *AgentWorker) runDebouncer(ctx context.Context, baton *BatonHolder, outCh chan<- actionMessage, inCh <-chan actionMessage) error {
	a.logger.Debug("[runDebouncer] Starting")
	defer a.logger.Debug("[runDebouncer] Exiting")

	// When the debouncer returns, close the output channel to let the next
	// loop know to stop listening to it.
	defer close(outCh)

	// Give up the baton when we're no longer running so that the regular
	// ping loop can have a go (if it hasn't also ended).
	// Release is idempotent, so this is safe even if already released.
	defer baton.Release()

	// This "closed" channel is used for a little hack below.
	// `<-cmp.Or(lastActionResult, closed)` will receive from:
	// - lastActionResult, if lastActionResult is not nil, or
	// - closed (i.e. immediately) if lastActionResult is nil.
	closed := make(chan error)
	close(closed)

	// lastActionResult is closed when the action handler is done handling the
	// last action we sent.
	// It starts nil because at the beginning, there is no previous action.
	var lastActionResult chan error

	// nextAction and nextJobID are the next action that should be sent.
	var nextAction string
	var nextJobID string

	// pending tracks whether there is an action to send.
	pending := false

	// Is the stream healthy?
	// If so, take the baton (which blocks the ping loop).
	// If not, release the baton (unblocking the ping loop).
	// Releasing may have to wait for the current action to complete.
	healthy := true

	for {
		select {
		case <-a.stop:
			a.logger.Debug("[runDebouncer] Stopping due to agent stop")
			return nil
		case <-ctx.Done():
			a.logger.Debug("[runDebouncer] Stopping due to context cancel")
			return ctx.Err()

		case <-iif(healthy, baton.Acquire()): // if the stream is healthy, acquire the baton
			baton.Acquired()
			a.logger.Debug("[runDebouncer] Acquired the baton")

			// Do we have a pending message?
			if !pending {
				a.logger.Debug("[runDebouncer] No pending action to send")
				continue
			}
			// Yes, there is something to send.
			a.logger.Debug("[runDebouncer] Sending pending action %q, jobID %q", nextAction, nextJobID)
			pending = false
			lastActionResult = make(chan error)
			msg := actionMessage{
				action: nextAction,
				jobID:  nextJobID,
				errCh:  lastActionResult,
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
				a.logger.Debug("[runDebouncer] Streaming loop is unhealthy")

				// It is not, so release the baton as soon as we can (when the
				// current action is done).
				select {
				case <-cmp.Or(lastActionResult, closed):
					// The last action is done, or there is no last action.
					// We're unhealthy, so release the baton now.
					baton.Release()

				default:
					// No, wait until the action is complete to release.
					// (Logic is in <-lastActionResult branch.)
				}
				continue
			}

			// Yes, we're healthy. Do we have the baton?
			if !baton.Held() {
				// No, the ping loop currently holds the baton.
				// Debounce messages until we have it.
				a.logger.Debug("[runDebouncer] Debouncing (action %q, jobID %q) while waiting for baton", msg.action, msg.jobID)
				nextAction = msg.action
				nextJobID = msg.jobID
				pending = true
				continue
			}

			// Yes, we have the baton.
			// Can we send this message right away?
			select {
			case <-cmp.Or(lastActionResult, closed):
				// The last action is done, or there is no last action.
				// Either way, we're clear to pass this message on to the
				// action handler right away.
				a.logger.Debug("[runDebouncer] Forwarding action immediately (action %q, jobID %q)", msg.action, msg.jobID)
				pending = false
				lastActionResult = make(chan error)
				msg.errCh = lastActionResult
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
				a.logger.Debug("[runDebouncer] Debouncing (action %q, jobID %q) while waiting for current action to complete", msg.action, msg.jobID)
				nextAction = msg.action
				nextJobID = msg.jobID
				pending = true
			}

		case <-lastActionResult: // most recent action has completed
			a.logger.Debug("[runDebouncer] Last action has completed")
			// First, set it to nil so we don't come back here right
			// away. (Operations on a nil channel block forever.)
			lastActionResult = nil
			// Is the streaming side healthy?
			if !healthy {
				// No, we're not healthy. If we have the baton, now is the
				// time to give it up, falling back to the ping loop.
				a.logger.Debug("[runDebouncer] Streaming loop wasn't healthy earlier")
				baton.Release()
				continue
			}
			// Yes, we're healthy. Is there a pending message to send?
			if !pending {
				// Nothing waiting to be sent.
				a.logger.Debug("[runDebouncer] No pending action to send")

				continue
			}
			// Yes, there is something to send. Let's send it!
			a.logger.Debug("[runDebouncer] Sending pending action %q, jobID %q", nextAction, nextJobID)
			pending = false
			lastActionResult = make(chan error)
			msg := actionMessage{
				action: nextAction,
				jobID:  nextJobID,
				errCh:  lastActionResult,
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
