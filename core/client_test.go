package core_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/core"
	"github.com/buildkite/agent/v4/logger"
)

type registrationTransport func(*http.Request) (*http.Response, error)

func (f registrationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRegisterBackoff(t *testing.T) {
	var waits []time.Duration
	attempts := 0
	c := core.Client{
		Logger: logger.Discard,
		APIClient: api.NewClient(logger.Discard, api.Config{
			Endpoint: "https://agent.example/",
			HTTPClient: &http.Client{Transport: registrationTransport(func(req *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					return nil, &net.DNSError{Err: "temporary DNS failure", IsTemporary: true, IsTimeout: true}
				}
				status, body := http.StatusServiceUnavailable, `{"message":"unavailable"}`
				if attempts == 11 {
					status, body = http.StatusOK, `{"id":"registered-agent","name":"test"}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})},
		}),
		RetrySleepFunc: func(d time.Duration) { waits = append(waits, d) },
	}
	reg, err := c.Register(t.Context(), api.AgentRegisterRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if reg.UUID != "registered-agent" {
		t.Errorf("registered agent ID = %q, want registered-agent", reg.UUID)
	}
	// The first four retries grow exponentially; subsequent retries stay capped.
	windows := [][2]time.Duration{
		{time.Second, 2 * time.Second},
		{2 * time.Second, 4 * time.Second},
		{4 * time.Second, 8 * time.Second},
		{8 * time.Second, 16 * time.Second},
		{15 * time.Second, 30 * time.Second},
	}
	if len(waits) != 10 {
		t.Fatalf("retry waits = %d, want 10", len(waits))
	}
	for i, d := range waits {
		window := windows[min(i, len(windows)-1)]
		if d < window[0] || d >= window[1] {
			t.Errorf("retry %d wait = %s, want [%s, %s)", i+1, d, window[0], window[1])
		}
	}
	// Even at the cap, agents must not settle into a fixed retry interval.
	if slices.Min(waits[4:]) == slices.Max(waits[4:]) {
		t.Errorf("capped waits have no jitter: %v", waits[4:])
	}
}

func TestRegisterRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name   string
		header string
		status int
		min    time.Duration
		max    time.Duration
	}{
		{"rate limited", "120", http.StatusTooManyRequests, 121 * time.Second, 122 * time.Second},
		{"unavailable", "120", http.StatusServiceUnavailable, 121 * time.Second, 122 * time.Second},
		{"HTTP date", time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat), http.StatusServiceUnavailable, 121 * time.Second, 122 * time.Second},
		{"shorter than backoff", "1", http.StatusTooManyRequests, 2 * time.Second, 3 * time.Second},
		{"zero", "0", http.StatusTooManyRequests, time.Second, 2 * time.Second},
		{"invalid", "later", http.StatusTooManyRequests, time.Second, 2 * time.Second},
		{"invalid duration", "1m30", http.StatusTooManyRequests, time.Second, 2 * time.Second},
		{"negative", "-10", http.StatusTooManyRequests, time.Second, 2 * time.Second},
		{"past date", time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat), http.StatusTooManyRequests, time.Second, 2 * time.Second},
		{"unauthorized", "120", http.StatusUnauthorized, 0, 0},
		{"invalid registration", "120", http.StatusUnprocessableEntity, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			var waits []time.Duration
			c := core.Client{
				Logger: logger.Discard,
				APIClient: api.NewClient(logger.Discard, api.Config{
					Endpoint: "https://agent.example/",
					HTTPClient: &http.Client{Transport: registrationTransport(func(req *http.Request) (*http.Response, error) {
						attempts++
						status, body := test.status, `{"message":"registration failed"}`
						if attempts > 1 {
							status, body = http.StatusOK, `{"id":"registered-agent"}`
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {test.header}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
					})},
				}),
				RetrySleepFunc: func(d time.Duration) { waits = append(waits, d) },
			}
			started := time.Now()
			// Independent registrations given the same hint must still have jitter.
			for range 6 {
				attempts = 0
				reg, err := c.Register(t.Context(), api.AgentRegisterRequest{})
				if test.max == 0 {
					if err == nil || attempts != 1 || len(waits) != 0 {
						t.Fatalf("permanent failure: err = %v, attempts = %d, waits = %v", err, attempts, waits)
					}
					return
				}
				if err != nil || reg.UUID != "registered-agent" {
					t.Fatalf("Register = %v, %v, want successful registration", reg, err)
				}
			}
			if len(waits) != 6 {
				t.Fatalf("retry waits = %v, want six waits", waits)
			}
			minWait, maxWait := test.min, test.max
			if date, err := http.ParseTime(test.header); err == nil && date.After(started) {
				minWait = time.Until(date) + time.Second
				maxWait = date.Sub(started) + 2*time.Second
			}
			for _, d := range waits {
				if d < minWait || d >= maxWait {
					t.Errorf("wait = %s, want [%s, %s)", d, minWait, maxWait)
				}
			}
			if slices.Min(waits) == slices.Max(waits) {
				t.Errorf("retry waits have no jitter: %v", waits)
			}
		})
	}
}

func TestRegisterDeadline(t *testing.T) {
	for _, parentTimeout := range []time.Duration{time.Hour, 20 * time.Millisecond} {
		t.Run(parentTimeout.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), parentTimeout)
			defer cancel()
			attempts := 0
			c := core.Client{
				Logger: logger.Discard,
				APIClient: api.NewClient(logger.Discard, api.Config{
					Endpoint: "https://agent.example/",
					HTTPClient: &http.Client{Transport: registrationTransport(func(req *http.Request) (*http.Response, error) {
						attempts++
						deadline, ok := req.Context().Deadline()
						want := min(parentTimeout, 5*time.Minute)
						if left := time.Until(deadline); !ok || left > want || left < want-time.Second {
							t.Errorf("request deadline remaining = %s (set: %t), want approximately %s", left, ok, want)
						}
						if parentTimeout == time.Hour {
							cancel()
						}
						<-req.Context().Done()
						return nil, req.Context().Err()
					})},
				}),
			}
			_, err := c.Register(ctx, api.AgentRegisterRequest{})
			if !errors.Is(err, ctx.Err()) || attempts != 1 {
				t.Errorf("Register error = %v, attempts = %d; want %v after one attempt", err, attempts, ctx.Err())
			}
		})
	}
}

func TestRegisterRetryWaitDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	attempts := 0
	c := core.Client{
		Logger: logger.Discard,
		APIClient: api.NewClient(logger.Discard, api.Config{
			Endpoint: "https://agent.example/",
			HTTPClient: &http.Client{Transport: registrationTransport(func(req *http.Request) (*http.Response, error) {
				attempts++
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Retry-After": {"600"}},
					Body:       io.NopCloser(strings.NewReader(`{"message":"rate limited"}`)),
					Request:    req,
				}, nil
			})},
		}),
	}
	_, err := c.Register(ctx, api.AgentRegisterRequest{})
	if !errors.Is(err, context.DeadlineExceeded) || attempts != 1 {
		t.Errorf("Register error = %v, attempts = %d; want deadline exceeded after one attempt", err, attempts)
	}
}
