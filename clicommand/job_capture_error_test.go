package clicommand

import (
	"context"
	"encoding/json"
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

func TestJobCaptureErrorUsesLocalJobAPI(t *testing.T) {
	socketPath, err := jobapi.NewSocketPath(os.TempDir())
	if err != nil {
		t.Fatalf("NewSocketPath() error = %v", err)
	}
	var reported *jobapi.CapturedError
	server, token, err := jobapi.NewServer(shell.TestingLogger{T: t}, socketPath, env.New(), replacer.NewMux(), jobapi.WithCapturedErrorReporter(func(_ context.Context, capturedError *jobapi.CapturedError) error {
		reported = capturedError
		return nil
	}))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("server.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", socketPath)
	t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", token)

	for _, source := range []string{"omitted", "empty", "inline", "stdin", "env", "override"} {
		t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE", "")
		t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_CONTEXT", "")
		app := captureErrorTestApp()
		// Unselected stdin must not be consumed, even if it contains invalid JSON.
		app.Reader = strings.NewReader("not JSON")
		message := "Failed to pull \"image\"\nregistry denied access"
		args := []string{"buildkite-agent", "capture-error", "block.image_pull_failed", "--message", message}
		input := "{\n\"id\":9007199254740993,\n\"image\":\"example:latest\"\n}\n"
		switch source {
		case "empty":
			t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_CONTEXT", input)
			args = append(args, "--context", "")
		case "inline":
			args = append(args, "--context", input)
		case "stdin":
			args = append(args, "--context", "-")
			app.Reader = strings.NewReader(input)
		case "env":
			t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE", message)
			t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_CONTEXT", input)
			args = args[:3]
		case "override":
			t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE", "other message")
			t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_CONTEXT", "not JSON")
			args = append(args, "--context", input)
		}
		if err := app.Run(t.Context(), args); err != nil {
			t.Fatalf("capture-error command error = %v", err)
		}
		if reported == nil || reported.Code != "block.image_pull_failed" || reported.Message != message {
			t.Fatalf("reported = %+v, want original code and message", reported)
		}
		if source != "omitted" && source != "empty" {
			if reported.Context["id"] != json.Number("9007199254740993") || reported.Context["image"] != "example:latest" {
				t.Errorf("context = %v, want exact supplied values", reported.Context)
			}
		} else if reported.Context != nil {
			t.Errorf("context = %v, want omitted", reported.Context)
		}
	}
}

func TestJobCaptureErrorRejectsInvalidArgumentsBeforeTransport(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_MESSAGE", "")
	t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_CONTEXT", "")
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--message", "failure"}, "error code is required"},
		{[]string{"code", "extra", "--message", "failure"}, "error code is required"},
		{[]string{"code"}, "message"},
		{[]string{"code", "--message", " "}, "message must not be blank"},
	} {
		app := captureErrorTestApp()
		err := app.Run(t.Context(), append([]string{"buildkite-agent", "capture-error"}, test.args...))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("args %v: error = %v, want %q", test.args, err, test.want)
		}
	}
	for _, input := range []string{"{", "null", "[]", "{} {}", ""} {
		for _, source := range []string{"inline", "stdin", "env"} {
			if input == "" && source != "stdin" {
				continue // Empty optional context is allowed; explicitly selected stdin must contain JSON.
			}
			app := captureErrorTestApp()
			arg := input
			if source == "stdin" {
				arg = "-"
				app.Reader = strings.NewReader(input)
			}
			args := []string{"buildkite-agent", "capture-error", "code", "--message", "failure", "--context", arg}
			if source == "env" {
				t.Setenv("BUILDKITE_AGENT_JOB_CAPTURE_ERROR_CONTEXT", input)
				args = args[:5]
			}
			err := app.Run(t.Context(), args)
			if err == nil || !strings.Contains(err.Error(), "invalid context JSON") {
				t.Errorf("%s context %q: error = %v, want invalid context JSON", source, input, err)
			}
		}
	}
}

func TestJobCaptureErrorDoesNotFallbackWithoutLocalJobAPI(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	app := captureErrorTestApp()
	err := app.Run(t.Context(), []string{"buildkite-agent", "capture-error", "x", "--message", "raw customer diagnostic"})
	if err == nil || !strings.Contains(err.Error(), "Local Job API is required") {
		t.Fatalf("error = %v, want Local Job API required error", err)
	}
}
