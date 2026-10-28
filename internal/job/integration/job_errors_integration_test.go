package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/osc7501"
)

func TestCommandFailureDiagnosticProgramStatus(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("command uses POSIX shell syntax")
	}

	ctx, _ := experiments.Enable(mainCtx, experiments.ProgramStatus)
	tester, err := NewExecutorTester(ctx)
	if err != nil {
		t.Fatalf("NewExecutorTester() error = %v", err)
	}
	defer tester.Close()
	agentAPI := newJobErrorsAPI(t, nil)
	const secret = "synthetic-diagnostic-secret-7842"
	err = tester.Run(t, append(agentAPI.env(),
		"BUILDKITE_PROGRAM_STATUS=true",
		"DIAGNOSTIC_SECRET="+secret,
		"BUILDKITE_REDACTED_VARS=DIAGNOSTIC_SECRET,BUILDKITE_AGENT_ACCESS_TOKEN",
		`BUILDKITE_COMMAND=printf 'Test suite failed: %s\n' "$DIAGNOSTIC_SECRET"; exit 3`,
	)...)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("tester.Run() = %v, want exit status 3", err)
	}

	direct := agentAPI.report(t, "buildkite.command_failed")
	if !strings.Contains(direct.Message, "exited with status 3") || !strings.Contains(direct.Message, "Test suite failed: [REDACTED]") {
		t.Errorf("direct diagnostic missing failure details: %q", direct.Message)
	}
	if strings.Contains(direct.Message, secret) || strings.Contains(tester.Output, secret) {
		t.Error("registered secret leaked in direct diagnostic or job output")
	}

	var diagnostics []osc7501.Report
	scanner := osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) {
		if strings.Contains(r.Msg, secret) {
			t.Error("registered secret leaked in decoded OSC message")
		}
		if r.App == "buildkite-agent" && r.ID == "diagnostic" {
			diagnostics = append(diagnostics, r)
		}
	}}
	_, _ = scanner.Write([]byte(tester.Output))
	if len(diagnostics) != 1 {
		t.Fatalf("decoded diagnostic reports = %d, want 1", len(diagnostics))
	}
	// OSC text fields replace control characters, including newlines, with spaces.
	wantMessage := strings.ReplaceAll(direct.Message, "\n", " ")
	if got := diagnostics[0]; got.State != osc7501.StateError || got.Msg != wantMessage {
		t.Errorf("OSC diagnostic = %+v, want error with message %q", got, wantMessage)
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
