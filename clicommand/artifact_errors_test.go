package clicommand

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestArtifactFailuresAreCaptured(t *testing.T) {
	agentAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/builds/build-id/artifacts/search":
			_, _ = w.Write([]byte(`[]`))
		default:
			// Signed URLs in storage errors must never reach the report.
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"denied https://bucket.example/a?X-Amz-Signature=signed"}`))
		}
	}))
	t.Cleanup(agentAPI.Close)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	for _, test := range []struct {
		name    string
		command *cli.Command
		args    []string
		code    string
		message string
	}{
		{
			name:    "upload",
			command: ArtifactUploadCommand,
			args:    []string{"upload", "--job", "job-id", "report.txt"},
			code:    "artifact_upload_failed",
			message: `Failed to upload artifacts matching "report.txt" to Buildkite: POST ` + agentAPI.URL + `/jobs/job-id/artifacts: 403 Forbidden: denied https://bucket.example/a?[REDACTED]`,
		},
		{
			name:    "download with no matches",
			command: ArtifactDownloadCommand,
			args:    []string{"download", "--build", "build-id", "missing/*.txt", dir},
			code:    "artifact_download_failed",
			message: `No artifacts uploaded to build build-id matched "missing/*.txt". Check that the pattern matches the uploaded paths, and that the step uploading them finishes before this one starts, for example by adding depends_on.`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			reports := startAgentErrorTestServer(t)
			cmd := *test.command
			app := &cli.Command{Name: "buildkite-agent", Commands: []*cli.Command{&cmd}, Writer: io.Discard, ErrWriter: io.Discard}
			args := []string{"buildkite-agent", test.args[0], "--endpoint", agentAPI.URL, "--agent-access-token", "job-token"}
			if err := app.Run(t.Context(), append(args, test.args[1:]...)); err == nil {
				t.Fatal("command succeeded, want failure")
			}

			if len(*reports) != 1 {
				t.Fatalf("reports = %+v, want one", *reports)
			}
			report := (*reports)[0]
			if report.Code != test.code || report.Message != test.message {
				t.Errorf("report = %q %q, want %q %q", report.Code, report.Message, test.code, test.message)
			}
		})
	}
}
