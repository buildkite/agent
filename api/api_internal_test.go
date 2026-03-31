package api

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/logger"
)

func TestNewRequestBuildkiteTimeoutMilliseconds(t *testing.T) {
	c := NewClient(logger.NewBuffer(), Config{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	r, err := c.newRequest(ctx, "GET", "/foo", nil)
	if err != nil {
		t.Errorf("c.newRequest(ctx, %q, %q, %v) error = %v, want nil", "GET", "/foo", nil, err)
	}

	value := r.Header.Get("Buildkite-Timeout-Milliseconds")
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		t.Errorf("strconv.ParseInt(%q, %d, %d) error = %v, want nil", value, 10, 64, err)
	}

	// Allow a generous 1000ms between setting the timeout and the header being set.
	if ms <= 9_000 || ms > 10_000 {
		t.Errorf("Expected Buildkite-Timeout-Milliseconds to reflect 10 second timeout, got %q (%d ms)", value, ms)
	}
}

func TestNewRequestWithoutBuildkiteTimeoutMilliseconds(t *testing.T) {
	c := NewClient(logger.NewBuffer(), Config{})

	ctx := context.Background() // no timeout/deadline

	r, err := c.newRequest(ctx, "GET", "/foo", nil)
	if err != nil {
		t.Errorf("c.newRequest(ctx, %q, %q, %v) error = %v, want nil", "GET", "/foo", nil, err)
	}

	if value, ok := r.Header["Buildkite-Timeout-Milliseconds"]; ok {
		t.Errorf("Expected no Buildkite-Timeout-Milliseconds header, got %q", value)
	}
}
