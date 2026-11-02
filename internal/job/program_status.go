package job

import (
	"context"
	"io"
	"slices"

	"github.com/buildkite/agent/v4/internal/programstatus"
)

// commandStatusID is the record for the step's command. It's left behind
// when the job ends, so whoever is watching can see how the job finished.
const commandStatusID = "command"

// jobStatus reports the job's phases and hooks as OSC 7501 program status
// records, such as hook/plugin/docker/pre-command. The records have ids, so
// programs the job runs can report on the root record without overwriting
// them. A nil jobStatus does nothing.
type jobStatus struct {
	r         *programstatus.Reporter
	cancelled <-chan struct{} // closed when the job is cancelled
	done      []string        // records whose last report was done

	// afterCancel holds the records started after the job was cancelled,
	// such as the pre-exit hook. They weren't interrupted, so their errors
	// are errors.
	afterCancel map[string]bool
}

// newJobStatus returns a jobStatus that writes to w. cancelled is closed when
// the job is cancelled.
func newJobStatus(w io.Writer, cancelled <-chan struct{}) *jobStatus {
	return &jobStatus{r: programstatus.New(w), cancelled: cancelled, afterCancel: make(map[string]bool)}
}

func (s *jobStatus) isCancelled() bool {
	select {
	case <-s.cancelled:
		return true
	default:
		return false
	}
}

// start reports that the record id is running.
func (s *jobStatus) start(id, msg string) {
	if s == nil {
		return
	}
	s.afterCancel[id] = s.isCancelled()
	s.r.Working(id, -1, msg)
}

// finish reports how the record id ended. what describes it in an error
// message, such as "The command". A record that fails because the job was
// cancelled while it ran ends idle, as the spec asks of cancelled programs.
func (s *jobStatus) finish(id string, err error, what string) {
	if s == nil {
		return
	}
	if err != nil && !s.afterCancel[id] && s.isCancelled() {
		// Cancelling interrupts the process, so err is usually its exit
		// status rather than context.Canceled.
		err = context.Canceled
	}
	errorMsg := what + " failed"
	if exit, ok := describeExit(err); ok {
		errorMsg = what + " " + exit
	}
	s.r.Finish(id, err, "", errorMsg)

	s.done = slices.DeleteFunc(s.done, func(d string) bool { return d == id })
	if err == nil {
		s.done = append(s.done, id)
	}
}

// clearDone removes the records that finished successfully, except the
// command's, leaving the result and anything that failed.
func (s *jobStatus) clearDone() {
	if s == nil {
		return
	}
	for _, id := range s.done {
		if id != commandStatusID {
			s.r.Clear(id)
		}
	}
	s.done = nil
}

// hookStatusID returns the record id for a hook, such as hook/agent/environment
// or hook/plugin/docker/pre-command.
func hookStatusID(hookCfg HookConfig) string {
	if hookCfg.Scope != HookScopePlugin {
		return "hook/" + hookCfg.Scope + "/" + hookCfg.Name
	}
	// Short plugin names only contain [a-z0-9-], but an id segment can be at
	// most 32 bytes long.
	name := hookCfg.PluginID
	if len(name) > 32 {
		name = name[:32]
	}
	if name == "" {
		name = "unknown"
	}
	return "hook/plugin/" + name + "/" + hookCfg.Name
}
