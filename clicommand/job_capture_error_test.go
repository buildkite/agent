package clicommand

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/urfave/cli/v3"
)

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

	cmd := *JobCaptureErrorCommand
	app := &cli.Command{Commands: []*cli.Command{&cmd}}
	err = app.Run(t.Context(), []string{"buildkite-agent", "capture-error", `{"code":"image_pull_failed","message":"denied"}`})
	if err != nil {
		t.Fatalf("capture-error command error = %v", err)
	}
	if reported == nil || reported.Code != "image_pull_failed" {
		t.Errorf("reported = %+v, want image_pull_failed", reported)
	}
}

func TestJobCaptureErrorRejectsMalformedPayloadBeforeTransport(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	cmd := *JobCaptureErrorCommand
	app := &cli.Command{Commands: []*cli.Command{&cmd}}
	err := app.Run(t.Context(), []string{"buildkite-agent", "capture-error", "{"})
	if err == nil || !strings.Contains(err.Error(), "invalid captured-error JSON") {
		t.Fatalf("error = %v, want invalid captured-error JSON", err)
	}
}

func TestJobCaptureErrorDoesNotFallbackWithoutLocalJobAPI(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	cmd := *JobCaptureErrorCommand
	app := &cli.Command{Commands: []*cli.Command{&cmd}}
	err := app.Run(t.Context(), []string{"buildkite-agent", "capture-error", `{"code":"x","message":"raw customer diagnostic"}`})
	if err == nil || !strings.Contains(err.Error(), "Local Job API is required") {
		t.Fatalf("error = %v, want Local Job API required error", err)
	}
}
