package clicommand

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/buildkite/agent/v4/internal/logtest"
)

func newAgentStopTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.RequestURI() {
		case "/stop":
			rw.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
		}
	}))
}

func TestAgentStop(t *testing.T) {
	server := newAgentStopTestServer(t)
	defer server.Close()

	ctx := t.Context()
	cfg := AgentStopConfig{
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
	}
	l, lh := logtest.NewLogger()

	err := stop(ctx, cfg, l)
	if err != nil {
		t.Errorf("stop(ctx, cfg, l) error = %v, want nil", err)
	}
	if got, want := lh.Messages(), "Successfully stopped agent"; !slices.Contains(got, want) {
		t.Errorf("lh.Messages() = %v, want containing %q", got, want)
	}
}
