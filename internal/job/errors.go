package job

import (
	"context"
	"errors"
)

// FormatJobError returns a human-friendly description of err for inclusion in
// the job log or agent stderr.
//
// Bare context errors (`context canceled`, `context deadline exceeded`) leak
// out of various code paths -- the kubernetes runner returns ctx.Err()
// directly, and shell errors that wrap a cancelled context can otherwise
// surface a Go default string. This helper normalises those cases so the user
// sees something they can act on.
//
// If err is nil, the returned string is empty.
func FormatJobError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		// context.DeadlineExceeded only fires when something explicitly set a
		// deadline -- e.g. the in-process signal grace period in
		// WithGracePeriod. From the user's perspective, that means a timeout
		// was reached.
		return "job timed out"
	case errors.Is(err, context.Canceled):
		// We can't tell from the agent side whether this came from a
		// server-issued cancel (UI button or job-level timeout, both of which
		// transition the job to the same `canceling` state) or from an
		// operator interrupting the agent. "cancelled" is the honest umbrella
		// term that doesn't lie about the cause.
		return "job cancelled"
	default:
		return err.Error()
	}
}
