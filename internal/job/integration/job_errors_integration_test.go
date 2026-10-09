package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
)

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
