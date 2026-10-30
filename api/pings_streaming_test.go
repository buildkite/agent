package api_test

import (
	"context"
	"crypto/tls"
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

func (delayedPingStreamHandler) StreamPings(ctx context.Context, _ *connect.Request[agentedgev1.StreamPingsRequest], stream *connect.ServerStream[agentedgev1.StreamPingsResponse]) error {
	if err := stream.Send(resumePing()); err != nil {
		return err
	}

	select {
	case <-time.After(150 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	return stream.Send(resumePing())
}

func TestStreamPingsDoesNotUseAPIClientTimeout(t *testing.T) {
	path, handler := agentedgev1connect.NewAgentEdgeServiceHandler(delayedPingStreamHandler{})
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := newHTTP2PingServer(t, mux)

	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL + "/v3",
		Timeout:  50 * time.Millisecond,
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
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

func TestStreamPingsSetupTimeout(t *testing.T) {
	for _, response := range []string{"no headers", "headers only", "partial message"} {
		t.Run(response, func(t *testing.T) {
			requestCancelled := make(chan struct{})
			server := newHTTP2PingServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if response != "no headers" {
					w.Header().Set("Content-Type", "application/grpc")
					w.WriteHeader(http.StatusOK)
					if response == "partial message" {
						// An envelope advertising ten bytes, with only one byte of payload.
						_, _ = w.Write([]byte{0, 0, 0, 0, 10, 0})
					}

					w.(http.Flusher).Flush()
				}

				<-r.Context().Done()
				close(requestCancelled)
			}))
			httpClient := server.Client()
			httpClient.Timeout = 50 * time.Millisecond
			client := api.NewClient(logger.Discard, api.Config{
				Endpoint:   server.URL + "/v3",
				HTTPClient: httpClient,
			})
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			stream, err := client.StreamPings(ctx, "agent-id")
			if err == nil {
				for _, streamErr := range stream {
					if streamErr != nil {
						t.Errorf("setup failure yielded through established stream: %v", streamErr)
					}
				}

				t.Fatal("StreamPings succeeded without a complete first message")
			}

			if ctx.Err() != nil {
				t.Fatalf("StreamPings waited for caller deadline rather than setup timeout: %v", err)
			}

			if got := connect.CodeOf(err); got != connect.CodeDeadlineExceeded {
				t.Errorf("StreamPings error code = %v, want deadline_exceeded: %v", got, err)
			}

			select {
			case <-requestCancelled:
			case <-ctx.Done():
				t.Fatal("setup timeout didn't cancel the HTTP/2 request")
			}
		})
	}
}

func TestStreamPingsCancellation(t *testing.T) {
	for _, phase := range []string{"opening", "established", "iteration stopped"} {
		t.Run(phase, func(t *testing.T) {
			requestStarted := make(chan struct{})
			requestCancelled := make(chan struct{})
			server := newHTTP2PingServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if phase != "opening" {
					w.Header().Set("Content-Type", "application/grpc")
					// A complete, empty protobuf message is a valid first message.
					_, _ = w.Write([]byte{0, 0, 0, 0, 0})
					w.(http.Flusher).Flush()
				}

				close(requestStarted)
				<-r.Context().Done()
				close(requestCancelled)
			}))
			client := api.NewClient(logger.Discard, api.Config{
				Endpoint:   server.URL + "/v3",
				HTTPClient: server.Client(),
			})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				stream, err := client.StreamPings(ctx, "agent-id")
				if err != nil {
					done <- err
					return
				}

				for _, err := range stream {
					if err != nil {
						done <- err
						return
					}

					if phase == "iteration stopped" {
						break
					}

					cancel()
				}

				done <- nil
			}()
			select {
			case <-requestStarted:
			case <-time.After(3 * time.Second):
				t.Fatal("request didn't start")
			}

			if phase == "opening" {
				cancel()
			}

			select {
			case err := <-done:
				if phase != "iteration stopped" && connect.CodeOf(err) != connect.CodeCanceled {
					t.Errorf("StreamPings cancellation error = %v, want canceled", err)
				}

				if phase == "iteration stopped" && err != nil {
					t.Errorf("stopping iteration: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("StreamPings didn't stop")
			}

			select {
			case <-requestCancelled:
			case <-time.After(3 * time.Second):
				t.Fatal("HTTP/2 request wasn't cancelled")
			}
		})
	}
}

func newHTTP2PingServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("request protocol = %s, want HTTP/2", r.Proto)
		}

		handler.ServeHTTP(w, r)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
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
