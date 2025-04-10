package clicommand

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func newAnnotateTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.RequestURI() {
		case "/jobs/jobid/annotations":
			io.WriteString(rw, `{"context":"", style:"", body:"abc"}`)
		default:
			t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
		}
	}))
}

func TestAnnotate(t *testing.T) {
	server := newAnnotateTestServer(t)
	defer server.Close()

	ctx := context.Background()
	cfg := AnnotateConfig{
		Body: "abc",
		Job:  "jobid",
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
		Priority: 1,
	}
	l := logger.NewBuffer()

	err := annotate(ctx, cfg, l)
	assert.NoError(t, err)
	assert.Contains(t, l.Messages, "[debug] Successfully annotated build")
}

func TestAnnotateMaxBodySize(t *testing.T) {
	ctx := context.Background()
	cfg := AnnotateConfig{
		Body: strings.Repeat("a", 1048577),
	}
	l := logger.NewBuffer()

	err := annotate(ctx, cfg, l)
	assert.Error(t, err, "Annotation body size (1048577) exceeds maximum (1048576)")
}
