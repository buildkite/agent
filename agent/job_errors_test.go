package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/joberrorosc"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/kubernetes"
	"github.com/buildkite/agent/v4/logger"
	"github.com/wolfeidau/osc7501"
)

// failedJobProcess is a job process that fails to run.
type failedJobProcess struct{ err error }

func (p failedJobProcess) Done() <-chan struct{}          { return nil }
func (p failedJobProcess) Started() <-chan struct{}       { return nil }
func (p failedJobProcess) Interrupt() error               { return nil }
func (p failedJobProcess) Terminate() error               { return nil }
func (p failedJobProcess) Run(context.Context) error      { return p.err }
func (p failedJobProcess) WaitStatus() process.WaitStatus { return nil }

func TestKubernetesStartTimeoutIsCaptured(t *testing.T) {
	for _, tc := range []struct {
		name               string
		jobOptIn, agentEnv string
		protected          []string
		stopping           bool
		osc                bool
		want               bool
	}{
		{name: "job opted in", jobOptIn: "true", want: true},
		{name: "OSC alongside direct capture", jobOptIn: "true", osc: true, want: true},
		{name: "agent opted in", agentEnv: "true", want: true},
		{name: "job opted out on an agent that opted in", jobOptIn: "false", agentEnv: "true"},
		{name: "not opted in"},
		// Bootstrap never sees a job's value for a protected name.
		{name: "job opted in on an agent that protects the opt-in", jobOptIn: "true", agentEnv: "false", protected: []string{"BUILDKITE_CAPTURE_AGENT_ERRORS"}},
		{name: "job opted out on an agent that protects an opt-in it set", jobOptIn: "false", agentEnv: "true", protected: []string{"BUILDKITE_CAPTURE_*"}, want: true},
		// Containers that never connect are expected while the agent stops.
		{name: "agent stopping", jobOptIn: "true", stopping: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BUILDKITE_CAPTURE_AGENT_ERRORS", tc.agentEnv)
			var mu sync.Mutex
			var reports []api.JobCapturedError
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var report api.JobCapturedError
				if r.URL.Path != "/jobs/job-id/errors" || json.NewDecoder(r.Body).Decode(&report) != nil {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				mu.Lock()
				reports = append(reports, report)
				mu.Unlock()
				w.WriteHeader(http.StatusCreated)
			}))
			t.Cleanup(upstream.Close)

			env := map[string]string{}
			if tc.jobOptIn != "" {
				env["BUILDKITE_CAPTURE_AGENT_ERRORS"] = tc.jobOptIn
			}
			r := &JobRunner{
				conf: JobRunnerConfig{
					Job:                &api.Job{ID: "job-id", Env: env},
					AgentConfiguration: AgentConfiguration{ProtectedEnvironmentVariables: tc.protected},
				},
				agentLogger: logger.Discard,
				apiClient:   api.NewClient(logger.Discard, api.Config{Endpoint: upstream.URL + "/", Token: "job-token"}),
				process:     failedJobProcess{err: &kubernetes.StartTimeoutError{Timeout: 5 * time.Minute}},
				jobLogs:     io.Discard,
			}
			r.agentStopping.Store(tc.stopping)

			ctx := t.Context()
			var log strings.Builder
			if tc.osc {
				ctx, _ = experiments.Enable(ctx, experiments.JobErrorsOSC)
				ctx, _ = experiments.Enable(ctx, experiments.ProgramStatus)
				r.jobLogs = &log
			}
			if exit := r.runJob(ctx); exit.Status != -1 || exit.SignalReason != SignalReasonProcessRunError {
				t.Errorf("runJob() = %+v, want status -1 with the process-run-error signal reason", exit)
			}

			mu.Lock()
			defer mu.Unlock()
			if !tc.want {
				if len(reports) != 0 {
					t.Fatalf("reports = %+v, want none", reports)
				}
				return
			}
			if tc.osc {
				if len(reports) != 2 || reports[0].Code != joberrorosc.Code || reports[0].Message != "A container did not connect before the start timeout." {
					t.Fatalf("reports = %+v, want independent OSC and direct reports", reports)
				}
				reports = reports[1:]
			}
			if len(reports) != 1 || reports[0].Code != "buildkite.kubernetes_container_not_connected" || !strings.Contains(reports[0].Message, "within 5m0s") {
				t.Fatalf("reports = %+v, want the unchanged direct capture naming the timeout", reports)
			}
			if tc.osc && !strings.Contains(log.String(), "\x1b]7501;") {
				t.Fatal("captured error did not travel through the job log")
			}
		})
	}
}

func TestOSCErrorForwarding(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusCreated, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var got []api.JobCapturedError
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/jobs/job-id/errors" || req.Header.Get("Authorization") != "Token job-token" {
					t.Errorf("unexpected request path or authentication: %s", req.URL.Path)
				}
				var e api.JobCapturedError
				if err := json.NewDecoder(req.Body).Decode(&e); err != nil {
					t.Error(err)
				}
				got = append(got, e)
				w.WriteHeader(status)
			}))
			defer upstream.Close()
			r := &JobRunner{
				conf:        JobRunnerConfig{Job: &api.Job{ID: "job-id"}},
				agentLogger: logger.Discard,
				apiClient:   api.NewClient(logger.Discard, api.Config{Endpoint: upstream.URL + "/", Token: "job-token"}),
			}
			var log strings.Builder
			writer := r.capturedErrorLogWriter(t.Context(), &log)
			for range 2 {
				if err := osc7501.NewReporter(writer, "buildkite-agent").Failed("error/command", "Failed at https://user:password@example.com/path?signature=secret"); err != nil {
					t.Fatal(err)
				}
			}
			want := 2
			if status == http.StatusNotFound || status == http.StatusUnprocessableEntity {
				want = 1
			}
			if len(got) != want || got[0].Code != joberrorosc.Code || got[0].IdempotencyKey == "" || got[0].Timestamp.IsZero() || got[0].Message != "Failed at https://xxxxx@example.com/path?[REDACTED]" {
				t.Fatalf("unexpected forwarded reports: %+v", got)
			}
		})
	}
}
