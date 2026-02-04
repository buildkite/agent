package clicommand

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
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
	l := logger.NewBuffer()

	err := stop(ctx, cfg, l)
	assert.NoError(t, err)
	assert.Contains(t, l.Messages, "[info] Successfully stopped agent")
}
