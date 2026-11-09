package job

import (
	"bytes"
	"context"
	"io"
	"slices"
	"sync"

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
	trailer   *statusTrailer
	cancelled <-chan struct{} // closed when the job is cancelled
	done      []string        // records whose last report was done

	// afterCancel holds the records started after the job was cancelled,
	// such as the pre-exit hook. They weren't interrupted, so their errors
	// are errors.
	afterCancel map[string]bool
}

// statusTrailer sits in the logger's writer chain. It holds the agent's own
// program status reports and attaches them to the end of the next line the
// logger writes, so the invisible sequence never comes before a section header
// such as "~~~", which the log viewer only recognises at the start of a line.
type statusTrailer struct {
	w io.Writer

	mu      sync.Mutex
	pending []byte
}

// hold keeps p until the next suitable line is written.
func (t *statusTrailer) hold(p []byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = append(t.pending, p...)
}

// holder adapts hold to an io.Writer.
type holder struct{ t *statusTrailer }

func (h holder) Write(p []byte) (int, error) {
	h.t.hold(p)
	return len(p), nil
}

// Write writes p to the logger's stream, inserting any held reports before the
// terminator of the first complete line. A "^^^ +++" line is left exactly as
// it is, because it's a header marker on its own.
func (t *statusTrailer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pending) == 0 {
		return t.w.Write(p)
	}

	for start := 0; start < len(p); {
		nl := bytes.IndexByte(p[start:], '\n')
		if nl < 0 {
			break
		}
		end := start + nl // index of '\n'
		line := bytes.TrimSuffix(p[start:end], []byte("\r"))
		if string(line) == "^^^ +++" {
			start = end + 1
			continue
		}
		at := start + len(line)
		out := make([]byte, 0, len(p)+len(t.pending))
		out = append(out, p[:at]...)
		out = append(out, t.pending...)
		out = append(out, p[at:]...)
		t.pending = nil
		if _, err := t.w.Write(out); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return t.w.Write(p)
}

// Flush writes any held reports as they are.
func (t *statusTrailer) Flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.pending) == 0 {
		return nil
	}
	_, err := t.w.Write(t.pending)
	t.pending = nil
	return err
}

// newJobStatus returns a jobStatus that reports through t. cancelled is closed
// when the job is cancelled.
func newJobStatus(t *statusTrailer, cancelled <-chan struct{}) *jobStatus {
	return &jobStatus{r: programstatus.New(holder{t}), trailer: t, cancelled: cancelled, afterCancel: make(map[string]bool)}
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
// command's, leaving the result and anything that failed. Nothing is logged
// afterwards, so it also flushes the reports the trailer still holds.
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
	_ = s.trailer.Flush()
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
