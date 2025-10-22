package api_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

func TestRegisteringAndConnectingClient(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/register":
			if got, want := authToken(req), "llamas"; got != want {
				http.Error(rw, fmt.Sprintf("authToken(req) = %q, want %q", got, want), http.StatusUnauthorized)
				return
			}
			rw.WriteHeader(http.StatusOK)
			fmt.Fprint(rw, `{"id":"12-34-56-78-91", "name":"agent-1", "access_token":"alpacas"}`) //nolint:errcheck // The test would still fail

		case "/connect":
			if got, want := authToken(req), "alpacas"; got != want {
				http.Error(rw, fmt.Sprintf("authToken(req) = %q, want %q", got, want), http.StatusUnauthorized)
				return
			}
			rw.WriteHeader(http.StatusOK)
			fmt.Fprint(rw, `{}`) //nolint:errcheck // The test would still fail

		default:
			http.Error(rw, fmt.Sprintf("not found; method = %q, path = %q", req.Method, req.URL.Path), http.StatusNotFound)
		}
	}))
	defer server.Close()

	// enable HTTP/2.0 to ensure the client can handle defaults to using it
	server.EnableHTTP2 = true
	server.StartTLS()

	ctx := context.Background()

	// Initial client with a registration token
	c := api.NewClient(logger.Discard, api.Config{
		Endpoint:  server.URL,
		Token:     "llamas",
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	})

	// Check a register works
	reg, httpResp, err := c.Register(ctx, &api.AgentRegisterRequest{})
	if err != nil {
		t.Fatalf("c.Register(&AgentRegisterRequest{}) error = %v", err)
	}

	if got, want := reg.Name, "agent-1"; got != want {
		t.Errorf("regResp.Name = %q, want %q", got, want)
	}

	if got, want := reg.AccessToken, "alpacas"; got != want {
		t.Errorf("regResp.AccessToken = %q, want %q", got, want)
	}

	if got, want := httpResp.Proto, "HTTP/2.0"; got != want {
		t.Errorf("httpResp.Proto = %q, want %q", got, want)
	}

	// New client with the access token
	c2 := c.FromAgentRegisterResponse(reg)

	// Check a connect works
	if _, err := c2.Connect(ctx); err != nil {
		t.Errorf("c.FromAgentRegisterResponse(regResp).Connect() error = %v", err)
	}
}

func TestCompressionBehavior(t *testing.T) {
	tests := []struct {
		name             string
		gzipAPIRequests  bool
		expectCompressed bool
	}{
		{
			name:             "compression disabled by default",
			gzipAPIRequests:  false,
			expectCompressed: false,
		},
		{
			name:             "compression enabled when requested",
			gzipAPIRequests:  true,
			expectCompressed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requestBody []byte
			var isCompressed bool

			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				// Read the request body
				body, err := io.ReadAll(req.Body)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
					return
				}
				requestBody = body

				// Check if the request is compressed
				isCompressed = req.Header.Get("Content-Encoding") == "gzip"

				// If compressed, try to decompress to verify it's valid gzip
				if isCompressed {
					gzReader, err := gzip.NewReader(bytes.NewReader(body))
					if err != nil {
						http.Error(rw, "Invalid gzip: "+err.Error(), http.StatusBadRequest)
						return
					}
					defer gzReader.Close()

					_, err = io.ReadAll(gzReader)
					if err != nil {
						http.Error(rw, "Failed to decompress: "+err.Error(), http.StatusBadRequest)
						return
					}
				}

				rw.WriteHeader(http.StatusOK)
				fmt.Fprint(rw, `{}`)
			}))
			defer server.Close()

			ctx := context.Background()
			client := api.NewClient(logger.Discard, api.Config{
				Endpoint:        server.URL,
				Token:           "test-token",
				GzipAPIRequests: tt.gzipAPIRequests,
			})

			// Make a request that will have a body (pipeline upload)
			testPipeline := map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{
						"command": "echo hello",
					},
				},
			}

			_, err := client.UploadPipeline(ctx, "test-job-id", &api.PipelineChange{
				UUID:     "test-uuid",
				Pipeline: testPipeline,
			})

			if err != nil {
				t.Fatalf("UploadPipeline failed: %v", err)
			}

			// Verify compression behavior matches expectation
			if isCompressed != tt.expectCompressed {
				t.Errorf("Expected compressed=%v, got compressed=%v", tt.expectCompressed, isCompressed)
			}

			// Verify we received a non-empty body
			if len(requestBody) == 0 {
				t.Error("Expected non-empty request body")
			}
		})
	}
}

func authToken(req *http.Request) string {
	return strings.TrimPrefix(req.Header.Get("Authorization"), "Token ")
}
