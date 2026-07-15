package core

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
)

type finishJobAPIClient struct {
	APIClient // panics if unexpected methods are called

	calls    atomic.Int32
	failOnce error
	failWith *api.Response
}

func (f *finishJobAPIClient) FinishJob(context.Context, *api.Job, *bool) (*api.Response, error) {
	n := f.calls.Add(1)
	if n == 1 && f.failOnce != nil {
		return f.failWith, f.failOnce
	}
	return &api.Response{Response: &http.Response{StatusCode: http.StatusOK}}, nil
}

func TestFinishJobRetriesHTTP2ClientConnectionLost(t *testing.T) {
	t.Parallel()

	fake := &finishJobAPIClient{
		failOnce: errors.New("http2: client connection lost"),
	}
	sleeps := 0
	client := &Client{
		APIClient: fake,
		Logger:    logger.Discard,
		RetrySleepFunc: func(time.Duration) {
			sleeps++
		},
	}

	err := client.FinishJob(t.Context(), &api.Job{ID: "job-1"}, time.Now(), ProcessExit{Status: 0}, 0, nil)
	if err != nil {
		t.Fatalf("FinishJob() error = %v, want nil", err)
	}
	if got := fake.calls.Load(); got != 2 {
		t.Fatalf("FinishJob API calls = %d, want 2 (retry after transport error)", got)
	}
	if sleeps != 1 {
		t.Fatalf("retry sleeps = %d, want 1", sleeps)
	}
}

func TestFinishJobDoesNotRetryOn401(t *testing.T) {
	t.Parallel()

	fake := &finishJobAPIClient{
		failOnce: errors.New("unauthorized"),
		failWith: &api.Response{Response: &http.Response{StatusCode: http.StatusUnauthorized}},
	}
	client := &Client{
		APIClient:      fake,
		Logger:         logger.Discard,
		RetrySleepFunc: func(time.Duration) {},
	}

	err := client.FinishJob(t.Context(), &api.Job{ID: "job-1"}, time.Now(), ProcessExit{Status: 0}, 0, nil)
	if err == nil {
		t.Fatal("FinishJob() error = nil, want unauthorized")
	}
	if got := fake.calls.Load(); got != 1 {
		t.Fatalf("FinishJob API calls = %d, want 1 (no retry on 401)", got)
	}
}
