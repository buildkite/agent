package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/buildkite/agent/v4/api"
	agentedgev1 "github.com/buildkite/agent/v4/api/proto/gen"
	"github.com/buildkite/agent/v4/api/proto/gen/agentedgev1connect"
	"github.com/buildkite/agent/v4/logger"
)

type delayedPingStreamHandler struct{}

func (delayedPingStreamHandler) StreamPings(_ context.Context, _ *connect.Request[agentedgev1.StreamPingsRequest], stream *connect.ServerStream[agentedgev1.StreamPingsResponse]) error {
	if err := stream.Send(resumePing()); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)
	return stream.Send(resumePing())
}

func TestStreamPingsDoesNotUseAPIClientTimeout(t *testing.T) {
	path, handler := agentedgev1connect.NewAgentEdgeServiceHandler(delayedPingStreamHandler{})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL + "/v3",
		Timeout:  50 * time.Millisecond,
	})
	stream, err := client.StreamPings(t.Context(), "agent-id")
	if err != nil {
		t.Fatalf("client.StreamPings: %v", err)
	}

	var messages int
	for _, err := range stream {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		messages++
	}
	if got, want := messages, 2; got != want {
		t.Errorf("stream message count = %d, want %d", got, want)
	}
}

func TestAPIRequestsStillUseClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)

	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL + "/",
		Timeout:  50 * time.Millisecond,
	})
	_, _, err := client.Ping(t.Context())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("client.Ping error = %v, want context deadline exceeded", err)
	}
}

func resumePing() *agentedgev1.StreamPingsResponse {
	return &agentedgev1.StreamPingsResponse{
		Action: &agentedgev1.StreamPingsResponse_Resume{
			Resume: &agentedgev1.ResumeAction{},
		},
	}
}
