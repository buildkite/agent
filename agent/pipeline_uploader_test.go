package agent_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/go-pipeline"
	"github.com/stretchr/testify/assert"
)

func TestAsyncPipelineUpload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	l := logger.NewBuffer()

	for _, test := range []struct {
		replace        bool
		state          string
		expectedSleeps []time.Duration
		err            error
	}{
		{
			state:          "applied",
			expectedSleeps: []time.Duration{},
		},
		{
			state:          "rejected",
			expectedSleeps: []time.Duration{},
			err:            errors.New("Failed to upload and process pipeline: Pipeline upload rejected: "),
		},
		{
			state: "pending",
			expectedSleeps: func() []time.Duration {
				sleeps := make([]time.Duration, 0, 59)
				for range 59 {
					sleeps = append(sleeps, 5*time.Second)
				}
				return sleeps
			}(),
			err: errors.New("Failed to upload and process pipeline: Pipeline upload not yet applied: pending"),
		},
	} {
		t.Run(test.state, func(t *testing.T) {
			t.Parallel()

			jobID := api.NewUUID()
			stepUploadUUID := api.NewUUID()
			pipelineStr := `---
steps:
  - name: ":s3: xxx"
    command: "script/buildkite/xxx.sh"
    plugins:
      xxx/aws-assume-role#v0.1.0:
        role: arn:aws:iam::xxx:role/xxx
      ecr#v1.1.4:
        login: true
        account_ids: xxx
        registry_region: us-east-1
      docker-compose#v2.5.1:
        run: xxx
        config: .buildkite/docker/docker-compose.yml
        env:
          - AWS_ACCESS_KEY_ID
          - AWS_SECRET_ACCESS_KEY
          - AWS_SESSION_TOKEN
    agents:
      queue: xxx`

			pipeline, err := pipeline.Parse(strings.NewReader(pipelineStr))
			assert.NoError(t, err)

			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case fmt.Sprintf("/jobs/%s/pipelines", jobID):
					assert.Equal(t, req.URL.Query().Get("async"), "true")
					if req.Method == "POST" {
						rw.Header().Add("Retry-After", "5")
						rw.Header().Add("Location", fmt.Sprintf("/jobs/%s/pipelines/%s", jobID, stepUploadUUID))
						rw.WriteHeader(http.StatusAccepted)
						return
					}
				case fmt.Sprintf("/jobs/%s/pipelines/%s", jobID, stepUploadUUID):
					if req.Method == "GET" {
						_, _ = fmt.Fprintf(rw, `{"state": "%s"}`, test.state)
						return
					}
				}
				t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
				http.Error(rw, "Not found", http.StatusNotFound)
			}))
			defer server.Close()

			retrySleeps := []time.Duration{}
			uploader := &agent.PipelineUploader{
				Client: api.NewClient(logger.Discard, api.Config{
					Endpoint: server.URL,
					Token:    "llamas",
				}),
				JobID: jobID,
				Change: &api.PipelineChange{
					UUID:     stepUploadUUID,
					Pipeline: pipeline,
					Replace:  test.replace,
				},
				RetrySleepFunc: func(d time.Duration) {
					retrySleeps = append(retrySleeps, d)
				},
			}

			err = uploader.Upload(ctx, l)
			if test.err == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, test.err.Error())
			}
			assert.Equal(t, test.expectedSleeps, retrySleeps)
		})
	}
}

func TestFallbackPipelineUpload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	l := logger.NewBuffer()

	genSleeps := func(n int, s time.Duration) []time.Duration {
		sleeps := make([]time.Duration, 0, n)
		for range n {
			sleeps = append(sleeps, s)
		}
		return sleeps
	}

	for _, test := range []struct {
		name            string
		num529s         int
		expectedSleeps  []time.Duration
		expectedUploads int
		errStatus       int // 0 indicates no error should occur
	}{
		{
			name:            "happy",
			num529s:         0,
			expectedSleeps:  []time.Duration{},
			expectedUploads: 1,
		},
		{
			name:            "59_529s",
			num529s:         59,
			expectedSleeps:  genSleeps(58, 5*time.Second),
			expectedUploads: 59,
		},
		{
			name:            "60_529s",
			num529s:         60,
			expectedSleeps:  genSleeps(59, 5*time.Second),
			expectedUploads: 60,
		},
		{
			name:            "61_529s",
			num529s:         61,
			expectedSleeps:  genSleeps(59, 5*time.Second),
			expectedUploads: 60,
			errStatus:       529,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			jobID := api.NewUUID()
			stepUploadUUID := api.NewUUID()
			pipelineStr := `---
steps:
  - name: ":s3: xxx"
    command: "script/buildkite/xxx.sh"
    plugins:
      xxx/aws-assume-role#v0.1.0:
        role: arn:aws:iam::xxx:role/xxx
      ecr#v1.1.4:
        login: true
        account_ids: xxx
        registry_region: us-east-1
      docker-compose#v2.5.1:
        run: xxx
        config: .buildkite/docker/docker-compose.yml
        env:
          - AWS_ACCESS_KEY_ID
          - AWS_SECRET_ACCESS_KEY
          - AWS_SESSION_TOKEN
    agents:
      queue: xxx`

			pipeline, err := pipeline.Parse(strings.NewReader(pipelineStr))
			assert.NoError(t, err)

			countUploadCalls := 0
			server := httptest.NewServer(
				http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
					switch req.URL.Path {
					case fmt.Sprintf("/jobs/%s/pipelines", jobID):
						if req.Method == "POST" {
							countUploadCalls++
							if countUploadCalls < test.num529s {
								http.Error(rw, `{"message":"still waiting"}`, 529)
								return
							}
							if countUploadCalls > test.expectedUploads {
								http.Error(
									rw,
									`{"message":"too many calls to pipeline upload"}`,
									http.StatusBadRequest,
								)
								return
							}
							rw.WriteHeader(http.StatusOK)
							return
						}
					case fmt.Sprintf("/jobs/%s/pipelines/%s", jobID, stepUploadUUID):
						assert.Fail(t, "should not call the status route")
						http.Error(
							rw,
							"This route should not have been called",
							http.StatusServiceUnavailable,
						)
						return
					}
					t.Errorf("Unknown endpoint %s %s", req.Method, req.URL.Path)
					http.Error(rw, "Not found", http.StatusNotFound)
				}),
			)
			defer server.Close()

			retrySleeps := []time.Duration{}
			uploader := &agent.PipelineUploader{
				Client: api.NewClient(logger.Discard, api.Config{
					Endpoint: server.URL,
					Token:    "llamas",
				}),
				JobID: jobID,
				Change: &api.PipelineChange{
					UUID:     stepUploadUUID,
					Pipeline: pipeline,
				},
				RetrySleepFunc: func(d time.Duration) {
					retrySleeps = append(retrySleeps, d)
				},
			}

			err = uploader.Upload(ctx, l)
			if test.errStatus == 0 {
				assert.NoError(t, err)
			} else {
				assert.True(
					t,
					api.IsErrHavingStatus(err, test.errStatus),
					"expected api error with status: %d, received: %v",
					test.errStatus, err,
				)
			}
			assert.Equal(t, test.expectedSleeps, retrySleeps)
		})
	}
}
