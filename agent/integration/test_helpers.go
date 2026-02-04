package integration

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/ptr"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/metrics"
	"github.com/buildkite/bintest/v3"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

func mockBootstrap(t *testing.T) *bintest.Mock {
	t.Helper()

	// tests run using t.Run() will have a slash in their name, which will mess with paths to bintest binaries
	name := strings.ReplaceAll(t.Name(), "/", "-")
	name = strings.ReplaceAll(name, "'", "") // we also don"t like single quotes
	bs, err := bintest.NewMock(fmt.Sprintf("buildkite-agent-bootstrap-%s", name))
	if err != nil {
		t.Fatalf("bintest.NewMock() error = %v", err)
	}

	return bs
}

type testRunJobConfig struct {
	job              *api.Job
	server           *httptest.Server
	agentCfg         agent.AgentConfiguration
	mockBootstrap    *bintest.Mock
	verificationJWKS jwk.Set
	client           *api.Client
}

func runJob(t *testing.T, ctx context.Context, cfg testRunJobConfig) error {
	t.Helper()

	l := logger.Discard

	// minimal metrics, this could be cleaner
	m := metrics.NewCollector(l, metrics.CollectorConfig{})
	scope := m.Scope(metrics.Tags{})

	// set the bootstrap into the config
	cfg.agentCfg.BootstrapScript = cfg.mockBootstrap.Path

	if cfg.client == nil {
		cfg.client = api.NewClient(l, api.Config{
			Endpoint: cfg.server.URL,
			Token:    "llamasrock",
		})
	}

	jr, err := agent.NewJobRunner(ctx, l, cfg.client, agent.JobRunnerConfig{
		Job:                cfg.job,
		JWKS:               cfg.verificationJWKS,
		AgentConfiguration: cfg.agentCfg,
		MetricsScope:       scope,
		JobStatusInterval:  1 * time.Second,
	})
	if err != nil {
		t.Fatalf("agent.NewJobRunner() error = %v", err)
	}

	var ignoreAgentInDispatches *bool
	if cfg.agentCfg.DisconnectAfterJob {
		ignoreAgentInDispatches = ptr.To(true)
	}

	return jr.Run(ctx, ignoreAgentInDispatches)
}

type testAgentEndpoint struct {
	calls     map[string][][]byte
	logChunks map[int]string
	mtx       sync.Mutex
}

func createTestAgentEndpoint() *testAgentEndpoint {
	return &testAgentEndpoint{
		calls:     make(map[string][][]byte, 4),
		logChunks: make(map[int]string),
	}
}

func (tae *testAgentEndpoint) finishesFor(t *testing.T, jobID string) []api.Job {
	t.Helper()
	tae.mtx.Lock()
	defer tae.mtx.Unlock()

	endpoint := fmt.Sprintf("/jobs/%s/finish", jobID)
	finishes := make([]api.Job, 0, len(tae.calls))

	for _, b := range tae.calls[endpoint] {
		var job api.Job
		err := json.Unmarshal(b, &job)
		if err != nil {
			t.Fatalf("decoding accept request body: %v", err)
		}
		finishes = append(finishes, job)
	}

	return finishes
}

func (tae *testAgentEndpoint) logsFor(t *testing.T, _ string) string {
	t.Helper()
	tae.mtx.Lock()
	defer tae.mtx.Unlock()

	logChunks := make([]string, len(tae.logChunks))
	for seq, chunk := range tae.logChunks {
		logChunks[seq-1] = chunk
	}

	return strings.Join(logChunks, "")
}

type route struct {
	Path   string
	Method string
	http.HandlerFunc
}

func (t *testAgentEndpoint) getJobsHandler() http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
		fmt.Fprintf(rw, `{"state":"running"}`)
	}
}

func (t *testAgentEndpoint) chunksHandler() http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}

		sequence := req.URL.Query().Get("sequence")
		seqNo, err := strconv.Atoi(sequence)
		if err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}

		r, err := gzip.NewReader(bytes.NewBuffer(b))
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}

		uz, err := io.ReadAll(r)
		if err != nil {
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}

		t.logChunks[seqNo] = string(uz)
		rw.WriteHeader(http.StatusCreated)
	}
}

func (t *testAgentEndpoint) defaultRoutes() []route {
	return []route{
		{
			Method:      "GET",
			Path:        "/jobs/",
			HandlerFunc: t.getJobsHandler(),
		},
		{
			Method:      "PUT",
			Path:        "/jobs/{id}/start",
			HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		},
		{
			Method:      "POST",
			Path:        "/jobs/{id}/chunks",
			HandlerFunc: t.chunksHandler(),
		},
		{
			Method:      "PUT",
			Path:        "/jobs/{id}/finish",
			HandlerFunc: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		},
	}
}

func (t *testAgentEndpoint) server(extraRoutes ...route) *httptest.Server {
	mux := http.NewServeMux()

	defaultRoutes := t.defaultRoutes()
	routesUniq := make(map[string]http.HandlerFunc, len(defaultRoutes))
	for _, r := range defaultRoutes {
		routesUniq[fmt.Sprintf("%s %s", r.Method, r.Path)] = r.HandlerFunc
	}

	// extra routes overwrite default routes if they conflict
	for _, r := range extraRoutes {
		routesUniq[fmt.Sprintf("%s %s", r.Method, r.Path)] = r.HandlerFunc
	}

	wrapRecordRequest := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			writeErr := func(status int, f string, v ...any) {
				msg := fmt.Sprintf(f, v...)
				fmt.Fprintln(os.Stderr, msg)
				http.Error(rw, msg, status)
			}

			// wait a minute, what's going on here?
			// well, we want to read the body of the request, but because HTTP response bodies are io.ReadClosers, they can only
			// be read once. So we read the body, then write it back into the request body so that the next handler can read it.
			b, err := io.ReadAll(req.Body)
			if err != nil {
				writeErr(http.StatusBadRequest, "incomplete body read: %v", err)
				return
			}
			if err := req.Body.Close(); err != nil {
				writeErr(http.StatusInternalServerError, "error from req.Body.Close: %v", err)
				return
			}

			req.Body = io.NopCloser(bytes.NewReader(b))

			t.mtx.Lock()
			t.calls[req.URL.Path] = append(t.calls[req.URL.Path], b)
			t.mtx.Unlock()

			// We also require job tokens are used for authentication
			authzHeader := req.Header.Get("Authorization")
			if !strings.HasPrefix(authzHeader, "Token bkaj_") {
				writeErr(http.StatusUnauthorized, "Authorization header = %q, want job token prefix 'Token bkaj_'", authzHeader)
				return
			}

			next.ServeHTTP(rw, req)
		})
	}

	for path, handler := range routesUniq {
		mux.Handle(path, wrapRecordRequest(handler))
	}

	return httptest.NewServer(mux)
}

func mockPreBootstrap(t *testing.T, hooksDir string) *bintest.Mock {
	t.Helper()

	mock, err := bintest.NewMock(fmt.Sprintf("buildkite-agent-pre-bootstrap-hook-%s", t.Name()))
	if err != nil {
		t.Fatalf("bintest.NewMock() error = %v", err)
	}

	hookScript := filepath.Join(hooksDir, "pre-bootstrap")
	body := ""

	if runtime.GOOS == "windows" {
		// You may be tempted to change this to `@%q`, but please do not. bintest doesn't like it when things change.
		// (%q escapes backslashes, which are windows path separators and leads to this test failing on windows)
		body = fmt.Sprintf(`@"%s"`, mock.Path)
		hookScript += ".bat"
	} else {
		body = "#!/bin/sh\n" + mock.Path
	}

	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		t.Fatalf("creating pre-bootstrap hook mock: os.MkdirAll() error = %v", err)
	}

	if err := os.WriteFile(hookScript, []byte(body), 0o777); err != nil {
		t.Fatalf("creating pre-bootstrap hook mock: s.WriteFile() error = %v", err)
	}

	return mock
}
