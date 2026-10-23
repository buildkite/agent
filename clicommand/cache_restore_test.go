package clicommand

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/urfave/cli/v3"
)

func TestCacheRestoreFailureIsCaptured(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("cache.yml", []byte("caches:\n  - name: rubocop\n    cache_key: [rubocop-v1]\n    target_paths: [rubocop-cache]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"Cache restore is not permitted"}`)
	}))
	t.Cleanup(server.Close)
	reports := startAgentErrorTestServer(t)

	cmd := *CacheRestoreCommand
	app := &cli.Command{Commands: []*cli.Command{&cmd}}
	args := []string{"buildkite-agent", "restore", "--endpoint", server.URL, "--agent-access-token", "test-token", "--registry", "test", "--cache-config-file", "cache.yml", "--name", "rubocop", "--cache-fail-on-error"}
	if err := app.Run(t.Context(), args); err == nil {
		t.Fatal("cache restore succeeded, want failure")
	}

	if len(*reports) != 1 {
		t.Fatalf("reports = %+v, want one", *reports)
	}
	report := (*reports)[0]
	if report.Code != "buildkite.cache_restore_failed" || report.Message != `Failed to restore cache "rubocop" from registry "test": failed to restore cache "rubocop": failed to retrieve cache: request failed with status: 403 Forbidden` {
		t.Errorf("report = %q %q", report.Code, report.Message)
	}
}

func TestCacheRestoreDownloadTimeoutFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("cache.yml", []byte("caches:\n  - {name: one, cache_key: [one], target_paths: [out]}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/retrieve") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(api.CacheEntryRetrieveResp{
				Store: "agent_managed", Fallback: true,
				Blobs: []api.CacheBlob{{Digest: api.CacheDigest{Algorithm: "future", Value: "object"}}},
			})
			return
		}
		if r.Method == http.MethodGet {
			calls.Add(1)
			w.Header().Set("Content-Length", "1")
			_, _ = io.WriteString(w, "x")
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("BUILDKITE_CACHE_RESTORE_DOWNLOAD_TIMEOUT", "1ns")
	for _, tc := range []struct {
		name                    string
		flags                   []string
		wantError, wantDownload bool
	}{
		{name: "environment", wantError: true},
		{name: "flag overrides environment", flags: []string{"--download-timeout", "0"}, wantDownload: true},
		{name: "negative", flags: []string{"--download-timeout", "-1s"}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls.Store(0)
			cmd := *CacheRestoreCommand
			app := &cli.Command{Commands: []*cli.Command{&cmd}}
			args := []string{"buildkite-agent", "restore", "--endpoint", server.URL, "--agent-access-token", "test", "--cache-config-file", "cache.yml", "--cache-fail-on-error", "--cache-store-url", "s3://bucket?use_path_style=true&endpoint=" + server.URL}
			err := app.Run(t.Context(), append(args, tc.flags...))
			if (err != nil) != tc.wantError || (calls.Load() > 0) != tc.wantDownload {
				t.Fatalf("error=%v downloads=%d, want error=%v download=%v", err, calls.Load(), tc.wantError, tc.wantDownload)
			}
		})
	}
}
