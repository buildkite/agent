package job

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/jobapi"
)

// agentErrorCaptureEnabled reports whether the job opted in to automatic
// reports of agent-observed failures, and the Local Job API can deliver them.
func agentErrorCaptureEnabled(environ *env.Environment) bool {
	return environ.GetString("BUILDKITE_CAPTURE_AGENT_ERRORS", "") == "true" &&
		environ.GetString("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "") == "true"
}

// captureJobError reports a failure the agent observed while running the job,
// such as a failed hook. Messages are plain text that explains the failure
// and what to change. The Local Job API redacts registered secrets.
// Delivery is best-effort and never changes the job's outcome.
func captureJobError(ctx context.Context, sh *shell.Shell, code, message string) {
	// Failures after cancellation are consequences of it, not separate errors.
	if !agentErrorCaptureEnabled(sh.Env) || sh.Env.GetString("BUILDKITE_JOB_CANCELLED", "") == "true" {
		return
	}
	now := time.Now()
	deliverError(ctx, sh, jobapi.CapturedError{Code: code, Message: message, Timestamp: &now})
}

// captureHookError reports a hook that failed or could not be run, with the
// end of its output, so the report explains the failure without the job log.
func (e *Executor) captureHookError(ctx context.Context, hookCfg HookConfig, hookName string, err error) {
	if err == nil {
		return
	}
	// Say where the hook lives when it is not in the repository being built.
	where := map[string]string{
		HookScopeAgent:  " It is installed on the agent, not in the repository.",
		HookScopePlugin: " It is part of the plugin, not the repository.",
	}[hookCfg.Scope]
	path := e.hookDisplayPath(hookCfg)
	message := fmt.Sprintf("The %s hook at %s could not be run: %v.%s", hookName, path, err, where)
	if exit, ok := describeExit(err); ok {
		message = fmt.Sprintf("The %s hook at %s %s.%s", hookName, path, exit, where)
	}
	captureJobError(ctx, e.shell, "hook_failed", e.withRecentOutput(message))
}

// hookDisplayPath shows repository hooks relative to the checkout and plugin
// hooks relative to the plugin, which is how a pipeline author finds them.
func (e *Executor) hookDisplayPath(hookCfg HookConfig) string {
	switch hookCfg.Scope {
	case HookScopeRepository:
		checkout := e.shell.Env.GetString("BUILDKITE_BUILD_CHECKOUT_PATH", "")
		if rel, err := filepath.Rel(checkout, hookCfg.Path); checkout != "" && err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	case HookScopePlugin:
		return "hooks/" + filepath.Base(hookCfg.Path)
	}
	return hookCfg.Path
}

// withRecentOutput appends the end of the redacted output from the hook,
// command, or plugin checkout that just failed, using whatever room the
// message budget leaves after the summary.
func (e *Executor) withRecentOutput(summary string) string {
	// Release output a redactor is holding back in case it starts a secret.
	_ = e.redactors.Flush()
	const heading = "\n\nLast lines of output:\n"
	recent := e.outputTail.tail(jobapi.MaxCapturedErrorDetail - len(summary) - len(heading))
	if recent == "" {
		return summary
	}
	return summary + heading + recent
}

// describeExit describes how a process that exited unsuccessfully ended, such
// as "exited with status 3" or "was terminated by signal SIGKILL". It reports
// whether err was such an exit.
func describeExit(err error) (string, bool) {
	if !shell.IsExitError(err) {
		return "", false
	}
	if exitErr := new(exec.ExitError); errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return "was terminated by signal " + process.SignalString(status.Signal()), true
		}
	}
	return fmt.Sprintf("exited with status %d", shell.ExitCode(err)), true
}

func deliverError(ctx context.Context, sh *shell.Shell, report jobapi.CapturedError) {
	// Failures after cancellation are consequences of it, not separate errors.
	if ctx.Err() != nil || sh.Env.GetString("BUILDKITE_JOB_CANCELLED", "") == "true" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client, err := jobapi.NewClient(ctx, sh.Env.GetString("BUILDKITE_AGENT_JOB_API_SOCKET", ""), sh.Env.GetString("BUILDKITE_AGENT_JOB_API_TOKEN", ""))
	if err == nil {
		_, err = client.CaptureError(ctx, &report)
	}
	if err != nil {
		// Transport errors can contain upstream response bodies or socket paths.
		sh.Warningf("Could not capture job error %q", report.Code)
	}
}
