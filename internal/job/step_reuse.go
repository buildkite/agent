package job

import (
	"context"
	"errors"
	"strconv"

	"github.com/buildkite/agent/v4/internal/reuse"
	"github.com/buildkite/agent/v4/internal/self"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
)

// stepReuseEnabled reports whether the control plane enabled step reuse for
// this job.
func (e *Executor) stepReuseEnabled() bool {
	return e.shell.Env.GetString("BUILDKITE_STEP_REUSE", "") == "true"
}

// restoreStepReuseInputs restores upstream step outputs into the checkout
// before the command phase. It returns an error with exit code
// reuse.ExitCodeInputUnavailable if an upstream output is unavailable, in
// which case the command must not run. Other failures are only logged.
func (e *Executor) restoreStepReuseInputs(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "step-reuse-restore", e.TracingBackend)
	var err error
	defer func() { tracetools.FinishWithError(span, err) }()

	e.shell.Headerf("Restoring step reuse inputs")
	runErr := e.shell.Command(self.Path(ctx), "reuse", "restore").Run(ctx)
	if runErr == nil {
		return nil
	}
	if shell.ExitCode(runErr) == reuse.ExitCodeInputUnavailable {
		err = &shell.ExitError{
			Code: reuse.ExitCodeInputUnavailable,
			Err:  errors.New("an upstream step reuse output is unavailable, so the command was not run"),
		}
		return err
	}
	e.shell.Warningf("Restoring step reuse inputs failed, continuing: %v", runErr)
	return nil
}

// saveStepReuseResult stores the step's outputs and reports its result for
// step reuse. Failures are only logged; step reuse never fails a job.
func (e *Executor) saveStepReuseResult(ctx context.Context, exitStatus int) {
	span, ctx := tracetools.StartSpanFromContext(ctx, "step-reuse-save", e.TracingBackend)
	defer span.End()

	e.shell.Headerf("Saving step reuse result")
	if err := e.shell.Command(self.Path(ctx), "reuse", "save", "--exit-status="+strconv.Itoa(exitStatus)).Run(ctx); err != nil {
		e.shell.Warningf("Saving step reuse result failed: %v", err)
	}
}
