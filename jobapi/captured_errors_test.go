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
)

func TestCapturedErrorURLCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, message, want string
		secrets             []string
	}{
		{
			name:    "unregistered URL credentials",
			message: "fatal: https://user:unregistered@example.com/repo and ssh://other-token@host/path\n",
			want:    "fatal: https://xxxxx@example.com/repo and ssh://xxxxx@host/path\n",
		},
		{
			name:    "registered multiline secret containing URL",
			message: "fatal: https://user:password@example.com/repo\nprivate suffix\n",
			secrets: []string{"https://user:password@example.com/repo\nprivate suffix"},
			want:    "fatal: [REDACTED]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reported *jobapi.CapturedError
			redactors := replacer.NewMux(redact.New(io.Discard, tc.secrets))
			srv, token, err := testServer(t, testEnviron(), redactors, jobapi.WithCapturedErrorReporter(func(_ context.Context, report *jobapi.CapturedError) error {
				reported = report
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if err := srv.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = srv.Stop() })
			body, err := json.Marshal(jobapi.CapturedError{Code: "git_fetch_failed", Message: tc.message})
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://job/api/current-job/v0/errors", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := testSocketClient(srv.SocketPath).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			var response jobapi.CapturedError
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusCreated || reported == nil || reported.Message != tc.want || response.Message != tc.want {
				t.Fatalf("status=%d, forwarded=%+v, response=%+v; want message %q in both", resp.StatusCode, reported, response, tc.want)
			}
		})
	}
}

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
		name, body, wantError, wantMessage string
		wantWarning                        bool
	}{
		{
			name:        "response is also redacted",
			body:        `{"code":"x","message":"Failed alpha-secret"}`,
			wantMessage: "Failed [REDACTED]",
		},
		{
			name:      "redaction expands code beyond schema limit",
			body:      `{"code":"` + strings.Repeat("q", 26) + `","message":"Failed"}`,
			wantError: "redacting captured error: code must be a nonblank string of at most 255 bytes without NUL",
		},
		{
			name:        "redaction leaves exactly 1000 characters",
			body:        `{"code":"x","message":"` + strings.Repeat("é", 990) + `q"}`,
			wantMessage: strings.Repeat("é", 990) + "[REDACTED]",
		},
		{
			name:        "redaction pushes the message over 1000 characters",
			body:        `{"code":"x","message":"` + strings.Repeat("é", 991) + `q"}`,
			wantMessage: strings.Repeat("é", 988) + "…[truncated]",
			wantWarning: true,
		},
		{
			name:        "redaction shrinks an overlong message below the limit",
			body:        `{"code":"x","message":"` + strings.Repeat("alpha-secret", 90) + `"}`,
			wantMessage: strings.Repeat("[REDACTED]", 90),
		},
		{
			name:        "secret crossing the cut is redacted before truncation",
			body:        `{"code":"x","message":"` + strings.Repeat("x", 985) + "alpha-secret" + strings.Repeat("z", 30) + `"}`,
			wantMessage: strings.Repeat("x", 985) + "[RE…[truncated]",
			wantWarning: true,
		},
		{
			name:        "redaction expansion is shortened before forwarding",
			body:        `{"code":"x","message":"` + strings.Repeat("q", 4000) + `"}`,
			wantMessage: strings.Repeat("[REDACTED]", 98) + "[REDACTE…[truncated]",
			wantWarning: true,
		},
		{
			name:        "redacting the code does not reduce the message allowance",
			body:        `{"code":"q","message":"` + strings.Repeat("x", 1000) + `"}`,
			wantMessage: strings.Repeat("x", 1000),
		},
		{
			// 63 URLs fit in 1000 characters, but masking lengthens each by 4.
			name:        "URL masking pushes the message over 1000 characters",
			body:        `{"code":"x","message":"` + strings.Repeat("https://a@b ", 63) + `"}`,
			wantMessage: strings.Repeat("https://xxxxx@b ", 63)[:988] + "…[truncated]",
			wantWarning: true,
		},
		{
			name:        "URL query strings are masked",
			body:        `{"code":"x","message":"GET https://a.example/x?sig=unregistered failed"}`,
			wantMessage: "GET https://a.example/x?[REDACTED] failed",
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
				var response jobapi.CapturedErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
					t.Fatal(err)
				}
				if response.Message != test.wantMessage {
					t.Errorf("message = %q, want %q", response.Message, test.wantMessage)
				}
				if (response.Warning != "") != test.wantWarning {
					t.Errorf("warning = %q, want warning = %t", response.Warning, test.wantWarning)
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

func TestCapturedErrorRedactsBuildkiteTokensWithoutRegisteredSecrets(t *testing.T) {
	t.Parallel()

	var reported *jobapi.CapturedError
	srv, token, err := testServer(t, testEnviron(), replacer.NewMux(), jobapi.WithCapturedErrorReporter(func(_ context.Context, capturedError *jobapi.CapturedError) error {
		reported = capturedError
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	bkToken := "bkua_" + strings.Repeat("x9Y8", 10)
	body, err := json.Marshal(map[string]any{
		"code":    "x",
		"message": "Request failed with " + bkToken + " (short body: bkua_encoded-token)",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/errors", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := testSocketClient(srv.SocketPath).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	if reported == nil {
		t.Fatal("reporter was not called")
	}
	if got, want := reported.Message, "Request failed with [REDACTED] (short body: bkua_encoded-token)"; got != want {
		t.Errorf("reported message = %q, want %q", got, want)
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

	payload := `{"code":"container.process_failed","message":"Container command failed"}`
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
		{name: "NUL message", body: `{"code":"x","message":"a\u0000b"}`, token: token, want: http.StatusBadRequest},
		{name: "year zero", body: `{"code":"x","message":"diagnostic","timestamp":"0000-12-31T23:59:59Z"}`, token: token, want: http.StatusBadRequest},
		{name: "UTC year underflow", body: `{"code":"x","message":"diagnostic","timestamp":"0001-01-01T00:00:00+01:00"}`, token: token, want: http.StatusBadRequest},
		{name: "UTC year overflow", body: `{"code":"x","message":"diagnostic","timestamp":"9999-12-31T23:59:59-01:00"}`, token: token, want: http.StatusBadRequest},
		{name: "future timestamp", body: `{"code":"x","message":"diagnostic","timestamp":"9999-12-31T23:59:59Z"}`, token: token, want: http.StatusCreated},
		{name: "NUL in discarded tail", body: `{"code":"x","message":"` + strings.Repeat("x", 1001) + `\u0000"}`, token: token, want: http.StatusBadRequest},
		{name: "context is unsupported", body: `{"code":"x","message":"diagnostic","context":{"detail":"x"}}`, token: token, want: http.StatusBadRequest},
		{name: "oversized body", body: `{"code":"x","message":"` + strings.Repeat("x", 32<<10) + `"}`, token: token, want: http.StatusRequestEntityTooLarge},
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

func TestCapturedErrorMessageCharacterLimit(t *testing.T) {
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

	message := strings.Repeat("🧪", 1000)
	for _, test := range []struct {
		name, code, message, wantMessage string
	}{
		{"999 characters", "x", strings.Repeat("🧪", 999), strings.Repeat("🧪", 999)},
		{"1000 characters", "x", message, message},
		{"1001 characters", "x", message + "🧪", strings.Repeat("🧪", 988) + "…[truncated]"},
		{"1001 ASCII characters", "x", strings.Repeat("x", 1001), strings.Repeat("x", 988) + "…[truncated]"},
		{"code has a separate allowance", strings.Repeat("é", 127) + "x", message, message},
		{"JSON escapes do not count as extra characters", "x", strings.Repeat("\n", 999) + "x", strings.Repeat("\n", 999) + "x"},
		{"combining marks each count as a character", "x", strings.Repeat("e\u0301", 500), strings.Repeat("e\u0301", 500)},
		{"1001 code points despite fewer visible characters", "x", strings.Repeat("e\u0301", 500) + "x", strings.Repeat("e\u0301", 494) + "…[truncated]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := map[string]any{"code": test.code, "message": test.message}
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest(http.MethodPost, "http://job/api/current-job/v0/errors", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := testSocketClient(srv.SocketPath).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("status = %d, want 201", resp.StatusCode)
			}
			var response jobapi.CapturedErrorResponse
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if response.Message != test.wantMessage {
				t.Errorf("response message = %q, want %q", response.Message, test.wantMessage)
			}
			if wantWarning := test.message != test.wantMessage; (response.Warning != "") != wantWarning {
				t.Errorf("warning = %q, want warning = %t", response.Warning, wantWarning)
			}
			select {
			case payload := <-reported:
				if payload.Code != test.code || payload.Message != test.wantMessage {
					t.Errorf("forwarded report = %+v, want code %q and message %q", payload, test.code, test.wantMessage)
				}
			default:
				t.Error("report was not forwarded")
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

	message := strings.Repeat("é", 700)
	body := `{"code":"x","message":"` + message + `"}`
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
				if payload.Message != message {
					t.Error("message was changed before forwarding")
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

func TestCapturedErrorMessage(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name, detail, want string
	}{
		{name: "detail", detail: "fatal: bad ref\n", want: "fatal: bad ref\n"},
		{name: "detail at limit", detail: strings.Repeat("é", jobapi.MaxCapturedErrorDetail), want: strings.Repeat("é", jobapi.MaxCapturedErrorDetail)},
		{name: "detail over limit", detail: strings.Repeat("é", jobapi.MaxCapturedErrorDetail+1), want: "fallback"},
		{name: "blank", detail: " \n", want: "fallback"},
		{name: "invalid UTF-8", detail: "bad \xff", want: "fallback"},
		{name: "NUL", detail: "bad \x00", want: "fallback"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := jobapi.CapturedErrorMessage(test.detail, "fallback"); got != test.want {
				t.Errorf("CapturedErrorMessage() = %.40q, want %.40q", got, test.want)
			}
		})
	}
}
