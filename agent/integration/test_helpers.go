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

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
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
}

func runJob(t *testing.T, cfg testRunJobConfig) {
	t.Helper()

	l := logger.Discard

	// minimal metrics, this could be cleaner
	m := metrics.NewCollector(l, metrics.CollectorConfig{})
	scope := m.Scope(metrics.Tags{})

	// set the bootstrap into the config
	cfg.agentCfg.BootstrapScript = cfg.mockBootstrap.Path

	client := api.NewClient(l, api.Config{
		Endpoint: cfg.server.URL,
		Token:    "llamasrock",
	})

	jr, err := agent.NewJobRunner(l, client, agent.JobRunnerConfig{
		Job:                cfg.job,
		JWKS:               cfg.verificationJWKS,
		AgentConfiguration: cfg.agentCfg,
		MetricsScope:       scope,
	})
	if err != nil {
		t.Fatalf("agent.NewJobRunner() error = %v", err)
	}

	if err := jr.Run(context.Background()); err != nil {
		t.Errorf("jr.Run() = %v", err)
	}
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

func (tae *testAgentEndpoint) logsFor(t *testing.T, jobID string) string {
	t.Helper()
	tae.mtx.Lock()
	defer tae.mtx.Unlock()

	logChunks := make([]string, len(tae.logChunks))
	for seq, chunk := range tae.logChunks {
		logChunks[seq-1] = chunk
	}

	return strings.Join(logChunks, "")
}

func (t *testAgentEndpoint) server(jobID string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.mtx.Lock()
		defer t.mtx.Unlock()

		b, _ := io.ReadAll(req.Body)
		t.calls[req.URL.Path] = append(t.calls[req.URL.Path], b)

		switch req.URL.Path {
		case "/jobs/" + jobID:
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintf(rw, `{"state":"running"}`)
		case "/jobs/" + jobID + "/start":
			rw.WriteHeader(http.StatusOK)
		case "/jobs/" + jobID + "/chunks":
			sequence := req.URL.Query().Get("sequence")
			seqNo, _ := strconv.Atoi(sequence)
			r, _ := gzip.NewReader(bytes.NewBuffer(b))
			uz, _ := io.ReadAll(r)
			t.logChunks[seqNo] = string(uz)
			rw.WriteHeader(http.StatusCreated)
		case "/jobs/" + jobID + "/finish":
			rw.WriteHeader(http.StatusOK)
		default:
			http.Error(rw, fmt.Sprintf("not found; method = %q, path = %q", req.Method, req.URL.Path), http.StatusNotFound)
		}
	}))
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
