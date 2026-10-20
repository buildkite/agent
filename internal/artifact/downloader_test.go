package artifact

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/internal/experiments"
	"github.com/buildkite/agent/v3/logger"
)

func TestArtifactDownloaderConnectsToEndpoint(t *testing.T) {
	t.Cleanup(func() {
		os.Remove("llamas.txt") //nolint:errcheck // Best-effort cleanup.
	})

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.RequestURI() {
		case "/builds/my-build/artifacts/search?state=finished":
			//nolint:errcheck // Test should fail with incomplete response.
			fmt.Fprintf(rw, `[{
				"id": "4600ac5c-5a13-4e92-bb83-f86f218f7b32",
				"file_size": 3,
				"absolute_path": "llamas.txt",
				"path": "llamas.txt",
				"url": "http://%s/download"
			}]`, req.Host)
		case "/download":
			fmt.Fprintln(rw, "OK") //nolint:errcheck // YOLO?
		default:
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := t.Context()

	ac := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    "llamasforever",
	})

	d := NewDownloader(logger.Discard, ac, DownloaderConfig{
		BuildID: "my-build",
	})

	if err := d.Download(ctx); err != nil {
		t.Errorf("d.Download() = %v", err)
	}
}

func TestArtifactDownloadDestinationConfinement(t *testing.T) {
	const content = "downloaded artifact\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"artifact"`)
		http.ServeContent(w, r, "artifact", time.Time{}, strings.NewReader(content))
	}))
	defer server.Close()

	// Exercise the Azure SDK against the local server without real credentials.
	t.Setenv("BUILDKITE_AZURE_BLOB_CONNECTION_STRING", "DefaultEndpointsProtocol=http;AccountName=test;AccountKey=dGVzdA==;BlobEndpoint="+server.URL+";")
	s3Client := newTestS3Client(t, server)

	for _, backend := range []string{"http", "s3-single", "s3-multipart", "azure"} {
		t.Run(backend, func(t *testing.T) {
			for _, scenario := range []struct {
				name           string
				wantErr        bool
				allowTraversal bool
			}{
				{name: "ordinary"},
				{name: "merged-prefix"},
				{name: "lexical-traversal"},
				{name: "relative-parent-escape", wantErr: true},
				{name: "absolute-parent-escape", wantErr: true},
				{name: "nested-directory-escape", wantErr: true},
				{name: "merged-prefix-escape", wantErr: true},
				{name: "contained-relative-link"},
				{name: "contained-absolute-link", wantErr: true},
				{name: "final-file-link"},
				{name: "destination-link"},
				{name: "experiment-lexical-traversal", allowTraversal: true},
				{name: "experiment-relative-parent-escape", allowTraversal: true},
				{name: "experiment-absolute-parent-escape", allowTraversal: true},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					base := t.TempDir()
					destination := filepath.Join(base, "checkout")
					outside := filepath.Join(base, "outside")
					for _, dir := range []string{destination, outside, filepath.Join(destination, "real")} {
						if err := os.MkdirAll(dir, 0o755); err != nil {
							t.Fatal(err)
						}
					}
					victim := filepath.Join(outside, "victim")
					if err := os.WriteFile(victim, []byte("unchanged"), 0o644); err != nil {
						t.Fatal(err)
					}

					path := "dir/artifact"
					wantPath := filepath.Join(destination, path)
					link, linkTarget := filepath.Join(destination, "link"), ""
					switch scenario.name {
					case "merged-prefix":
						path = "checkout/dir/artifact"
					case "lexical-traversal":
						path = "../../dir/artifact"
					case "experiment-lexical-traversal":
						path = "../outside/victim"
					case "relative-parent-escape", "merged-prefix-escape", "experiment-relative-parent-escape":
						linkTarget = "../outside"
						path = "link/victim"
						if scenario.name == "merged-prefix-escape" {
							path = "checkout/" + path
						}
					case "absolute-parent-escape", "experiment-absolute-parent-escape":
						linkTarget, path = outside, "link/victim"
					case "nested-directory-escape":
						linkTarget, path = "../outside", "link/new/subdir/artifact"
					case "contained-relative-link", "contained-absolute-link":
						linkTarget, path = "real", "link/artifact"
						wantPath = filepath.Join(destination, "real/artifact")
						if scenario.name == "contained-absolute-link" {
							linkTarget = filepath.Join(destination, "real")
						}
					case "final-file-link":
						path = "artifact"
						wantPath = filepath.Join(destination, path)
						link, linkTarget = wantPath, victim
					case "destination-link":
						link, linkTarget = filepath.Join(base, "alias"), destination
						destination = link
					}
					if linkTarget != "" {
						if err := os.Symlink(linkTarget, link); err != nil {
							if runtime.GOOS == "windows" {
								t.Skipf("cannot create symlink: %v", err)
							}
							t.Fatal(err)
						}
					}

					var d downloader
					switch backend {
					case "http":
						d = NewDownload(logger.Discard, server.Client(), DownloadConfig{
							URL: server.URL, Destination: destination, Path: path, Retries: 1,
						})
					case "s3-single", "s3-multipart":
						d = NewS3Downloader(logger.Discard, S3DownloaderConfig{
							S3Client: s3Client, S3Path: "s3://test-bucket", Destination: destination,
							Path: path, Retries: 1, AllowS3Multipart: backend == "s3-multipart",
						})
					case "azure":
						d = NewAzureBlobDownloader(logger.Discard, AzureBlobDownloaderConfig{
							Repository:  "https://test.blob.core.windows.net/container/prefix",
							Destination: destination, Path: path, Retries: 1,
						})
					}
					ctx := t.Context()
					wantVictim := "unchanged"
					if scenario.allowTraversal {
						ctx, _ = experiments.Enable(ctx, experiments.AllowArtifactPathTraversal)
						wantPath, wantVictim = victim, content
					}
					err := d.Start(ctx)
					if (err != nil) != scenario.wantErr {
						t.Errorf("Start() = %v, want error: %v", err, scenario.wantErr)
					}
					if !scenario.wantErr {
						if got, err := os.ReadFile(wantPath); err != nil || string(got) != content {
							t.Errorf("downloaded contents = %q, %v; want %q", got, err, content)
						}
					}
					if got, err := os.ReadFile(victim); err != nil || string(got) != wantVictim {
						t.Errorf("outside file = %q, %v; want %q", got, err, wantVictim)
					}
					if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
						t.Errorf("outside directory contains %d entries, %v; want only victim", len(entries), err)
					}
					if scenario.wantErr {
						if entries, err := os.ReadDir(filepath.Join(destination, "real")); err != nil || len(entries) != 0 {
							t.Errorf("contained directory contains %d entries after rejection, %v; want empty", len(entries), err)
						}
					}
				})
			}
		})
	}
}

func TestDownloadChecksumMismatchPreservesTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "new contents") //nolint:errcheck // Read failure fails the test.
	}))
	defer server.Close()

	destination := t.TempDir()
	target := filepath.Join(destination, "artifact")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := NewDownload(logger.Discard, server.Client(), DownloadConfig{
		URL: server.URL, Destination: destination, Path: "artifact", Retries: 1,
		WantSHA256: strings.Repeat("0", 64),
	})
	if err := d.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Start() = %v, want checksum error", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "original" {
		t.Errorf("target = %q, %v; want original", got, err)
	}
	if entries, err := os.ReadDir(destination); err != nil || len(entries) != 1 {
		t.Errorf("destination contains %d entries, %v; want only original artifact", len(entries), err)
	}
}
