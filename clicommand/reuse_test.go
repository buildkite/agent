package clicommand

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/reuse"
	"github.com/urfave/cli/v3"
)

// fakeReuseAPI serves reuse plans per job and records reports.
type fakeReuseAPI struct {
	mu          sync.Mutex
	plans       map[string]api.ReusePlan
	results     []api.ReuseResult
	unavailable []api.ReuseInputUnavailable
}

func (f *fakeReuseAPI) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "jobs" || parts[2] != "reuse" {
		http.Error(rw, `{"message":"not found"}`, http.StatusNotFound)
		return
	}
	job, rest := parts[1], strings.Join(parts[3:], "/")
	switch req.Method + " " + rest {
	case "GET ":
		_ = json.NewEncoder(rw).Encode(f.plans[job])
	case "POST result":
		var r api.ReuseResult
		_ = json.NewDecoder(req.Body).Decode(&r)
		f.results = append(f.results, r)
		_, _ = io.WriteString(rw, `{"saved":true,"reason":"","action_key":"hs256:test"}`)
	case "POST input_unavailable":
		var r api.ReuseInputUnavailable
		_ = json.NewDecoder(req.Body).Decode(&r)
		f.unavailable = append(f.unavailable, r)
		_, _ = io.WriteString(rw, `{"repair":true,"message":"repair scheduled"}`)
	default:
		http.Error(rw, `{"message":"not found"}`, http.StatusNotFound)
	}
}

func runReuseCommand(t *testing.T, endpoint string, args ...string) error {
	t.Helper()
	restore, save := *ReuseRestoreCommand, *ReuseSaveCommand
	group := &cli.Command{Name: "reuse", Commands: []*cli.Command{&restore, &save}}
	app := &cli.Command{Name: "buildkite-agent", Commands: []*cli.Command{group}, Writer: io.Discard, ErrWriter: io.Discard}
	full := append([]string{"buildkite-agent", "reuse"}, args[0], "--endpoint", endpoint, "--agent-access-token", "token")
	return app.Run(t.Context(), append(full, args[1:]...))
}

func TestReuseSaveAndRestore(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	repo := t.TempDir()
	storeDir := t.TempDir()
	storeURL := "file://" + storeDir
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "-A")
	runGit("commit", "-q", "-m", "init")
	t.Chdir(repo)

	// The command's outputs.
	if err := os.MkdirAll(filepath.Join("dist", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("dist", "bin", "app"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("bin/app", filepath.Join("dist", "app")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("VERSION", []byte("1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantDist, _, err := reuse.TreeDigest("dist")
	if err != nil {
		t.Fatal(err)
	}
	wantInputs, err := reuse.HashFiles(t.Context(), ".", []string{"src/**"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}

	fake := &fakeReuseAPI{plans: map[string]api.ReusePlan{
		"save": {
			StepKey: "build",
			Save:    true,
			Files:   &api.ReuseFiles{Include: []string{"src/**"}},
			Outputs: api.ReuseOutputs{Paths: []string{"dist/", "VERSION"}},
		},
		"nosave": {StepKey: "build", Save: false},
	}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	// Saving with save: false reports nothing.
	if err := runReuseCommand(t, server.URL, "save", "--job", "nosave", "--exit-status", "0", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse save (nosave) error = %v", err)
	}
	if len(fake.results) != 0 {
		t.Fatalf("results after save: false = %+v, want none", fake.results)
	}

	// A failed command reports only its exit status.
	if err := runReuseCommand(t, server.URL, "save", "--job", "save", "--exit-status", "3", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse save (failed command) error = %v", err)
	}
	if got := fake.results[0]; got.ExitStatus != 3 || len(got.Paths) != 0 || got.VerifiedFilesDigest != nil {
		t.Errorf("result for failed command = %+v, want only exit status 3", got)
	}

	// A successful command stores its outputs.
	if err := runReuseCommand(t, server.URL, "save", "--job", "save", "--exit-status", "0", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse save error = %v", err)
	}
	result := fake.results[1]
	if result.DiscardReason != nil {
		t.Fatalf("discard_reason = %q, want none", *result.DiscardReason)
	}
	if result.VerifiedFilesDigest == nil || *result.VerifiedFilesDigest != wantInputs.Digest {
		t.Errorf("verified_files_digest = %v, want %q", result.VerifiedFilesDigest, wantInputs.Digest)
	}
	if len(result.Paths) != 2 {
		t.Fatalf("paths = %+v, want 2", result.Paths)
	}
	dist := result.Paths[0]
	if dist.Path != "dist/" || dist.Digest != wantDist || dist.Files != 2 || dist.Key != reuseStoreKey(wantDist) || dist.Size == 0 {
		t.Errorf("dist path = %+v, want digest %s with 2 files", dist, wantDist)
	}
	if _, err := os.Stat(filepath.Join(storeDir, filepath.FromSlash(dist.Key))); err != nil {
		t.Errorf("stored archive: %v", err)
	}

	// Restoring replaces the outputs in the checkout.
	if err := os.RemoveAll("dist"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("VERSION", []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake.plans["restore"] = api.ReusePlan{StepKey: "test", Restore: []api.ReuseRestore{{StepKey: "build", Paths: result.Paths}}}
	if err := runReuseCommand(t, server.URL, "restore", "--job", "restore", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse restore error = %v", err)
	}
	if got, _, err := reuse.TreeDigest("dist"); err != nil || got != wantDist {
		t.Errorf("restored dist digest = %q, %v, want %q", got, err, wantDist)
	}
	if got, err := os.ReadFile("VERSION"); err != nil || string(got) != "1.2.3\n" {
		t.Errorf("restored VERSION = %q, %v, want 1.2.3", got, err)
	}
	if got := readReuseRestoreElapsed("restore"); got < 0 {
		t.Errorf("restore elapsed = %v", got)
	}
	if _, err := os.Stat(reuseStatePath("restore")); !os.IsNotExist(err) {
		t.Errorf("restore state file still exists after reading: %v", err)
	}

	// A missing archive is reported, and the command exits 94.
	missing := result.Paths[0]
	missing.Key = "step-reuse/missing.tar.gz"
	fake.plans["missing"] = api.ReusePlan{StepKey: "test", Restore: []api.ReuseRestore{{StepKey: "build", Paths: []api.ReusePath{missing}}}}
	err = runReuseCommand(t, server.URL, "restore", "--job", "missing", "--cache-store-url", storeURL)
	if serr := new(SilentExitError); !errors.As(err, &serr) || serr.Code() != reuse.ExitCodeInputUnavailable {
		t.Fatalf("reuse restore (missing) error = %v, want exit %d", err, reuse.ExitCodeInputUnavailable)
	}
	want := api.ReuseInputUnavailable{StepKey: "build", Path: "dist/", Reason: "blob not found"}
	if len(fake.unavailable) != 1 || fake.unavailable[0] != want {
		t.Errorf("input_unavailable reports = %+v, want [%+v]", fake.unavailable, want)
	}

	// Without a store, the result is discarded.
	if err := runReuseCommand(t, server.URL, "save", "--job", "save", "--exit-status", "0", "--cache-store-url", ""); err != nil {
		t.Fatalf("reuse save (no store) error = %v", err)
	}
	if got := fake.results[2].DiscardReason; got == nil || *got != reuseNoStoreReason {
		t.Errorf("discard_reason without a store = %v, want %q", got, reuseNoStoreReason)
	}
}
