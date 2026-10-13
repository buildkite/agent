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
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/kubernetes"
	"github.com/buildkite/agent/v4/logger"
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
		stopping           bool
		want               bool
	}{
		{name: "job opted in", jobOptIn: "true", want: true},
		{name: "agent opted in", agentEnv: "true", want: true},
		{name: "job opted out on an agent that opted in", jobOptIn: "false", agentEnv: "true"},
		{name: "not opted in"},
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
				conf:        JobRunnerConfig{Job: &api.Job{ID: "job-id", Env: env}},
				agentLogger: logger.Discard,
				apiClient:   api.NewClient(logger.Discard, api.Config{Endpoint: upstream.URL + "/", Token: "job-token"}),
				process:     failedJobProcess{err: &kubernetes.StartTimeoutError{Timeout: 5 * time.Minute}},
				jobLogs:     io.Discard,
			}
			r.agentStopping.Store(tc.stopping)

			if exit := r.runJob(t.Context()); exit.Status != -1 || exit.SignalReason != SignalReasonProcessRunError {
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
			if len(reports) != 1 || reports[0].Code != "kubernetes_container_not_connected" || !strings.Contains(reports[0].Message, "within 5m0s") {
				t.Fatalf("reports = %+v, want one kubernetes_container_not_connected report naming the timeout", reports)
			}
		})
	}
}
