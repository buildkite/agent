package job

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/programstatus"
	"github.com/buildkite/agent/v4/internal/shell"
	"golang.org/x/term"
)

// commandStatusID is the record for the step's command. It's left behind
// when the job ends, so whoever is watching can see how the job finished.
const commandStatusID = "command"

// jobStatus reports the job's phases and hooks as OSC 7501 program status
// records, such as hook/plugin/docker/pre-command. The records have ids, so
// programs the job runs can report on the root record without overwriting
// them. A nil jobStatus does nothing.
type jobStatus struct {
	r    *programstatus.Reporter
	done []string // records whose last report was done
}

// newJobStatus returns a jobStatus that writes to w, or nil if the
// program-status experiment or BUILDKITE_PROGRAM_STATUS turn reports off.
func newJobStatus(ctx context.Context, w io.Writer) *jobStatus {
	if !experiments.IsEnabled(ctx, experiments.ProgramStatus) ||
		!programstatus.Enabled(os.LookupEnv, term.IsTerminal(int(os.Stdout.Fd()))) {
		return nil
	}
	return &jobStatus{r: programstatus.New(w)}
}

// start reports that the record id is running.
func (s *jobStatus) start(id, msg string) {
	if s != nil {
		s.r.Working(id, -1, msg)
	}
}

// finish reports how the record id ended. what describes it in an error
// message, such as "The command".
func (s *jobStatus) finish(id string, err error, what string) {
	if s == nil {
		return
	}
	errorMsg := what + " failed"
	if shell.IsExitError(err) {
		errorMsg = fmt.Sprintf("%s exited with status %d", what, shell.ExitCode(err))
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
	// Plugin names only contain [a-z0-9-], but an id segment can be at most
	// 32 bytes long.
	name := hookCfg.PluginName
	if len(name) > 32 {
		name = name[:32]
	}
	if name == "" {
		name = "unknown"
	}
	return "hook/plugin/" + name + "/" + hookCfg.Name
}
