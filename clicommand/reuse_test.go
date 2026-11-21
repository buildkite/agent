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
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/reuse"
	"github.com/buildkite/go-pipeline/jwkutil"
	"github.com/google/go-cmp/cmp"
	"github.com/lestrrat-go/jwx/v3/jwa"
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
	clearReuseSigningEnv(t)
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
		"publish": {
			StepKey: "build",
			Save:    false,
			Outputs: api.ReuseOutputs{Paths: []string{"VERSION"}},
		},
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

	// With save: false, output paths are still stored for later steps in the build.
	if err := runReuseCommand(t, server.URL, "save", "--job", "publish", "--exit-status", "1", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse save (publish, failed) error = %v", err)
	}
	if len(fake.results) != 0 {
		t.Fatalf("results after a failed save: false job = %+v, want none", fake.results)
	}
	if err := runReuseCommand(t, server.URL, "save", "--job", "publish", "--exit-status", "0", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse save (publish) error = %v", err)
	}
	if got := fake.results; len(got) != 1 || len(got[0].Paths) != 1 || got[0].Paths[0].Path != "VERSION" {
		t.Fatalf("results after save: false with outputs.paths = %+v, want VERSION stored", got)
	}
	fake.results = nil

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

// clearReuseSigningEnv stops the environment from configuring signing keys.
func clearReuseSigningEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"BUILDKITE_AGENT_JWKS_FILE", "BUILDKITE_AGENT_JWKS_KEY_ID",
		"BUILDKITE_AGENT_AWS_KMS_KEY", "BUILDKITE_AGENT_GCP_KMS_KEY",
		"BUILDKITE_AGENT_VERIFICATION_JWKS_FILE",
	} {
		t.Setenv(name, "")
	}
}

func writeJWKS(t *testing.T, path string, set any) {
	t.Helper()
	b, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReuseResultSignatures(t *testing.T) {
	clearReuseSigningEnv(t)
	t.Setenv("TMPDIR", t.TempDir())
	keysDir := t.TempDir()
	priv, pub, err := jwkutil.NewKeyPair("test-key", jwa.EdDSA())
	if err != nil {
		t.Fatal(err)
	}
	otherPriv, _, err := jwkutil.NewKeyPair("test-key", jwa.EdDSA())
	if err != nil {
		t.Fatal(err)
	}
	signingFile := filepath.Join(keysDir, "signing.json")
	otherFile := filepath.Join(keysDir, "other.json")
	verifyFile := filepath.Join(keysDir, "verify.json")
	writeJWKS(t, signingFile, priv)
	writeJWKS(t, otherFile, otherPriv)
	writeJWKS(t, verifyFile, pub)

	t.Chdir(t.TempDir())
	storeURL := "file://" + t.TempDir()
	if err := os.WriteFile("VERSION", []byte("1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("NOTES", []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeReuseAPI{plans: map[string]api.ReusePlan{
		"save": {
			StepKey: "build",
			Save:    true,
			Outputs: api.ReuseOutputs{Paths: []string{"VERSION", "NOTES"}, Values: []string{"version"}},
			Values:  map[string]string{"version": "1.2.3"},
		},
	}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	saveWith := func(keyFile string) api.ReuseResult {
		t.Helper()
		fake.results = nil
		if err := runReuseCommand(t, server.URL, "save", "--job", "save", "--exit-status", "0", "--cache-store-url", storeURL, "--jwks-file", keyFile, "--jwks-key-id", "test-key"); err != nil {
			t.Fatalf("reuse save error = %v", err)
		}
		return fake.results[0]
	}

	// The saved result is signed with the job's signing key.
	result := saveWith(signingFile)
	if result.Signature == nil {
		t.Fatal("signature = nil, want a signed manifest")
	}
	m, err := reuse.VerifyManifest(pub, *result.Signature)
	if err != nil {
		t.Fatalf("VerifyManifest() error = %v", err)
	}
	want := &reuse.Manifest{
		Version:    1,
		JobID:      "save",
		StepKey:    "build",
		ExitStatus: 0,
		Paths: []reuse.ManifestPath{
			{Path: "NOTES", Digest: result.Paths[1].Digest},
			{Path: "VERSION", Digest: result.Paths[0].Digest},
		},
		Values: map[string]string{"version": "1.2.3"},
	}
	if diff := cmp.Diff(m, want); diff != "" {
		t.Errorf("manifest (-got +want):\n%s", diff)
	}
	otherSig := saveWith(otherFile).Signature

	// Without a signing key, the result is unsigned.
	fake.results = nil
	if err := runReuseCommand(t, server.URL, "save", "--job", "save", "--exit-status", "0", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse save error = %v", err)
	}
	if got := fake.results[0].Signature; got != nil {
		t.Errorf("unsigned signature = %q, want nil", *got)
	}

	tamperedPaths := slices.Clone(result.Paths)
	tamperedPaths[0].Digest = result.Paths[1].Digest
	tamperedPaths[0].Key = result.Paths[1].Key
	for _, tt := range []struct {
		name       string
		entry      api.ReuseRestore
		verify     bool
		wantReason string
	}{
		{name: "verified", entry: api.ReuseRestore{StepKey: "build", Paths: result.Paths, Signature: result.Signature}, verify: true},
		{name: "unverified ignores a missing signature", entry: api.ReuseRestore{StepKey: "build", Paths: result.Paths}},
		{name: "missing signature", entry: api.ReuseRestore{StepKey: "build", Paths: result.Paths}, verify: true, wantReason: "result signature missing"},
		{name: "signed with an untrusted key", entry: api.ReuseRestore{StepKey: "build", Paths: result.Paths, Signature: otherSig}, verify: true, wantReason: "result signature invalid: "},
		{name: "signed for another step", entry: api.ReuseRestore{StepKey: "test", Paths: result.Paths, Signature: result.Signature}, verify: true, wantReason: `result signature invalid: signed for step "build"`},
		{name: "path digest not covered", entry: api.ReuseRestore{StepKey: "build", Paths: tamperedPaths, Signature: result.Signature}, verify: true, wantReason: "result signature invalid: doesn't cover VERSION"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake.unavailable = nil
			if err := os.WriteFile("VERSION", []byte("stale\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			job := "restore-" + strings.ReplaceAll(tt.name, " ", "-")
			fake.plans[job] = api.ReusePlan{StepKey: "test", Restore: []api.ReuseRestore{tt.entry}}
			args := []string{"restore", "--job", job, "--cache-store-url", storeURL}
			if tt.verify {
				args = append(args, "--verification-jwks-file", verifyFile)
			}
			err := runReuseCommand(t, server.URL, args...)

			if tt.wantReason == "" {
				if err != nil {
					t.Fatalf("reuse restore error = %v", err)
				}
				if got, err := os.ReadFile("VERSION"); err != nil || string(got) != "1.2.3\n" {
					t.Errorf("restored VERSION = %q, %v, want 1.2.3", got, err)
				}
				return
			}
			if serr := new(SilentExitError); !errors.As(err, &serr) || serr.Code() != reuse.ExitCodeInputUnavailable {
				t.Fatalf("reuse restore error = %v, want exit %d", err, reuse.ExitCodeInputUnavailable)
			}
			if len(fake.unavailable) != 1 || fake.unavailable[0].StepKey != tt.entry.StepKey || !strings.HasPrefix(fake.unavailable[0].Reason, tt.wantReason) {
				t.Errorf("input_unavailable reports = %+v, want one for step %s with reason %q", fake.unavailable, tt.entry.StepKey, tt.wantReason)
			}
			if got, _ := os.ReadFile("VERSION"); string(got) != "stale\n" {
				t.Errorf("VERSION = %q after a rejected restore, want it untouched", got)
			}
		})
	}
}
