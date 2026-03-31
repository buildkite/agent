package api_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/logger"
)

func TestJobURL(t *testing.T) {
	const buildURL = "https://buildkite.com/my-org/my-pipeline/builds/42"
	const jobID = "b078e2d2-86e9-4c12-bf3b-612a8058d0a4"
	const want = buildURL + "#" + jobID

	if got := api.JobURL(buildURL, jobID); got != want {
		t.Errorf("JobURL(%q, %q) = %q, want %q", buildURL, jobID, got, want)
	}

	job := &api.Job{
		ID:  jobID,
		Env: map[string]string{"BUILDKITE_BUILD_URL": buildURL},
	}
	if got := job.URL(); got != want {
		t.Errorf("job.URL() = %q, want %q", got, want)
	}
}

func TestPromiseFailure(t *testing.T) {
	const jobID = "b078e2d2-86e9-4c12-bf3b-612a8058d0a4"
	const accessToken = "llamas"

	ctx := t.Context()

	tests := []struct {
		name           string
		request        *api.JobPromiseFailureRequest
		expectedBody   []byte
		responseStatus int
		wantErr        bool
	}{
		{
			name:         "success",
			request:      &api.JobPromiseFailureRequest{ExitStatus: 1},
			expectedBody: []byte(`{"exit_status":1}` + "\n"),
		},
		{
			name:         "with reason",
			request:      &api.JobPromiseFailureRequest{ExitStatus: 42, Reason: "detected failing tests"},
			expectedBody: []byte(`{"exit_status":42,"reason":"detected failing tests"}` + "\n"),
		},
		{
			name:           "feature disabled",
			request:        &api.JobPromiseFailureRequest{ExitStatus: 1},
			expectedBody:   []byte(`{"exit_status":1}` + "\n"),
			responseStatus: http.StatusNotFound,
			wantErr:        true,
		},
		{
			name:           "conflict",
			request:        &api.JobPromiseFailureRequest{ExitStatus: 1},
			expectedBody:   []byte(`{"exit_status":1}` + "\n"),
			responseStatus: http.StatusConflict,
			wantErr:        true,
		},
		{
			name:           "job not running",
			request:        &api.JobPromiseFailureRequest{ExitStatus: 1},
			expectedBody:   []byte(`{"exit_status":1}` + "\n"),
			responseStatus: http.StatusUnprocessableEntity,
			wantErr:        true,
		},
	}

	path := fmt.Sprintf("/jobs/%s/promise_failure", url.PathEscape(jobID))

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Success cases respond with 204 No Content.
			responseStatus := test.responseStatus
			if responseStatus == 0 {
				responseStatus = http.StatusNoContent
			}

			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if got, want := authToken(req), accessToken; got != want {
					http.Error(rw, fmt.Sprintf("authToken(req) = %q, want %q", got, want), http.StatusUnauthorized)
					return
				}
				if got, want := req.Method, http.MethodPut; got != want {
					t.Errorf("req.Method = %q, want %q", got, want)
				}
				if got, want := req.URL.Path, path; got != want {
					t.Errorf("req.URL.Path = %q, want %q", got, want)
				}

				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Errorf("reading request body: %v", err)
				}
				if !bytes.Equal(body, test.expectedBody) {
					t.Errorf("request body = %q, want %q", body, test.expectedBody)
				}

				rw.WriteHeader(responseStatus)
			}))
			defer server.Close()

			client := api.NewClient(logger.Discard, api.Config{
				UserAgent: "Test",
				Endpoint:  server.URL,
				Token:     accessToken,
				DebugHTTP: true,
			})

			resp, err := client.PromiseFailure(ctx, jobID, test.request)

			if test.wantErr {
				if err == nil {
					t.Fatalf("PromiseFailure(%q) error = nil, want error", jobID)
				}
				if !api.IsErrHavingStatus(err, responseStatus) {
					t.Errorf("IsErrHavingStatus(err, %d) = false, want true (err = %v)", responseStatus, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("PromiseFailure(%q) error = %v, want nil", jobID, err)
			}
			if resp.StatusCode != http.StatusNoContent {
				t.Errorf("resp.StatusCode = %d, want %d", resp.StatusCode, http.StatusNoContent)
			}
		})
	}
}
