// Package programstatus sends OSC 7501 program status reports, which tell a
// terminal (or the Buildkite agent running the job) what a program is doing.
//
// See https://www.superlogical.com/rex/docs/build/program-status
package programstatus

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"

	"github.com/buildkite/osc7501"
	"golang.org/x/term"
)

// App identifies the agent in its reports.
const App = "buildkite-agent"

// Reporter sends program status reports. Reporting is best-effort, so its
// methods don't return errors. A nil Reporter does nothing.
type Reporter struct {
	r *osc7501.Reporter

	// OnReport receives each successfully written report. Set it before reporting.
	OnReport func(osc7501.Report)
}

// New returns a Reporter that writes reports to w.
func New(w io.Writer) *Reporter {
	return &Reporter{r: osc7501.NewReporter(w, App)}
}

// FromEnvironment returns a Reporter that writes to stderr if reports are
// enabled, or nil if they are not.
func FromEnvironment() *Reporter {
	if !Enabled(os.LookupEnv, term.IsTerminal(int(os.Stderr.Fd()))) {
		return nil
	}
	return New(os.Stderr)
}

// Enabled reports whether to send reports, using the same rules as the
// Buildkite CLI (bk). BUILDKITE_PROGRAM_STATUS turns reports on or off when
// it's set to a boolean. Otherwise, they're only sent to a terminal, outside
// CI.
func Enabled(lookupEnv func(string) (string, bool), isTerminal bool) bool {
	if v, ok := lookupEnv("BUILDKITE_PROGRAM_STATUS"); ok {
		if enabled, err := strconv.ParseBool(v); err == nil {
			return enabled
		}
	}
	for _, name := range []string{"CI", "BUILDKITE"} {
		if v, _ := lookupEnv(name); v != "" {
			return false
		}
	}
	termType, _ := lookupEnv("TERM")
	return isTerminal && termType != "dumb"
}

// Working reports that the record id is running. A negative progress means
// it's unknown.
func (r *Reporter) Working(id string, progress int, msg string) {
	if r != nil {
		r.report(osc7501.Report{State: osc7501.StateWorking, ID: id, Progress: progress, Msg: msg})
	}
}

// Finish reports the record id as done if err is nil, idle if it was
// cancelled, and failed otherwise.
func (r *Reporter) Finish(id string, err error, doneMsg, errorMsg string) {
	if r == nil {
		return
	}
	switch {
	case err == nil:
		r.report(osc7501.Report{State: osc7501.StateDone, ID: id, Progress: -1, Msg: doneMsg})
	case errors.Is(err, context.Canceled):
		r.report(osc7501.Report{State: osc7501.StateIdle, ID: id, Progress: -1})
	default:
		r.report(osc7501.Report{State: osc7501.StateError, ID: id, Progress: -1, Msg: errorMsg})
	}
}

// Clear removes the record id and the records beneath it.
func (r *Reporter) Clear(id string) {
	if r != nil {
		r.report(osc7501.Report{State: osc7501.StateClear, ID: id, Progress: -1})
	}
}

func (r *Reporter) report(rep osc7501.Report) {
	rep.App = App
	if err := r.r.Report(rep); err == nil && r.OnReport != nil {
		r.OnReport(rep)
	}
}
