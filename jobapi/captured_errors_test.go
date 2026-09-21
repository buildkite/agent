package jobapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/google/go-cmp/cmp"
)

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
		{name: "server truncates message", body: `{"code":"x","message":"` + strings.Repeat("x", 4097) + `"}`, token: token, want: http.StatusCreated},
		{name: "non-object context", body: `{"code":"x","message":"diagnostic","context":[]}`, token: token, want: http.StatusBadRequest},
		{name: "oversized body", body: `{"code":"x","message":"diagnostic","context":{"large":"` + strings.Repeat("x", 16<<10) + `"}}`, token: token, want: http.StatusBadRequest},
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
