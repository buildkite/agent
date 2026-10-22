package integration

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/joberrorosc"
	"github.com/buildkite/agent/v4/internal/process"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/agent/v4/metrics"
	"github.com/google/go-cmp/cmp"
)

func TestJobErrorsOSCEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX command")
	}
	t.Parallel()
	ctx, _ := experiments.Enable(mainCtx, experiments.JobErrorsOSC)
	ctx, _ = experiments.Enable(ctx, experiments.ProgramStatus)
	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	var mu sync.Mutex
	var logs strings.Builder
	var exit string
	a := newJobErrorsAPI(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chunks"):
			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer func() { _ = reader.Close() }()
			b, err := io.ReadAll(reader)
			if err != nil {
				t.Error(err)
			}
			mu.Lock()
			logs.Write(b)
			mu.Unlock()
		case strings.HasSuffix(r.URL.Path, "/finish"):
			var finished api.JobFinishRequest
			if err := json.NewDecoder(r.Body).Decode(&finished); err != nil {
				t.Error(err)
			}
			mu.Lock()
			exit = finished.ExitStatus
			mu.Unlock()
		case strings.HasSuffix(r.URL.Path, "/start"):
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	jobEnv := make(map[string]string)
	for _, pair := range append(tester.Env, a.env()...) {
		key, value, _ := strings.Cut(pair, "=")
		jobEnv[key] = value
	}
	jobEnv["INITIAL_SECRET"] = "initial-secret"
	jobEnv["SUT"] = tester.Name
	jobEnv["BUILDKITE_COMMAND"] = `printf runtime-secret | "$SUT" redactor add
"$SUT" job capture-error plugin.failed --message 'initial-secret runtime-secret https://user:password@example.com/path?signature=secret' >/dev/null 2>&1 || true
printf 'initial-secret\n'
exit 7`
	// Start the real parent runner and real bootstrap, with no external API.
	// Both automatic command errors and the Local Job API use this transport.
	runner, err := agent.NewJobRunner(ctx, logger.Discard,
		api.NewClient(logger.Discard, api.Config{Endpoint: a.URL + "/", Token: "test-job-token"}),
		agent.JobRunnerConfig{
			Job:               &api.Job{ID: "1111-1111-1111-1111", Env: jobEnv, ChunksMaxSizeBytes: 1 << 20, LogMaxSizeBytes: 1 << 20, ChunksIntervalSeconds: 60},
			JobStatusInterval: time.Hour,
			CancelSignal:      process.SIGTERM,
			MetricsScope:      metrics.NewCollector(logger.Discard, metrics.CollectorConfig{}).Scope(nil),
			AgentConfiguration: agent.AgentConfiguration{
				BootstrapScript: fmt.Sprintf("%q bootstrap --phases command", tester.Name),
				BuildPath:       tester.BuildDir, CommandEval: true,
				Shell: "/bin/bash -e -c", RedactedVars: []string{"*_SECRET", "*_TOKEN"},
				GitCommitVerification: "off",
				GitMirrorCheckoutMode: "dissociate",
			},
		})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(ctx, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if exit != "7" {
		t.Fatalf("job exit = %q, want 7; logs: %s", exit, logs.String())
	}
	report := a.report(t, "buildkite.command_failed")
	if !strings.Contains(report.Message, "status 7") || !strings.Contains(report.Message, "[REDACTED]") || strings.Contains(report.Message, "initial-secret") {
		t.Fatalf("unexpected error message: %q", report.Message)
	}
	pluginReport := a.report(t, "plugin.failed")
	if pluginReport.Message != "[REDACTED] [REDACTED] https://xxxxx@example.com/path?[REDACTED]" {
		t.Fatalf("plugin report was not redacted before OSC encoding: %q", pluginReport.Message)
	}
	var extracted []api.JobCapturedError
	scanner := joberrorosc.NewWriter(io.Discard, func(e api.JobCapturedError) { extracted = append(extracted, e) })
	if _, err := io.WriteString(scanner, logs.String()); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]api.JobCapturedError{pluginReport, report}, extracted); diff != "" {
		t.Fatalf("stored OSC vs API payload (-API +OSC):\n%s", diff)
	}
}

// jobErrorsAPI stands in for the Agent API. It records captured job errors and
// passes every other request to next, or responds 404 when next is nil.
type jobErrorsAPI struct {
	*httptest.Server
	mu      sync.Mutex
	reports []api.JobCapturedError
}

func newJobErrorsAPI(t *testing.T, next http.Handler) *jobErrorsAPI {
	t.Helper()
	a := &jobErrorsAPI{}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/jobs/1111-1111-1111-1111/errors") {
			if next == nil {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		var report api.JobCapturedError
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Errorf("decoding captured error: %v", err)
		}
		a.mu.Lock()
		a.reports = append(a.reports, report)
		a.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(a.Close)
	return a
}

// env opts the job in to agent error capture through this API.
func (a *jobErrorsAPI) env() []string {
	return []string{"BUILDKITE_AGENT_ENDPOINT=" + a.URL, "BUILDKITE_CAPTURE_AGENT_ERRORS=true"}
}

// codes returns the captured error codes in order.
func (a *jobErrorsAPI) codes() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.codesLocked()
}

// report returns the only captured error with code, failing otherwise.
func (a *jobErrorsAPI) report(t *testing.T, code string) api.JobCapturedError {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	var found []api.JobCapturedError
	for _, report := range a.reports {
		if report.Code == code {
			found = append(found, report)
		}
	}
	if len(found) != 1 {
		t.Fatalf("captured %q errors = %d, want 1; captured codes: %v", code, len(found), a.codesLocked())
	}
	return found[0]
}

func (a *jobErrorsAPI) codesLocked() []string {
	codes := make([]string, 0, len(a.reports))
	for _, report := range a.reports {
		codes = append(codes, report.Code)
	}
	return codes
}
