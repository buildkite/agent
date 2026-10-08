package job

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

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
	path, inCheckout := e.hookDisplayPath(hookCfg)
	// Say where the hook lives when it is not in the repository being built.
	where := ""
	if !inCheckout {
		where = map[string]string{
			HookScopeAgent:  " It is installed on the agent, not in the repository.",
			HookScopePlugin: " It is part of the plugin, not the repository.",
		}[hookCfg.Scope]
	}
	message := fmt.Sprintf("The %s hook at %s could not be run: %v.%s", hookName, path, err, where)
	switch exit, ok := describeExit(err); {
	case ok:
		message = fmt.Sprintf("The %s hook at %s %s.%s", hookName, path, exit, where)
	case errors.Is(err, syscall.ENOENT) && couldNotStart(err):
		// The hook could not be started. The error names the agent's temporary
		// wrapper script, not what is missing, which is usually the
		// interpreter on the hook's #! line.
		message = fmt.Sprintf("The %s hook at %s could not be run because a program it needs was not found, usually the interpreter on its #! line, such as bash.%s", hookName, path, where)
	}
	message = jobapi.CapturedErrorMessage(message, fmt.Sprintf("The %s hook failed.%s", hookName, where))
	captureJobError(ctx, e.shell, "hook_failed", e.withRecentOutput(message))
}

// couldNotStart reports whether err is from starting a process, such as
// "fork/exec <path>: no such file or directory".
func couldNotStart(err error) bool {
	pathErr := new(os.PathError)
	return errors.As(err, &pathErr) && strings.HasSuffix(pathErr.Op, "exec")
}

// hookDisplayPath shows hooks in the checkout, including those of vendored
// plugins, relative to the checkout, and other plugin hooks relative to the
// plugins directory, which is how a pipeline author finds them. It reports
// whether the hook is in the checkout.
func (e *Executor) hookDisplayPath(hookCfg HookConfig) (string, bool) {
	if rel, ok := relativePath(e.shell.Env.GetString("BUILDKITE_BUILD_CHECKOUT_PATH", ""), hookCfg.Path); ok {
		return rel, true
	}
	if hookCfg.Scope == HookScopePlugin {
		if rel, ok := relativePath(e.PluginsPath, hookCfg.Path); ok {
			return rel, false
		}
	}
	return hookCfg.Path, false
}

// relativePath returns path relative to dir, if it is inside dir.
func relativePath(dir, path string) (string, bool) {
	if dir == "" {
		return "", false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// captureCommandError reports a command that failed or could not be run,
// whether run by default or by a command hook, with the end of its output.
func (e *Executor) captureCommandError(ctx context.Context, err error) {
	// A refused repository command hook was reported when it was refused.
	if errors.As(err, new(localHookRefusedError)) {
		return
	}
	// Quote short, single-line commands. Longer scripts are in the step.
	command := strings.TrimSpace(e.Command)
	quoted := command != "" && utf8.RuneCountInString(command) <= 200 && !strings.Contains(command, "\n")
	subject := "The step's command"
	if quoted {
		subject = fmt.Sprintf("The command `%s`", command)
	}
	switch {
	case shell.IsExitError(err):
		if e.commandHook != "" {
			subject = e.commandHook + ", which runs instead of the step's command,"
		}
		exit, _ := describeExit(err)
		message := subject + " " + exit + "."
		if strings.HasSuffix(exit, "SIGKILL") {
			message += " The operating system may have killed it for using too much memory."
		}
		captureJobError(ctx, e.shell, "command_failed", e.withRecentOutput(message))
	case e.commandHook != "":
		// The hook may have run and failed afterwards, such as while the agent
		// read the environment changes it made.
		captureJobError(ctx, e.shell, "command_hook_failed", jobapi.CapturedErrorMessage(
			fmt.Sprintf("%s, which runs instead of the step's command, failed: %v.", e.commandHook, err),
			e.commandHook+", which runs instead of the step's command, failed."))
	case errors.Is(err, errNoCommand):
		captureJobError(ctx, e.shell, "command_missing", err.Error())
	case errors.Is(err, errCommandEvalDisabled), errors.Is(err, errCommandOutsideRepository):
		message := err.Error()
		if quoted {
			message += fmt.Sprintf(". The step's command is `%s`.", command)
		}
		captureJobError(ctx, e.shell, "command_eval_disabled", message)
	default:
		captureJobError(ctx, e.shell, "command_not_run", jobapi.CapturedErrorMessage(
			fmt.Sprintf("%s could not be run: %v.", subject, err), subject+" could not be run."))
	}
}

// withRecentOutput appends the end of the redacted output from the hook,
// command, or plugin checkout that just failed, using whatever room the
// message budget leaves after the summary.
func (e *Executor) withRecentOutput(summary string) string {
	// Release output a redactor is holding back in case it starts a secret.
	_ = e.redactors.Flush()
	const heading = "\n\nLast lines of output:\n"
	recent := e.outputTail.tail(jobapi.MaxCapturedErrorDetail - utf8.RuneCountInString(summary) - utf8.RuneCountInString(heading))
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
