package jobapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/jobapi"
)

func TestSetWorkdir(t *testing.T) {
	t.Parallel()

	// An absolute path that exists on the running OS. The endpoint doesn't stat
	// the filesystem, but using a real absolute path keeps the test portable
	// across platforms (e.g. C:\... on Windows).
	absDir := t.TempDir()

	cases := []apiTestCase[jobapi.WorkdirSetRequest, jobapi.WorkdirSetResponse]{
		{
			name:                 "happy case",
			requestBody:          &jobapi.WorkdirSetRequest{Workdir: absDir},
			expectedStatus:       http.StatusOK,
			expectedResponseBody: &jobapi.WorkdirSetResponse{Workdir: absDir},
		},
		{
			name:           "relative path returns a 422",
			requestBody:    &jobapi.WorkdirSetRequest{Workdir: "relative/path"},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError: &jobapi.ErrorResponse{
				Error: `workdir must be an absolute path, got "relative/path"`,
			},
		},
		{
			name:           "empty path returns a 422",
			requestBody:    &jobapi.WorkdirSetRequest{Workdir: ""},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError: &jobapi.ErrorResponse{
				Error: "workdir must not be empty",
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			environ := testEnviron()
			srv, token, err := testServer(t, environ, replacer.NewMux())
			if err != nil {
				t.Fatalf("creating server: %v", err)
			}

			if err := srv.Start(); err != nil {
				t.Fatalf("starting server: %v", err)
			}

			client := testSocketClient(srv.SocketPath)

			defer func() {
				if err := srv.Stop(); err != nil {
					t.Fatalf("stopping server: %v", err)
				}
			}()

			buf := bytes.NewBuffer(nil)
			if err := json.NewEncoder(buf).Encode(c.requestBody); err != nil {
				t.Fatalf("JSON-encoding c.requestBody into buf: %v", err)
			}

			req, err := http.NewRequest(http.MethodPut, "http://job/api/current-job/v0/workdir", buf)
			if err != nil {
				t.Fatalf("creating request: %v", err)
			}

			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

			testAPI(t, environ, req, client, c)

			// TakePendingWorkdir should return the directory exactly once on
			// success, then report no pending workdir on the next call.
			gotWd, ok := srv.TakePendingWorkdir()
			if c.expectedStatus == http.StatusOK {
				if !ok || gotWd != absDir {
					t.Fatalf("srv.TakePendingWorkdir() = (%q, %t), want (%q, true)", gotWd, ok, absDir)
				}
				if gotWd, ok := srv.TakePendingWorkdir(); ok {
					t.Fatalf("second srv.TakePendingWorkdir() = (%q, %t), want (\"\", false)", gotWd, ok)
				}
			} else if ok {
				t.Fatalf("srv.TakePendingWorkdir() = (%q, true) after a rejected request, want (\"\", false)", gotWd)
			}
		})
	}
}
