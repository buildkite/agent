package jobapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/socket"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/google/go-cmp/cmp"
)

func TestCapturedErrorRedactionResponse(t *testing.T) {
	t.Parallel()

	var reported atomic.Int32
	redactors := replacer.NewMux(redact.New(io.Discard, []string{"alpha-secret", "beta-secret", "q"}))
	srv, token, err := testServer(t, testEnviron(), redactors, jobapi.WithCapturedErrorReporter(func(context.Context, *jobapi.CapturedError) error {
		reported.Add(1)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	for _, test := range []struct {
		name, body, wantError string
	}{
		{
			name: "response is also redacted",
			body: `{"code":"x","message":"Failed alpha-secret","context":{"details":"beta-secret"}}`,
		},
		{
			name:      "two secret keys collide",
			body:      `{"code":"x","message":"Failed","context":{"nested":[{"alpha-secret":1,"beta-secret":2}]}}`,
			wantError: "redacting captured error: context keys collide after redaction",
		},
		{
			name:      "secret key collides with existing marker",
			body:      `{"code":"x","message":"Failed","context":{"alpha-secret":1,"[REDACTED]":2}}`,
			wantError: "redacting captured error: context keys collide after redaction",
		},
		{
			name:      "redaction expands code beyond schema limit",
			body:      `{"code":"` + strings.Repeat("q", 26) + `","message":"Failed"}`,
			wantError: "redacting captured error: code must be a nonblank string of at most 255 bytes without NUL",
		},
		{
			name:      "redaction expands report beyond body limit",
			body:      `{"code":"x","message":"` + strings.Repeat("q", 3300) + `"}`,
			wantError: "redacting captured error: captured error exceeds 32768 bytes after redaction",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := reported.Load()
			req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/errors", strings.NewReader(test.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := testSocketClient(srv.SocketPath).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			wantStatus := http.StatusCreated
			if test.wantError != "" {
				wantStatus = http.StatusUnprocessableEntity
			}
			if resp.StatusCode != wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, wantStatus)
			}
			if test.wantError != "" {
				var response socket.ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
					t.Fatal(err)
				}
				if response.Error != test.wantError {
					t.Errorf("error = %q, want %q", response.Error, test.wantError)
				}
			} else {
				var response jobapi.CapturedError
				if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
					t.Fatal(err)
				}
				if response.Message != "Failed [REDACTED]" || response.Context["details"] != "[REDACTED]" {
					t.Errorf("response not redacted: %+v", response)
				}
			}
			wantCalls := int32(1)
			if test.wantError != "" {
				wantCalls = 0
			}
			if got := reported.Load() - before; got != wantCalls {
				t.Errorf("forwarded reports = %d, want %d", got, wantCalls)
			}
		})
	}
}

func TestCapturedErrorIsAuthenticatedAndNormalized(t *testing.T) {
	t.Parallel()

	redactors := replacer.NewMux()
	var reported *jobapi.CapturedError
	srv, token, err := testServer(t, testEnviron(), redactors, jobapi.WithCapturedErrorReporter(func(_ context.Context, capturedError *jobapi.CapturedError) error {
		reported = capturedError
		return nil
	}))
	if err != nil {
		t.Fatalf("testServer() error = %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("srv.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	payload := `{"code":"container.process_failed","message":"Container command failed","context":{"plugin":"docker","image":"example:latest","exit_status":7,"Docker.Detail":{"a":{"b":{"c":{"d":[true,42]}}}}}}`
	req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/errors", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := testSocketClient(srv.SocketPath).Do(req)
	if err != nil {
		t.Fatalf("client.Do(req) error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("response status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if reported == nil {
		t.Fatal("reporter was not called")
	}
	if reported.Code != "container.process_failed" {
		t.Errorf("reported code = %q", reported.Code)
	}
	if got, want := reported.Message, "Container command failed"; got != want {
		t.Errorf("reported message = %q, want %q", got, want)
	}
	wantContext := map[string]any{
		"plugin":      "docker",
		"image":       "example:latest",
		"exit_status": json.Number("7"),
		"Docker.Detail": map[string]any{"a": map[string]any{"b": map[string]any{
			"c": map[string]any{"d": []any{true, json.Number("42")}},
		}}},
	}
	if diff := cmp.Diff(wantContext, reported.Context); diff != "" {
		t.Errorf("reported context diff (-want +got):\n%s", diff)
	}
	if reported.Timestamp == nil || time.Since(*reported.Timestamp) > time.Minute {
		t.Errorf("reported timestamp = %v, want recent parent timestamp", reported.Timestamp)
	}
	if reported.IdempotencyKey == "" || len(reported.IdempotencyKey) > 255 {
		t.Errorf("idempotency key length = %d, want 1–255", len(reported.IdempotencyKey))
	}
}

func TestCapturedErrorRejectsMalformedAndUnauthenticatedRequests(t *testing.T) {
	t.Parallel()

	srv, token, err := testServer(t, testEnviron(), replacer.NewMux(), jobapi.WithCapturedErrorReporter(func(context.Context, *jobapi.CapturedError) error {
		return nil
	}))
	if err != nil {
		t.Fatalf("testServer() error = %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("srv.Start() error = %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	for _, test := range []struct {
		name  string
		body  string
		token string
		want  int
	}{
		{name: "unauthenticated", body: `{"code":"x","message":"diagnostic"}`, want: http.StatusUnauthorized},
		{name: "invalid JSON", body: `{`, token: token, want: http.StatusBadRequest},
		{name: "null context", body: `{"code":"x","message":"diagnostic","context":null}`, token: token, want: http.StatusBadRequest},
		{name: "NUL message", body: `{"code":"x","message":"a\u0000b"}`, token: token, want: http.StatusBadRequest},
		{name: "year zero", body: `{"code":"x","message":"diagnostic","timestamp":"0000-12-31T23:59:59Z"}`, token: token, want: http.StatusBadRequest},
		{name: "UTC year underflow", body: `{"code":"x","message":"diagnostic","timestamp":"0001-01-01T00:00:00+01:00"}`, token: token, want: http.StatusBadRequest},
		{name: "UTC year overflow", body: `{"code":"x","message":"diagnostic","timestamp":"9999-12-31T23:59:59-01:00"}`, token: token, want: http.StatusBadRequest},
		{name: "future timestamp", body: `{"code":"x","message":"diagnostic","timestamp":"9999-12-31T23:59:59Z"}`, token: token, want: http.StatusCreated},
		{name: "long message", body: `{"code":"x","message":"` + strings.Repeat("x", 4097) + `"}`, token: token, want: http.StatusCreated},
		{name: "non-object context", body: `{"code":"x","message":"diagnostic","context":[]}`, token: token, want: http.StatusBadRequest},
		{name: "oversized body", body: `{"code":"x","message":"diagnostic","context":{"large":"` + strings.Repeat("x", 32<<10) + `"}}`, token: token, want: http.StatusRequestEntityTooLarge},
		{name: "missing message", body: `{"code":"x"}`, token: token, want: http.StatusBadRequest},
		{name: "NUL code", body: `{"code":"x\u0000","message":"diagnostic"}`, token: token, want: http.StatusBadRequest},
		{name: "unknown field", body: `{"code":"x","message":"diagnostic","raw":"unsafe"}`, token: token, want: http.StatusBadRequest},
		{name: "trailing JSON", body: `{"code":"x","message":"diagnostic"} {}`, token: token, want: http.StatusBadRequest},
		{name: "client correlation", body: `{"code":"x","message":"diagnostic","correlation_id":"chosen"}`, token: token, want: http.StatusBadRequest},
		{name: "client idempotency", body: `{"code":"x","message":"diagnostic","idempotency_key":"chosen"}`, token: token, want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/errors", bytes.NewBufferString(test.body))
			if err != nil {
				t.Fatalf("http.NewRequest() error = %v", err)
			}
			if test.token != "" {
				req.Header.Set("Authorization", "Bearer "+test.token)
			}
			resp, err := testSocketClient(srv.SocketPath).Do(req)
			if err != nil {
				t.Fatalf("client.Do(req) error = %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != test.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, test.want)
			}
		})
	}
}

func TestCapturedErrorBodyLimit(t *testing.T) {
	t.Parallel()

	reported := make(chan *jobapi.CapturedError, 1)
	srv, token, err := testServer(t, testEnviron(), replacer.NewMux(), jobapi.WithCapturedErrorReporter(func(_ context.Context, payload *jobapi.CapturedError) error {
		reported <- payload
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	// A 20 KiB message must be forwarded unchanged. Multibyte text makes the
	// byte-versus-character limit observable.
	message := strings.Repeat("é", 10<<10)
	body := `{"code":"x","message":"` + message + `","context":{"detail":"kept"}}`
	for _, test := range []struct {
		name string
		size int
		want int
	}{
		{"at limit", 32 << 10, http.StatusCreated},
		{"one byte over", (32 << 10) + 1, http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Padding exercises the body limit even after a complete JSON object.
			input := body + strings.Repeat(" ", test.size-len(body))
			req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/errors", strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			req.ContentLength = -1 // Direct callers need not declare a body length.
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := testSocketClient(srv.SocketPath).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != test.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, test.want)
			}
			if test.want == http.StatusRequestEntityTooLarge {
				var response socket.ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
					t.Fatal(err)
				}
				if response.Error != "captured error request exceeds 32768 bytes" {
					t.Errorf("error = %q, want explicit request limit", response.Error)
				}
			}
			select {
			case payload := <-reported:
				if test.want != http.StatusCreated {
					t.Fatal("oversized request was forwarded")
				}
				if payload.Message != message || payload.Context["detail"] != "kept" {
					t.Error("message or context was changed before forwarding")
				}
				if payload.Timestamp == nil || payload.IdempotencyKey == "" {
					t.Error("parent metadata was not added to the limit-sized request")
				}
			default:
				if test.want == http.StatusCreated {
					t.Error("accepted request was not forwarded")
				}
			}
		})
	}
}
