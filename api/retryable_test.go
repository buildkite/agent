package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"syscall"
	"testing"

	"github.com/buildkite/roko"
)

func TestIsRetryableError(t *testing.T) {
	t.Parallel()

	http2ConnLost := errors.New("http2: client connection lost")

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "http2 client connection lost",
			err:  http2ConnLost,
			want: true,
		},
		{
			name: "http2 client connection lost wrapped in url.Error",
			err: &url.Error{
				Op:  "Put",
				URL: "https://agent.buildkite.com/v3/jobs/abc/finish",
				Err: http2ConnLost,
			},
			want: true,
		},
		{
			name: "connection refused",
			err:  syscall.ECONNREFUSED,
			want: true,
		},
		{
			name: "use of closed network connection via url.Error",
			err: &url.Error{
				Op:  "Put",
				URL: "https://example.com",
				Err: errors.New("use of closed network connection"),
			},
			want: true,
		},
		{
			name: "non-retryable application error",
			err:  errors.New("something went wrong"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRetryableError(tt.err); got != tt.want {
				t.Errorf("IsRetryableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestBreakOnNonRetryable_HTTP2ClientConnectionLost(t *testing.T) {
	t.Parallel()

	retrier := roko.NewRetrier(roko.WithMaxAttempts(3))
	err := fmt.Errorf(`Put "https://agent.buildkite.com/v3/jobs/abc/finish": %w`,
		errors.New("http2: client connection lost"))

	if BreakOnNonRetryable(retrier, nil, err) {
		t.Fatal("BreakOnNonRetryable(...) = true, want false for http2: client connection lost")
	}
}

func TestBreakOnNonRetryable_NonRetryableStatus(t *testing.T) {
	t.Parallel()

	retrier := roko.NewRetrier(roko.WithMaxAttempts(3))
	resp := &Response{Response: &http.Response{StatusCode: http.StatusUnprocessableEntity}}

	if !BreakOnNonRetryable(retrier, resp, errors.New("unprocessable")) {
		t.Fatal("BreakOnNonRetryable(...) = false, want true for 422")
	}
}

func TestBreakOnNonRetryable_NonRetryable4xx(t *testing.T) {
	t.Parallel()

	retrier := roko.NewRetrier(roko.WithMaxAttempts(3))
	resp := &Response{Response: &http.Response{StatusCode: http.StatusForbidden}}

	if !BreakOnNonRetryable(retrier, resp, errors.New("forbidden")) {
		t.Fatal("BreakOnNonRetryable(...) = false, want true for 403")
	}
}
