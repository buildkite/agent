package clicommand

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/archive"
	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
)

func TestCacheSaveForce(t *testing.T) {
	for _, test := range []struct {
		name     string
		args     []string
		exists   bool
		denied   bool
		wantSave bool
		wantPeek bool
	}{
		{name: "existing entry is skipped", exists: true, wantPeek: true},
		{name: "force replaces existing entry", args: []string{"--force"}, exists: true, wantSave: true},
		{name: "force creates missing entry", args: []string{"--force"}, wantSave: true},
		{name: "explicit false skips existing entry", args: []string{"--force=false"}, exists: true, wantPeek: true},
		{name: "force respects policy denial", args: []string{"--force", "--cache-fail-on-error"}, exists: true, denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.WriteFile("rubocop-cache", []byte("updated cache contents"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile("cache.yml", []byte("caches:\n  - name: rubocop\n    cache_key: [rubocop-v1]\n    target_paths: [rubocop-cache]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			storageDir := t.TempDir()
			storagePath := filepath.ToSlash(storageDir)
			if !strings.HasPrefix(storagePath, "/") {
				storagePath = "/" + storagePath
			}
			storageURL := (&url.URL{Scheme: "file", Path: storagePath}).String()
			requests := make(chan string, 20)
			var entry api.CacheEntryCreateReq
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Method + " " + r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				switch r.Method + " " + r.URL.Path {
				case "POST /cache_registries/test/peek":
					if !test.exists {
						w.WriteHeader(http.StatusNotFound)
						_, _ = io.WriteString(w, `{"message":"Cache entry not found"}`)
						return
					}
					_, _ = io.WriteString(w, `{}`)
				case "GET /cache_registries/test":
					_, _ = io.WriteString(w, `{"store":"local_file"}`)
				case "PUT /cache_registries/test/store":
					if test.denied {
						w.WriteHeader(http.StatusForbidden)
						_, _ = io.WriteString(w, `{"message":"Cache save is not permitted by the registry policy"}`)
						return
					}
					if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					_, _ = io.WriteString(w, `{"upload_id":"upload-1"}`)
				case "PUT /cache_registries/test/commit":
					var commit api.CacheEntryCommitReq
					if err := json.NewDecoder(r.Body).Decode(&commit); err != nil {
						t.Error(err)
					}
					if commit.UploadID != "upload-1" {
						t.Errorf("UploadID = %q, want upload-1", commit.UploadID)
					}
					_, _ = io.WriteString(w, `{}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			t.Cleanup(server.Close)
			cmd := *CacheSaveCommand
			app := &cli.Command{Commands: []*cli.Command{&cmd}}
			args := []string{"buildkite-agent", "save", "--endpoint", server.URL, "--agent-access-token", "test-token", "--registry", "test", "--cache-config-file", "cache.yml", "--cache-store-url", storageURL, "--name", "rubocop"}
			err := app.Run(t.Context(), append(args, test.args...))
			if (err != nil) != test.denied {
				t.Fatalf("cache save error = %v, want error: %t", err, test.denied)
			}
			server.Close()
			close(requests)
			var gotRequests []string
			for request := range requests {
				gotRequests = append(gotRequests, request)
			}
			var wantRequests []string
			if test.wantPeek {
				wantRequests = append(wantRequests, "POST /cache_registries/test/peek")
			}
			if test.wantSave || test.denied {
				wantRequests = append(wantRequests, "GET /cache_registries/test", "PUT /cache_registries/test/store")
			}
			if test.wantSave {
				wantRequests = append(wantRequests, "PUT /cache_registries/test/commit")
			}
			if diff := cmp.Diff(wantRequests, gotRequests); diff != "" {
				t.Errorf("requests mismatch (-want +got):\n%s", diff)
			}
			if !test.wantSave {
				return
			}
			if diff := cmp.Diff([]string{"rubocop-cache"}, entry.TargetPaths); diff != "" {
				t.Errorf("target paths mismatch (-want +got):\n%s", diff)
			}
			if len(entry.CacheKey) != 1 || entry.CacheKey[0].Value != "rubocop-v1" {
				t.Errorf("cache key changed: %v", entry.CacheKey)
			}
			if len(entry.Blobs) != 1 {
				t.Fatalf("blobs = %v, want one archive", entry.Blobs)
			}
			blob, err := os.Open(filepath.Join(storageDir, entry.Blobs[0].Digest.Value))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = blob.Close() }()
			if err := os.Remove("rubocop-cache"); err != nil {
				t.Fatal(err)
			}
			if _, err := archive.ExtractFiles(t.Context(), blob, entry.Blobs[0].FileSize, entry.TargetPaths); err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile("rubocop-cache")
			if err != nil {
				t.Fatal(err)
			}
			if string(contents) != "updated cache contents" {
				t.Errorf("archive contents = %q, want updated cache contents", contents)
			}
		})
	}
}

// TestCacheSaveExperimentalArchiveMethod covers the A-1952 benchmark
// selector through the CLI: the archive uses the selected method, and every
// cache API request, including commit, identifies the method and labels in
// its User-Agent.
func TestCacheSaveExperimentalArchiveMethod(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("bench-data", []byte(strings.Repeat("cache contents ", 1_000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("cache.yml", []byte("caches:\n  - name: bench\n    cache_key: [bench-v1]\n    target_paths: [bench-data]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	storageDir := t.TempDir()
	storagePath := filepath.ToSlash(storageDir)
	if !strings.HasPrefix(storagePath, "/") {
		storagePath = "/" + storagePath
	}
	storageURL := (&url.URL{Scheme: "file", Path: storagePath}).String()

	var entry api.CacheEntryCreateReq
	var commitUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /cache_registries/test/peek":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Cache entry not found"}`)
		case "GET /cache_registries/test":
			_, _ = io.WriteString(w, `{"store":"local_file"}`)
		case "PUT /cache_registries/test/store":
			if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
				t.Error(err)
			}
			_, _ = io.WriteString(w, `{"upload_id":"upload-1"}`)
		case "PUT /cache_registries/test/commit":
			commitUserAgent = r.Header.Get("User-Agent")
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BUILDKITE_CACHE_EXPERIMENTAL_ARCHIVE_METHOD", "store")
	t.Setenv("BUILDKITE_CACHE_EXPERIMENTAL_TELEMETRY_LABELS", "workload_small_typical,trial_2")
	cmd := *CacheSaveCommand
	app := &cli.Command{Commands: []*cli.Command{&cmd}}
	args := []string{"buildkite-agent", "save", "--endpoint", server.URL, "--agent-access-token", "test-token", "--registry", "test", "--cache-config-file", "cache.yml", "--cache-store-url", storageURL, "--name", "bench", "--cache-fail-on-error"}
	if err := app.Run(t.Context(), args); err != nil {
		t.Fatalf("cache save: %v", err)
	}

	if want := " a1952_method_store a1952_workload_small_typical a1952_trial_2"; !strings.HasSuffix(commitUserAgent, want) {
		t.Errorf("commit User-Agent = %q, want suffix %q", commitUserAgent, want)
	}
	if len(entry.Blobs) != 1 {
		t.Fatalf("blobs = %v, want one archive", entry.Blobs)
	}
	if _, err := archive.CheckEntryMethod(filepath.Join(storageDir, entry.Blobs[0].Digest.Value), entry.Blobs[0].FileSize, "store"); err != nil {
		t.Errorf("stored archive: %v", err)
	}
}

func TestExperimentalCacheUserAgent(t *testing.T) {
	for _, test := range []struct {
		name    string
		cfg     CacheConfig
		want    string
		wantErr bool
	}{
		{name: "unset leaves the User-Agent alone", want: "ua"},
		{name: "method only", cfg: CacheConfig{ExperimentalArchiveMethod: "zstd"}, want: "ua a1952_method_zstd"},
		{name: "labels only", cfg: CacheConfig{ExperimentalTelemetryLabels: []string{"trial_1"}}, want: "ua a1952_trial_1"},
		{name: "unknown method", cfg: CacheConfig{ExperimentalArchiveMethod: "deflate"}, wantErr: true},
		{name: "label with a space", cfg: CacheConfig{ExperimentalTelemetryLabels: []string{"a b"}}, wantErr: true},
		{name: "label with upper case", cfg: CacheConfig{ExperimentalTelemetryLabels: []string{"Small"}}, wantErr: true},
		{name: "empty label", cfg: CacheConfig{ExperimentalTelemetryLabels: []string{""}}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := experimentalCacheUserAgent("ua", test.cfg)
			if (err != nil) != test.wantErr {
				t.Fatalf("err = %v, want error: %t", err, test.wantErr)
			}
			if got != test.want {
				t.Errorf("User-Agent = %q, want %q", got, test.want)
			}
		})
	}
}
