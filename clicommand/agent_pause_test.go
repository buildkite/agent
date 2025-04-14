package clicommand

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/buildkite/agent/v3/logger"
)

func newAgentPauseTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.RequestURI() {
		case "/pause":
			rw.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
		}
	}))
}

func TestAgentPause(t *testing.T) {
	server := newAgentPauseTestServer(t)
	defer server.Close()

	ctx := context.Background()
	cfg := AgentPauseConfig{
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
	}
	l := logger.NewBuffer()

	if err := pause(ctx, cfg, l); err != nil {
		t.Errorf("pause(ctx, %v, l) = %v", cfg, err)
	}
	if got, want := l.Messages, "[info] Successfully paused agent"; !slices.Contains(got, want) {
		t.Errorf("after pause, l.Messages = %q\nis missing %q", got, want)
	}
}
