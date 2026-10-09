package clicommand

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/urfave/cli/v3"
)

func captureErrorTestApp() *cli.Command {
	cmd := *JobCaptureErrorCommand
	cmd.Flags = slices.Clone(cmd.Flags)
	// urfave flags retain parsing state, so each invocation needs fresh flags.
	for i, flag := range cmd.Flags {
		if flag, ok := flag.(*cli.StringFlag); ok {
			copy := *flag
			cmd.Flags[i] = &copy
		}
	}
	return &cli.Command{Commands: []*cli.Command{&cmd}}
}

func startCaptureErrorTestServer(t *testing.T, report func(context.Context, *jobapi.CapturedError) error) {
	t.Helper()
	socketPath, err := jobapi.NewSocketPath(os.TempDir())
	if err != nil {
		t.Fatalf("NewSocketPath() error = %v", err)
	}
	server, token, err := jobapi.NewServer(shell.TestingLogger{T: t}, socketPath, env.New(), replacer.NewMux(), jobapi.WithCapturedErrorReporter(report))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("server.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", socketPath)
	t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", token)
}

func TestJobCaptureErrorUsesLocalJobAPI(t *testing.T) {
	var reported *jobapi.CapturedError
	startCaptureErrorTestServer(t, func(_ context.Context, capturedError *jobapi.CapturedError) error {
		reported = capturedError
		return nil
	})

	for _, source := range []string{"flag", "env", "override"} {
		t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE", "")
		message := "Failed to pull \"image\"\nregistry denied access"
		args := []string{"buildkite-agent", "capture-error", "block.image_pull_failed", "--message", message}
		switch source {
		case "env":
			t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE", message)
			args = args[:3]
		case "override":
			t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE", "other message")
		}
		if err := captureErrorTestApp().Run(t.Context(), args); err != nil {
			t.Fatalf("%s: capture-error command error = %v", source, err)
		}
		if reported == nil || reported.Code != "block.image_pull_failed" || reported.Message != message {
			t.Fatalf("%s: reported = %+v, want original code and message", source, reported)
		}
	}
}

func TestJobCaptureErrorRejectsInvalidArgumentsBeforeTransport(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE", "")
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--message", "failure"}, "error code is required"},
		{[]string{"code", "extra", "--message", "failure"}, "error code is required"},
		{[]string{"code"}, "message"},
		{[]string{"code", "--message", " "}, "message must not be blank"},
		{[]string{"code", "--message", "failure", "--context", "{}"}, "flag provided but not defined: -context"},
	} {
		app := captureErrorTestApp()
		err := app.Run(t.Context(), append([]string{"buildkite-agent", "capture-error"}, test.args...))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("args %v: error = %v, want %q", test.args, err, test.want)
		}
	}
}

func TestJobCaptureErrorDoesNotFallbackWithoutLocalJobAPI(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	err := captureErrorTestApp().Run(t.Context(), []string{"buildkite-agent", "capture-error", "x", "--message", "raw customer diagnostic"})
	if err == nil || !strings.Contains(err.Error(), "Local Job API is required") {
		t.Fatalf("error = %v, want Local Job API required error", err)
	}
}
