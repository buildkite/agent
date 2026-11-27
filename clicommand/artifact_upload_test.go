package clicommand

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/urfave/cli/v3"
)

func TestArtifactUploadObjectAttributes(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, name := range []string{"first.txt", "second.txt"} {
		if err := os.WriteFile(name, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := filepath.Join(t.TempDir(), "agent.cfg")
	if err := os.WriteFile(config, []byte("object-attributes=true\ns3-object-tags='{\"team\":\"agent\",\"scope\":\"infra\"}'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, env string
		args      []string
		want      string
		wantError bool
		noMatches bool
	}{
		{name: "agent defaults", want: "scope=infra&team=agent"},
		{name: "step replaces map", env: `{"team":"step"}`, want: "team=step"},
		{name: "CLI replaces step", env: `{"team":"step"}`, args: []string{`--s3-object-tags={"owner":"cli"}`}, want: "owner=cli"},
		{name: "clear inherited map", args: []string{"--s3-object-tags={}"}},
		{name: "disable malformed defaults", env: "not-json", args: []string{"--object-attributes=false", "--gcs-object-contexts=also-not-json"}},
		{name: "empty is invalid", args: []string{"--s3-object-tags="}, wantError: true},
		{name: "wrong-provider CLI", args: []string{"--gcs-object-contexts={}"}, wantError: true},
		{name: "no matches bypass validation", env: "not-json", args: []string{"--gcs-object-contexts={}"}, noMatches: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Inactive environment defaults are allowed, unlike explicit CLI flags.
			t.Setenv("BUILDKITE_GCS_OBJECT_CONTEXTS", "not-json")
			t.Setenv("BUILDKITE_AZURE_BLOB_TAGS", "not-json")
			t.Setenv("BUILDKITE_ARTIFACT_UPLOAD_OBJECT_ATTRIBUTES", "true")
			t.Setenv("BUILDKITE_S3_OBJECT_TAGS", test.env)
			if test.env == "" {
				if err := os.Unsetenv("BUILDKITE_S3_OBJECT_TAGS"); err != nil {
					t.Fatal(err)
				}
			}
			var mu sync.Mutex
			var uploaded []string
			var registrations, updates int
			storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					_, _ = io.WriteString(w, `<ListBucketResult/>`)
					return
				}
				if r.Method != "PUT" {
					t.Errorf("unexpected storage method %s", r.Method)
					w.WriteHeader(400)
					return
				}
				got, err := url.ParseQuery(r.Header.Get("X-Amz-Tagging"))
				if err != nil {
					t.Error(err)
				}
				if got.Encode() != test.want {
					t.Errorf("tags=%q, want %q", got.Encode(), test.want)
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if !strings.HasSuffix(r.URL.Path, "/"+string(data)) {
					t.Errorf("object %q contains %q", r.URL.Path, data)
				}
				mu.Lock()
				uploaded = append(uploaded, string(data))
				mu.Unlock()
				w.Header().Set("ETag", `"created"`)
			}))
			defer storage.Close()
			agentAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				mu.Lock()
				defer mu.Unlock()
				switch r.Method {
				case "POST":
					registrations++
					var batch api.ArtifactBatch
					if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
						t.Error(err)
					}
					response := api.ArtifactBatchCreateResponse{}
					for i := range batch.Artifacts {
						response.ArtifactIDs = append(response.ArtifactIDs, fmt.Sprintf("artifact-%d", i))
					}
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				case "PUT":
					var request api.ArtifactBatchUpdateRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					for _, artifact := range request.Artifacts {
						if artifact.State != "finished" {
							t.Errorf("artifact state=%q", artifact.State)
						}
						updates++
					}
					_, _ = io.WriteString(w, `{}`)
				default:
					t.Errorf("unexpected API method %s", r.Method)
					w.WriteHeader(400)
				}
			}))
			defer agentAPI.Close()
			t.Setenv("BUILDKITE_S3_DEFAULT_REGION", "us-east-1")
			t.Setenv("BUILDKITE_S3_ACCESS_KEY_ID", "test-key")
			t.Setenv("BUILDKITE_S3_SECRET_ACCESS_KEY", "test-secret")
			t.Setenv("BUILDKITE_S3_ENDPOINT", storage.URL)
			t.Setenv("BUILDKITE_S3_PROFILE", "")
			t.Setenv("AWS_PROFILE", "")
			// Fresh flags avoid cli's retained parse state while running the real action.
			cmd := &cli.Command{
				Name: "upload", Action: ArtifactUploadCommand.Action, Writer: io.Discard, ErrWriter: io.Discard,
				Flags: append(artifactObjectAttributeFlags(), &cli.StringFlag{Name: "config"}, &cli.StringFlag{Name: "job", Value: "job-id"}, &cli.StringFlag{Name: "endpoint", Value: agentAPI.URL}, &cli.StringFlag{Name: "agent-access-token", Value: "test-token"}, &cli.StringFlag{Name: "delimiter", Value: ";"}),
			}
			args := append([]string{"upload", "--config", config}, test.args...)
			pattern := "*.txt"
			if test.noMatches {
				pattern = "missing-*.log"
			}
			err := cmd.Run(t.Context(), append(args, pattern, "s3://bucket/prefix"))
			if (err != nil) != test.wantError {
				t.Fatalf("upload error=%v, want error %t", err, test.wantError)
			}
			mu.Lock()
			defer mu.Unlock()
			if test.wantError || test.noMatches {
				if registrations != 0 || len(uploaded) != 0 {
					t.Errorf("rejected/no-match upload made requests")
				}
				return
			}
			if registrations != 1 || updates != 2 || len(uploaded) != 2 {
				t.Errorf("registrations=%d updates=%d uploaded=%v", registrations, updates, uploaded)
			}
		})
	}
}
