package job

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/shell"
)

// agentErrorCaptureServer returns an executor whose Local Job API forwards
// captured errors to reports, with agent error capture enabled.
func agentErrorCaptureServer(t *testing.T) (context.Context, *Executor, <-chan api.JobCapturedError) {
	t.Helper()
	ctx, e, reports := gitErrorCaptureServer(t, false, http.StatusCreated)
	e.shell.Env.Set("BUILDKITE_CAPTURE_AGENT_ERRORS", "true")
	return ctx, e, reports
}

func TestCaptureJobErrorOptIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, optIn, capability string
		want                    bool
	}{
		{"unset", "", "true", false},
		{"disabled", "false", "true", false},
		{"invalid", "yes", "true", false},
		{"enabled", "true", "true", true},
		{"API unavailable", "true", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, e, reports := agentErrorCaptureServer(t)
			e.shell.Env.Set("BUILDKITE_CAPTURE_AGENT_ERRORS", tc.optIn)
			e.shell.Env.Set("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", tc.capability)

			captureJobError(ctx, e.shell, "hook_failed", "The hook failed.")

			if got := len(reports) == 1; got != tc.want {
				t.Fatalf("captured = %t, want %t", got, tc.want)
			}
			if tc.want {
				report := <-reports
				if report.Code != "hook_failed" || report.Message != "The hook failed." {
					t.Errorf("report = %+v", report)
				}
			}
		})
	}
}

func TestCaptureJobErrorSkipsCancelledJobs(t *testing.T) {
	t.Parallel()
	ctx, e, reports := agentErrorCaptureServer(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()

	captureJobError(ctx, e.shell, "hook_failed", "The hook failed.")

	if len(reports) != 0 {
		t.Fatalf("reports = %d, want none after cancellation", len(reports))
	}
}

func TestCaptureJobErrorSkipsJobsMarkedCancelled(t *testing.T) {
	t.Parallel()
	// Hooks such as post-command keep running during the grace period after
	// the executor is cancelled, which marks the job as cancelled.
	ctx, e, reports := agentErrorCaptureServer(t)
	e.shell.Env.Set("BUILDKITE_JOB_CANCELLED", "true")

	captureJobError(ctx, e.shell, "hook_failed", "The hook failed.")

	if len(reports) != 0 {
		t.Fatalf("reports = %d, want none for a cancelled job", len(reports))
	}
}

func TestHookDisplayPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	checkout := filepath.Join(root, "builds", "pipeline")
	plugins := filepath.Join(root, "plugins")
	e := &Executor{shell: shell.NewTestShell(t)}
	e.PluginsPath = plugins
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkout)

	for _, test := range []struct {
		name, scope, path, want string
		inCheckout              bool
	}{
		{"repository hook", HookScopeRepository, filepath.Join(checkout, ".buildkite", "hooks", "pre-command"), ".buildkite/hooks/pre-command", true},
		{"vendored plugin", HookScopePlugin, filepath.Join(checkout, ".buildkite", "plugins", "lint", "hooks", "command"), ".buildkite/plugins/lint/hooks/command", true},
		{"plugin", HookScopePlugin, filepath.Join(plugins, "github-com-acme-lint-buildkite-plugin", "custom-hooks", "command"), "github-com-acme-lint-buildkite-plugin/custom-hooks/command", false},
		{"agent hook", HookScopeAgent, filepath.Join(root, "hooks", "environment"), filepath.Join(root, "hooks", "environment"), false},
		{"sibling of the checkout", HookScopeRepository, checkout + "-other/hook", checkout + "-other/hook", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, inCheckout := e.hookDisplayPath(HookConfig{Scope: test.scope, Path: test.path})
			if path != test.want || inCheckout != test.inCheckout {
				t.Errorf("hookDisplayPath() = %q, %t; want %q, %t", path, inCheckout, test.want, test.inCheckout)
			}
		})
	}
}

func TestDescribeExitSeesThroughHookErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are POSIX")
	}
	t.Parallel()
	err := exec.Command("sh", "-c", "kill -KILL $$").Run()
	wrapped := &shell.ExitError{Code: -1, Err: hookExitError{message: "the hook exited with status -1", err: err}}
	if got, ok := describeExit(wrapped); !ok || got != "was terminated by signal SIGKILL" {
		t.Errorf("describeExit() = %q, %t; want the signal", got, ok)
	}
}

func TestCaptureHookErrorMissingInterpreter(t *testing.T) {
	t.Parallel()
	ctx, e, reports := agentErrorCaptureServer(t)
	var err error = &os.PathError{Op: "fork/exec", Path: "/tmp/buildkite-agent-hook-wrapper/hook", Err: syscall.ENOENT}
	e.captureHookError(ctx, HookConfig{Scope: HookScopeAgent, Path: "/etc/buildkite-agent/hooks/environment"}, "agent environment", err)
	report := <-reports
	if want := "The agent environment hook at /etc/buildkite-agent/hooks/environment could not be run because a program it needs was not found, usually the interpreter on its #! line, such as bash. It is installed on the agent, not in the repository."; report.Message != want {
		t.Errorf("message = %q, want %q", report.Message, want)
	}

	// A missing file after the hook ran is not a missing interpreter.
	err = fmt.Errorf("failed to get environment: %w", &os.PathError{Op: "open", Path: "/tmp/env", Err: syscall.ENOENT})
	e.captureHookError(ctx, HookConfig{Scope: HookScopeAgent, Path: "/etc/buildkite-agent/hooks/environment"}, "agent environment", err)
	if report := <-reports; strings.Contains(report.Message, "interpreter") {
		t.Errorf("message = %q, want the error rather than a missing interpreter", report.Message)
	}
}
