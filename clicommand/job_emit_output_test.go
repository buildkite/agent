package clicommand

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/urfave/cli/v3"
)

func runJobEmitOutputCommand(t *testing.T, serverURL string, args ...string) error {
	t.Helper()
	t.Setenv("BUILDKITE_JOB_ID", "job-123")

	// Clone the command because Run mutates it, which breaks the command completeness test.
	cmd := *JobEmitOutputCommand
	var out, errOut bytes.Buffer
	app := &cli.Command{
		Name:      "buildkite-agent",
		Commands:  []*cli.Command{&cmd},
		Writer:    &out,
		ErrWriter: &errOut,
	}

	runArgs := []string{
		"buildkite-agent",
		"emit-output",
		"--endpoint", serverURL,
		"--agent-access-token", "job-token",
	}
	runArgs = append(runArgs, args...)

	return app.Run(t.Context(), runArgs)
}

func TestJobEmitOutput(t *testing.T) {
	const schema = "example.error.v1"
	wantPayload := json.RawMessage(`{"code":"rate_limited","retryable":true}`)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if got, want := req.Method, http.MethodPost; got != want {
			t.Errorf("req.Method = %q, want %q", got, want)
		}
		if got, want := req.URL.Path, "/jobs/job-123/outputs"; got != want {
			t.Errorf("req.URL.Path = %q, want %q", got, want)
		}
		if got, want := req.Header.Get("Authorization"), "Token job-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}

		var body api.JobOutputRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		if got, want := body.Schema, schema; got != want {
			t.Errorf("body.Schema = %q, want %q", got, want)
		}
		if !bytes.Equal(body.Payload, wantPayload) {
			t.Errorf("body.Payload = %s, want %s", body.Payload, wantPayload)
		}

		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(rw, `{"uuid":"output-123","schema":%q,"payload":%s,"created_at":"2026-09-01T12:34:56Z"}`, schema, wantPayload)
	}))
	defer server.Close()

	err := runJobEmitOutputCommand(t, server.URL,
		"--schema", schema,
		"--payload", string(wantPayload),
	)
	if err != nil {
		t.Fatalf("runJobEmitOutputCommand() error = %v, want nil", err)
	}
}

func TestJobEmitOutputRejectsMalformedPayload(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		rw.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	err := runJobEmitOutputCommand(t, server.URL,
		"--schema", "example.error.v1",
		"--payload", `{"retryable":}`,
	)
	if err == nil {
		t.Fatal("runJobEmitOutputCommand() error = nil, want non-nil")
	}
	if got, want := err.Error(), "invalid JSON for --payload"; !strings.Contains(got, want) {
		t.Errorf("error = %q, want containing %q", got, want)
	}
	if got, want := err.Error(), "invalid character"; !strings.Contains(got, want) {
		t.Errorf("error = %q, want containing parser detail %q", got, want)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("server received %d requests, want 0", got)
	}
}

func TestJobEmitOutputReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = fmt.Fprint(rw, `{"message":"payload must not exceed 1 MiB"}`)
	}))
	defer server.Close()

	err := runJobEmitOutputCommand(t, server.URL,
		"--schema", "example.error.v1",
		"--payload", `null`,
	)
	if err == nil {
		t.Fatal("runJobEmitOutputCommand() error = nil, want non-nil")
	}
	for _, want := range []string{"failed to emit job output", "413 Request Entity Too Large", "payload must not exceed 1 MiB"} {
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("error = %q, want containing %q", got, want)
		}
	}
}

func TestJobEmitOutputRequiresCurrentJob(t *testing.T) {
	t.Setenv("BUILDKITE_JOB_ID", "")

	cmd := *JobEmitOutputCommand
	app := &cli.Command{
		Name:     "buildkite-agent",
		Commands: []*cli.Command{&cmd},
		Writer:   os.Stdout,
	}

	err := app.Run(t.Context(), []string{
		"buildkite-agent",
		"emit-output",
		"--endpoint", "http://example.invalid",
		"--agent-access-token", "job-token",
		"--schema", "example.error.v1",
		"--payload", `null`,
	})
	if err == nil {
		t.Fatal("command error = nil, want non-nil")
	}
	if got, want := err.Error(), "BUILDKITE_JOB_ID is not set"; !strings.Contains(got, want) {
		t.Errorf("error = %q, want containing %q", got, want)
	}
}
