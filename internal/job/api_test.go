package job

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/internal/socket"
	"github.com/buildkite/agent/v4/jobapi"
)

func TestCapturedErrorJobCancellation(t *testing.T) {
	ctx, _ := experiments.Enable(t.Context(), experiments.CaptureError)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	started := make(chan struct{})
	upstreamCancelled := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) != 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		close(started)
		select {
		case <-r.Context().Done():
			close(upstreamCancelled)
		case <-release:
		}
	}))
	defer upstream.Close()
	defer close(release)
	sh, err := shell.New(shell.WithEnv(env.New()), shell.WithLogger(shell.TestingLogger{T: t}))
	if err != nil {
		t.Fatal(err)
	}
	e := &Executor{shell: sh, redactors: replacer.NewMux()}
	e.JobID = "test-job"
	e.SocketsPath = os.TempDir()
	sh.Env.Set("BUILDKITE_AGENT_ENDPOINT", upstream.URL)
	cleanup, err := e.startJobAPI(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	client, err := jobapi.NewClient(t.Context(), e.jobAPI.SocketPath, sh.Env.GetString("BUILDKITE_AGENT_JOB_API_TOKEN", ""))
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		result <- client.CaptureError(t.Context(), &jobapi.CapturedError{Code: "container_process_failed", Message: "Container failed"})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("report did not reach upstream")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("report succeeded after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("report did not stop after job cancellation")
	}
	select {
	case <-upstreamCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request was not cancelled")
	}
	// A hook may start reporting after the job was already cancelled. It must
	// fail locally without starting a fresh upstream request.
	if err := client.CaptureError(t.Context(), &jobapi.CapturedError{Code: "container_process_failed", Message: "Container failed"}); err == nil {
		t.Fatal("report succeeded for an already cancelled job")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream requests = %d, want 1", got)
	}
	if _, err := client.EnvGet(t.Context()); err != nil {
		t.Fatalf("cancellation affected another Job API endpoint: %v", err)
	}
}

func TestCapturedErrorExperiment(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name, want := "disabled", http.StatusNotFound
		ctx := t.Context()
		if enabled {
			name, want = "enabled", http.StatusCreated
			ctx, _ = experiments.Enable(ctx, experiments.CaptureError)
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/v3/jobs/test-job/errors" || r.Header.Get("Authorization") != "Token test-job-token" {
					t.Errorf("unexpected upstream request: %s %s", r.Method, r.URL.Path)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if payload["timestamp"] == nil || payload["idempotency_key"] == nil || payload["code"] != "image_pull_failed" {
					t.Errorf("unexpected upstream payload: %v", payload)
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"uuid":"occurrence-id"}`))
			}))
			defer upstream.Close()
			sh, err := shell.New(shell.WithEnv(env.New()), shell.WithLogger(shell.TestingLogger{T: t}))
			if err != nil {
				t.Fatal(err)
			}
			e := &Executor{shell: sh, redactors: replacer.NewMux()}
			e.JobID = "test-job"
			sh.Env.Set("BUILDKITE_AGENT_ENDPOINT", upstream.URL+"/v3")
			sh.Env.Set("BUILDKITE_AGENT_ACCESS_TOKEN", "test-job-token")
			sh.Env.Set("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "true")
			e.SocketsPath = os.TempDir()
			cleanup, err := e.startJobAPI(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			capability, present := sh.Env.Get("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR")
			if present != enabled || (enabled && capability != "true") {
				t.Errorf("capture capability = %q (present %v), experiment enabled = %v", capability, present, enabled)
			}
			transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", e.jobAPI.SocketPath)
			}}
			t.Cleanup(transport.CloseIdleConnections)
			client := &http.Client{Transport: transport}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://job/api/current-job/v0/errors", strings.NewReader(`{"code":"image_pull_failed","message":"Failed to pull image"}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+sh.Env.GetString("BUILDKITE_AGENT_JOB_API_TOKEN", ""))
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != want {
				t.Errorf("status = %d, want %d", resp.StatusCode, want)
			}
			if !enabled {
				apiClient, err := jobapi.NewClient(ctx, e.jobAPI.SocketPath, sh.Env.GetString("BUILDKITE_AGENT_JOB_API_TOKEN", ""))
				if err != nil {
					t.Fatal(err)
				}
				err = apiClient.CaptureError(ctx, &jobapi.CapturedError{Code: "image_pull_failed", Message: "Failed to pull image"})
				if err == nil || !strings.Contains(err.Error(), "enable the capture-error experiment on the parent agent") {
					t.Fatalf("capture error = %v, want actionable disabled-experiment error", err)
				}
			}
			if (calls == 1) != enabled {
				t.Errorf("upstream calls = %d, experiment enabled = %v", calls, enabled)
			}
		})
	}
}

func TestCapturedErrorCapabilityAbsentAfterStartupFailure(t *testing.T) {
	sh, err := shell.New(shell.WithEnv(env.New()), shell.WithLogger(shell.TestingLogger{T: t}))
	if err != nil {
		t.Fatal(err)
	}
	sh.Env.Set("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "true")
	e := &Executor{shell: sh, redactors: replacer.NewMux()}
	e.SocketsPath = filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(e.SocketsPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, _ := experiments.Enable(t.Context(), experiments.CaptureError)
	cleanup, err := e.startJobAPI(ctx)
	t.Cleanup(cleanup)
	if socket.Available() && err == nil {
		t.Fatal("startJobAPI succeeded with a file as its sockets directory")
	}
	if sh.Env.Exists("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR") {
		t.Fatal("capture capability advertised without a running Job API")
	}
}
