package clicommand

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwt"
	"github.com/urfave/cli/v3"
)

func newOIDCRequestTokenTestServer(t *testing.T, jobID, token string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		wantPath := fmt.Sprintf("/jobs/%s/oidc/tokens", url.PathEscape(jobID))
		if req.URL.Path != wantPath {
			http.Error(rw, fmt.Sprintf("got path %q, want %q", req.URL.Path, wantPath), http.StatusNotFound)
			return
		}

		_, _ = fmt.Fprintf(rw, `{"token":%q}`, token)
	}))
}

func runOIDCRequestTokenCommand(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	// Clone the command, because Run mutates it, which breaks the command completeness test.
	cmd := *OIDCRequestTokenCommand
	var out bytes.Buffer
	app := &cli.Command{
		Name:      "buildkite-agent",
		Commands:  []*cli.Command{&cmd},
		Writer:    &out,
		ErrWriter: os.Stderr,
	}

	runArgs := []string{
		"buildkite-agent",
		"request-token",
		"--endpoint", serverURL,
		"--agent-access-token", "agent-access-token",
		"--job", "job-123",
	}
	runArgs = append(runArgs, args...)

	err := app.Run(t.Context(), runArgs)
	return out.String(), err
}

func TestOIDCRequestToken(t *testing.T) {
	const oidcToken = "oidc-token"

	server := newOIDCRequestTokenTestServer(t, "job-123", oidcToken)
	defer server.Close()

	t.Run("requires explicit opt out when redaction cannot be set up", func(t *testing.T) {
		t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
		t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")

		out, err := runOIDCRequestTokenCommand(t, server.URL)
		if err == nil {
			t.Fatal("runOIDCRequestTokenCommand() error = nil, want non-nil")
		}

		if out != "" {
			t.Fatalf("runOIDCRequestTokenCommand() output = %q, want empty", out)
		}

		for _, want := range []string{
			"automatic OIDC token redaction requires the Job API",
			"could leak in logs without redaction",
			"--skip-redaction",
			"BUILDKITE_AGENT_OIDC_REQUEST_TOKEN_SKIP_TOKEN_REDACTION=true",
		} {
			if got := err.Error(); !strings.Contains(got, want) {
				t.Fatalf("runOIDCRequestTokenCommand() error = %q, want containing %q", got, want)
			}
		}
	})

	t.Run("prints token when redaction is explicitly skipped", func(t *testing.T) {
		t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
		t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")

		out, err := runOIDCRequestTokenCommand(t, server.URL, "--skip-redaction")
		if err != nil {
			t.Fatalf("runOIDCRequestTokenCommand() error = %v, want nil", err)
		}

		if want := oidcToken + "\n"; out != want {
			t.Fatalf("runOIDCRequestTokenCommand() output = %q, want %q", out, want)
		}
	})
}

// A non-retryable 4xx means the API positively refused to issue a token, so
// the command must not retry, and must exit with the documented distinct
// status so callers (e.g. test-engine-client) can tell a refusal apart from a
// transient failure without parsing stderr.
func TestOIDCRequestTokenRefusalExitsWithDistinctStatus(t *testing.T) {
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		requests.Add(1)
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = fmt.Fprint(rw, `{"message":"audience not allowed"}`)
	}))
	defer server.Close()

	_, err := runOIDCRequestTokenCommand(t, server.URL)
	if err == nil {
		t.Fatal("runOIDCRequestTokenCommand() error = nil, want non-nil")
	}

	eerr := new(ExitError)
	if !errors.As(err, &eerr) {
		t.Fatalf("runOIDCRequestTokenCommand() error = %v (%T), want ExitError", err, err)
	}
	if got, want := eerr.Code(), OIDCTokenRefusedExitStatus; got != want {
		t.Errorf("ExitError.Code() = %d, want %d", got, want)
	}
	if got, want := err.Error(), "audience not allowed"; !strings.Contains(got, want) {
		t.Errorf("error = %q, want containing %q", got, want)
	}

	if got, want := requests.Load(), int32(1); got != want {
		t.Errorf("server received %d requests, want %d (refusals must not be retried)", got, want)
	}
}

func TestOIDCTokenRefused(t *testing.T) {
	errorResponse := func(status int) error {
		return &api.ErrorResponse{
			Response: &http.Response{
				StatusCode: status,
				Request: &http.Request{
					Method: "POST",
					URL:    &url.URL{Scheme: "https", Host: "agent.buildkite.com", Path: "/v3/jobs/job-123/oidc/tokens"},
				},
			},
			Message: "message",
		}
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "400 bad request", err: errorResponse(http.StatusBadRequest), want: true},
		{name: "401 unauthorized", err: errorResponse(http.StatusUnauthorized), want: true},
		{name: "403 forbidden", err: errorResponse(http.StatusForbidden), want: true},
		{name: "404 not found", err: errorResponse(http.StatusNotFound), want: true},
		{name: "410 gone", err: errorResponse(http.StatusGone), want: true},
		{name: "422 unprocessable", err: errorResponse(http.StatusUnprocessableEntity), want: true},
		{name: "wrapped 422", err: fmt.Errorf("could not obtain OIDC token: %w", errorResponse(http.StatusUnprocessableEntity)), want: true},
		{name: "408 request timeout", err: errorResponse(http.StatusRequestTimeout), want: false},
		{name: "421 misdirected request", err: errorResponse(http.StatusMisdirectedRequest), want: false},
		{name: "425 too early", err: errorResponse(http.StatusTooEarly), want: false},
		{name: "429 too many requests", err: errorResponse(http.StatusTooManyRequests), want: false},
		{name: "unlisted 4xx (418)", err: errorResponse(http.StatusTeapot), want: false},
		{name: "500 internal server error", err: errorResponse(http.StatusInternalServerError), want: false},
		{name: "504 gateway timeout", err: errorResponse(http.StatusGatewayTimeout), want: false},
		{name: "transport error", err: errors.New("net/http: request canceled"), want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := oidcTokenRefused(test.err); got != test.want {
				t.Errorf("oidcTokenRefused(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}

// An intermediary in front of the API can answer with an HTML error page and a
// 2xx status. That used to fail the job on the first attempt, because a success
// status was read as "settled", so the retrier broke instead of retrying.
func TestOIDCRequestTokenRetriesUndecodableResponse(t *testing.T) {
	t.Setenv("BUILDKITE_AGENT_JOB_API_SOCKET", "")
	t.Setenv("BUILDKITE_AGENT_JOB_API_TOKEN", "")

	const oidcToken = "oidc-token"
	var requests atomic.Int32

	// The first attempt gets a gateway error page with a 200; the second gets the
	// real thing. Note the retrier's first backoff is a real 1s sleep.
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		if requests.Add(1) == 1 {
			rw.Header().Set("Content-Type", "text/html")
			rw.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(rw, "<html><head><title>502 Bad Gateway</title></head></html>")
			return
		}
		_, _ = fmt.Fprintf(rw, `{"token":%q}`, oidcToken)
	}))
	defer server.Close()

	out, err := runOIDCRequestTokenCommand(t, server.URL, "--skip-redaction")
	if err != nil {
		t.Fatalf("runOIDCRequestTokenCommand() error = %v, want nil", err)
	}

	if want := oidcToken + "\n"; out != want {
		t.Fatalf("runOIDCRequestTokenCommand() output = %q, want %q", out, want)
	}

	if got, want := requests.Load(), int32(2); got != want {
		t.Errorf("server received %d requests, want %d", got, want)
	}
}

func TestOIDCRequestTokenRetryAfter(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		retryAfter string
		wantDelay  time.Duration
	}{
		{name: "overloaded proxy", status: http.StatusServiceUnavailable, retryAfter: "3", wantDelay: 3 * time.Second},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "3", wantDelay: 3 * time.Second},
		{name: "invalid header", status: http.StatusServiceUnavailable, retryAfter: "invalid", wantDelay: time.Second},
		{name: "negative delay", status: http.StatusServiceUnavailable, retryAfter: "-1", wantDelay: time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			firstRequest := make(chan time.Time, 1)
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if requests.Add(1) == 1 {
					firstRequest <- time.Now()
					rw.Header().Set("Retry-After", tt.retryAfter)
					http.Error(rw, "envoy overloaded", tt.status)
					return
				}
				if delay := time.Since(<-firstRequest); delay < tt.wantDelay {
					t.Errorf("retried after %s, want at least %s", delay, tt.wantDelay)
				}
				_, _ = fmt.Fprint(rw, `{"token":"oidc-token"}`)
			}))
			defer server.Close()

			out, err := runOIDCRequestTokenCommand(t, server.URL, "--skip-redaction")
			if err != nil {
				t.Fatalf("runOIDCRequestTokenCommand() error = %v, want nil", err)
			}
			if want := "oidc-token\n"; out != want {
				t.Errorf("output = %q, want %q", out, want)
			}
			if got := requests.Load(); got != 2 {
				t.Errorf("server received %d requests, want 2", got)
			}
		})
	}
}

func TestOIDCRequestTokenFailureIsCaptured(t *testing.T) {
	reports := startAgentErrorTestServer(t)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(rw, `{"message":"audience not allowed"}`)
	}))
	defer server.Close()

	if _, err := runOIDCRequestTokenCommand(t, server.URL, "--audience", "sts.example.com"); err == nil {
		t.Fatal("runOIDCRequestTokenCommand() error = nil, want refusal")
	}

	if len(*reports) != 1 {
		t.Fatalf("reports = %+v, want one", *reports)
	}
	report := (*reports)[0]
	for _, want := range []string{"for audience sts.example.com", "403 Forbidden: audience not allowed", "Buildkite refused the request"} {
		if report.Code != "buildkite.oidc_token_request_failed" || !strings.Contains(report.Message, want) {
			t.Errorf("report = %q %q, want buildkite.oidc_token_request_failed containing %q", report.Code, report.Message, want)
		}
	}
}

func TestOIDCTokenMaxLifetime(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "unset", value: "", want: 0},
		{name: "no limit", value: "0", want: 0},
		{name: "limit", value: "600", want: 600},
		{name: "negative", value: "-1", wantErr: true},
		{name: "not a number", value: "abc", wantErr: true},
		{name: "duration", value: "10m", wantErr: true},
		{name: "padded", value: " 60", wantErr: true},
		{name: "overflow", value: "99999999999999999999", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(OIDCTokenMaxLifetimeEnvVar, test.value)

			got, err := oidcTokenMaxLifetime()
			if test.wantErr {
				if err == nil {
					t.Fatalf("oidcTokenMaxLifetime() = (%d, nil), want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("oidcTokenMaxLifetime() error = %v, want nil", err)
			}
			if got != test.want {
				t.Errorf("oidcTokenMaxLifetime() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestOIDCTokenRequestLifetime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		requested   int
		maxLifetime int
		want        int
		wantErr     bool
	}{
		{name: "no limit, default lifetime", requested: 0, maxLifetime: 0, want: 0},
		{name: "no limit, requested lifetime", requested: 86400, maxLifetime: 0, want: 86400},
		{name: "under the limit", requested: 60, maxLifetime: 600, want: 60},
		{name: "at the limit", requested: 600, maxLifetime: 600, want: 600},
		{name: "over the limit", requested: 601, maxLifetime: 600, wantErr: true},
		{name: "far over the limit", requested: 9223372036854775807, maxLifetime: 600, wantErr: true},
		{name: "default lifetime over a short limit", requested: 0, maxLifetime: 60, want: 60},
		{name: "default lifetime over a limit of 1", requested: 0, maxLifetime: 1, want: 1},
		{name: "default lifetime just over the limit", requested: 0, maxLifetime: oidcTokenDefaultLifetime - 1, want: oidcTokenDefaultLifetime - 1},
		{name: "default lifetime at the limit", requested: 0, maxLifetime: oidcTokenDefaultLifetime, want: 0},
		{name: "default lifetime under a long limit", requested: 0, maxLifetime: 3600, want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := oidcTokenRequestLifetime(test.requested, test.maxLifetime)
			if test.wantErr {
				if err == nil {
					t.Fatalf("oidcTokenRequestLifetime(%d, %d) = (%d, nil), want error", test.requested, test.maxLifetime, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("oidcTokenRequestLifetime(%d, %d) error = %v, want nil", test.requested, test.maxLifetime, err)
			}
			if got != test.want {
				t.Errorf("oidcTokenRequestLifetime(%d, %d) = %d, want %d", test.requested, test.maxLifetime, got, test.want)
			}
		})
	}
}

// signedOIDCTestToken returns a signed JWT with the given lifetime, or without
// the iat or exp claim when issuedAt or expiry is the zero time.
func signedOIDCTestToken(t *testing.T, issuedAt, expiry time.Time) string {
	t.Helper()

	builder := jwt.NewBuilder().Audience([]string{"sts.amazonaws.com"})
	if !issuedAt.IsZero() {
		builder = builder.IssuedAt(issuedAt)
	}
	if !expiry.IsZero() {
		builder = builder.Expiration(expiry)
	}
	token, err := builder.Build()
	if err != nil {
		t.Fatalf("building test token: %v", err)
	}
	signed, err := jwt.Sign(token, jwt.WithKey(jwa.HS256(), []byte("test-signing-key")))
	if err != nil {
		t.Fatalf("signing test token: %v", err)
	}
	return string(signed)
}

func TestCheckOIDCTokenLifetime(t *testing.T) {
	t.Parallel()

	issuedAt := time.Unix(1_800_000_000, 0)

	tests := []struct {
		name        string
		token       string
		maxLifetime int
		wantErr     bool
	}{
		{name: "no limit skips the check", token: "not-a-jwt", maxLifetime: 0},
		{name: "under the limit", token: signedOIDCTestToken(t, issuedAt, issuedAt.Add(time.Minute)), maxLifetime: 300},
		{name: "at the limit", token: signedOIDCTestToken(t, issuedAt, issuedAt.Add(5*time.Minute)), maxLifetime: 300},
		{name: "over the limit", token: signedOIDCTestToken(t, issuedAt, issuedAt.Add(5*time.Minute+time.Second)), maxLifetime: 300, wantErr: true},
		{name: "expired when issued", token: signedOIDCTestToken(t, issuedAt, issuedAt.Add(-time.Minute)), maxLifetime: 300},
		{name: "no iat claim", token: signedOIDCTestToken(t, time.Time{}, issuedAt.Add(time.Minute)), maxLifetime: 300, wantErr: true},
		{name: "no exp claim", token: signedOIDCTestToken(t, issuedAt, time.Time{}), maxLifetime: 300, wantErr: true},
		{name: "not a JWT", token: "not-a-jwt", maxLifetime: 300, wantErr: true},
		{name: "empty token", token: "", maxLifetime: 300, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := checkOIDCTokenLifetime(test.token, test.maxLifetime)
			if gotErr := err != nil; gotErr != test.wantErr {
				t.Errorf("checkOIDCTokenLifetime(%q, %d) error = %v, want error: %t", test.token, test.maxLifetime, err, test.wantErr)
			}
		})
	}
}

// newOIDCLifetimeTestServer returns a server that records the lifetime of each
// token request and issues a token valid for issuedLifetime, or for the
// requested lifetime (or 5 minutes, the API's default) when issuedLifetime is 0.
func newOIDCLifetimeTestServer(t *testing.T, issuedLifetime time.Duration, requestedLifetimes *[]int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		var body struct {
			Lifetime *int `json:"lifetime"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}

		lifetime := issuedLifetime
		requested := 0
		if body.Lifetime != nil {
			requested = *body.Lifetime
		}
		*requestedLifetimes = append(*requestedLifetimes, requested)
		if lifetime == 0 {
			lifetime = oidcTokenDefaultLifetime * time.Second
			if requested > 0 {
				lifetime = time.Duration(requested) * time.Second
			}
		}

		issuedAt := time.Now().Truncate(time.Second)
		_, _ = fmt.Fprintf(rw, `{"token":%q}`, signedOIDCTestToken(t, issuedAt, issuedAt.Add(lifetime)))
	}))
}

func TestOIDCRequestTokenMaxLifetime(t *testing.T) {
	tests := []struct {
		name           string
		maxLifetime    string
		args           []string
		issuedLifetime time.Duration
		wantRequests   []int
		wantErr        string
	}{
		{
			name:         "no limit passes the requested lifetime through",
			maxLifetime:  "0",
			args:         []string{"--lifetime", "86400"},
			wantRequests: []int{86400},
		},
		{
			name:         "unset limit passes the requested lifetime through",
			maxLifetime:  "",
			args:         []string{"--lifetime", "86400"},
			wantRequests: []int{86400},
		},
		{
			name:         "lifetime under the limit",
			maxLifetime:  "600",
			args:         []string{"--lifetime", "60"},
			wantRequests: []int{60},
		},
		{
			name:         "lifetime at the limit",
			maxLifetime:  "600",
			args:         []string{"--lifetime", "600"},
			wantRequests: []int{600},
		},
		{
			name:        "lifetime over the limit is rejected without a request",
			maxLifetime: "600",
			args:        []string{"--lifetime", "601"},
			wantErr:     "lifetime 601 exceeds this agent's maximum OIDC token lifetime of 600 seconds",
		},
		{
			name:         "default lifetime over the limit requests the limit",
			maxLifetime:  "60",
			wantRequests: []int{60},
		},
		{
			name:         "default lifetime under the limit is left to the API",
			maxLifetime:  "3600",
			wantRequests: []int{0},
		},
		{
			name:           "token issued for longer than the limit is not printed",
			maxLifetime:    "3600",
			issuedLifetime: 2 * time.Hour,
			wantRequests:   []int{0},
			wantErr:        "exceeds this agent's maximum OIDC token lifetime of 3600 seconds",
		},
		{
			name:        "invalid limit fails closed without a request",
			maxLifetime: "abc",
			args:        []string{"--lifetime", "60"},
			wantErr:     "is not a valid maximum OIDC token lifetime",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(OIDCTokenMaxLifetimeEnvVar, test.maxLifetime)

			var requests []int
			server := newOIDCLifetimeTestServer(t, test.issuedLifetime, &requests)
			defer server.Close()

			args := append([]string{"--skip-redaction"}, test.args...)
			out, err := runOIDCRequestTokenCommand(t, server.URL, args...)

			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("runOIDCRequestTokenCommand() error = nil, want error containing %q", test.wantErr)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Errorf("runOIDCRequestTokenCommand() error = %q, want containing %q", err, test.wantErr)
				}
				if out != "" {
					t.Errorf("runOIDCRequestTokenCommand() output = %q, want empty", out)
				}
			} else {
				if err != nil {
					t.Fatalf("runOIDCRequestTokenCommand() error = %v, want nil", err)
				}
				if strings.Count(out, ".") != 2 {
					t.Errorf("runOIDCRequestTokenCommand() output = %q, want a JWT", out)
				}
			}

			if !slices.Equal(requests, test.wantRequests) {
				t.Errorf("requested lifetimes = %v, want %v", requests, test.wantRequests)
			}
		})
	}
}
