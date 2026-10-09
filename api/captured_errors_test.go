package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
)

func TestCaptureJobError(t *testing.T) {
	for _, status := range []int{201, 400, 401, 404, 409, 410, 413, 422, 429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/v3/jobs/job-id/errors" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Token job-token" {
					t.Errorf("Authorization = %q", got)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				want := map[string]any{
					"code": "image_pull_failed", "message": "Failed to pull image",
					"timestamp": "2026-09-09T10:30:00.123456Z", "idempotency_key": "attempt-1",
					"context": map[string]any{"exit_status": float64(17)},
				}
				if !reflect.DeepEqual(body, want) {
					t.Errorf("body = %#v, want %#v", body, want)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"uuid":"occurrence-id","message":"result"}`))
			}))
			defer server.Close()
			client := api.NewClient(logger.Discard, api.Config{Endpoint: server.URL + "/v3", Token: "job-token"})
			resp, err := client.CaptureJobError(t.Context(), "job-id", &api.JobCapturedError{
				Code: "image_pull_failed", Message: "Failed to pull image",
				Timestamp:      time.Date(2026, 9, 9, 10, 30, 0, 123456000, time.UTC),
				IdempotencyKey: "attempt-1", Context: map[string]any{"exit_status": 17},
			})
			if (err != nil) != (status != 201) {
				t.Errorf("error = %v for status %d", err, status)
			}
			if resp == nil || resp.StatusCode != status || calls != 1 {
				t.Errorf("response = %v, calls = %d", resp, calls)
			}
		})
	}
}
