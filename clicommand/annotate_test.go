package clicommand

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/logtest"
)

func newAnnotateTestServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.RequestURI() {
		case "/jobs/jobid/annotations":
			_, _ = io.WriteString(rw, `{"context":"", style:"", body:"abc"}`)
		default:
			t.Errorf("unexpected HTTP request: %s %v", req.Method, req.URL.RequestURI())
		}
	}))
}

func TestAnnotate(t *testing.T) {
	server := newAnnotateTestServer(t)
	defer server.Close()

	cfg := AnnotateConfig{
		Body: "abc",
		Job:  "jobid",
		APIConfig: APIConfig{
			AgentAccessToken: "agentaccesstoken",
			Endpoint:         server.URL,
		},
		Priority: 1,
	}
	l, lh := logtest.NewLogger()

	err := annotate(t.Context(), cfg, l)
	if err != nil {
		t.Errorf("annotate(ctx, %v, l) error = %v", cfg, err)
	}
	if want := "Successfully annotated build"; !slices.Contains(lh.Messages(), want) {
		t.Errorf("annotate(ctx, %v, l) logged messages = %q, missing %q", cfg, lh.Messages(), want)
	}
}

func TestAnnotateMaxBodySize(t *testing.T) {
	cfg := AnnotateConfig{
		Body: strings.Repeat("a", 1048577),
	}
	l := slog.New(slog.DiscardHandler)

	err := annotate(t.Context(), cfg, l)
	wantErr := annotationTooBigError{bodySize: 1048577}
	if !errors.Is(err, wantErr) {
		t.Errorf("annotate(ctx, AnnotateConfig{Body: \"aaa...aaa\"}, l) error = %v, want %[2]T %[2]v", err, wantErr)
	}
}
