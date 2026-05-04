package clicommand

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/buildkite/agent/v4/logger"
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

	ctx := context.Background()
	cfg := AgentStopConfig{
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
	}
	l, rec := logger.Test(t, logger.QuietTb())

	err := stop(ctx, cfg, l)
	if err != nil {
		t.Errorf("stop(ctx, cfg, l) error = %v, want nil", err)
	}
	if got, want := rec.Messages(), "Successfully stopped agent"; !slices.Contains(got, want) {
		t.Errorf("rec.Messages() = %v, want containing %q", got, want)
	}
}
