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
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/reuse"
	"github.com/buildkite/agent/v4/logger"
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

	// A repair run replaces a corrupt stored archive, which other runs keep.
	if err := runReuseCommand(t, server.URL, "restore", "--job", "restore", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse restore error = %v", err)
	}
	distArchive := filepath.Join(storeDir, filepath.FromSlash(reuseStoreKey(wantDist)))
	if err := os.WriteFile(distArchive, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	repair := "repair"
	fake.plans["repair"] = api.ReusePlan{StepKey: "build", Save: true, Mode: &repair, Outputs: api.ReuseOutputs{Paths: []string{"dist/"}}}
	for _, job := range []string{"save", "repair"} {
		if err := runReuseCommand(t, server.URL, "save", "--job", job, "--exit-status", "0", "--cache-store-url", storeURL); err != nil {
			t.Fatalf("reuse save (%s) error = %v", job, err)
		}
		got, err := os.ReadFile(distArchive)
		if err != nil {
			t.Fatal(err)
		}
		if corrupt := string(got) == "corrupt"; corrupt != (job == "save") {
			t.Errorf("stored archive corrupt after %s = %t, want %t", job, corrupt, job == "save")
		}
	}
	if err := os.RemoveAll("dist"); err != nil {
		t.Fatal(err)
	}
	fake.plans["restore-repaired"] = api.ReusePlan{StepKey: "test", Restore: []api.ReuseRestore{{StepKey: "build", Paths: fake.results[len(fake.results)-1].Paths}}}
	if err := runReuseCommand(t, server.URL, "restore", "--job", "restore-repaired", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse restore (repaired) error = %v", err)
	}
	if got, _, err := reuse.TreeDigest("dist"); err != nil || got != wantDist {
		t.Errorf("restored repaired dist digest = %q, %v, want %q", got, err, wantDist)
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

	zero, two := 0, 2
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
		{name: "signed by the source job", entry: api.ReuseRestore{StepKey: "build", JobID: "reusing-job", SourceJobID: "save", Paths: result.Paths, Signature: result.Signature}, verify: true},
		{name: "signed for another job", entry: api.ReuseRestore{StepKey: "build", JobID: "build-job", SourceJobID: "build-job", Paths: result.Paths, Signature: result.Signature}, verify: true, wantReason: "result signature invalid: signed for job save"},
		{name: "signed for another step", entry: api.ReuseRestore{StepKey: "test", Paths: result.Paths, Signature: result.Signature}, verify: true, wantReason: `result signature invalid: signed for step "build"`},
		{name: "path digest not covered", entry: api.ReuseRestore{StepKey: "build", Paths: tamperedPaths, Signature: result.Signature}, verify: true, wantReason: "result signature invalid: doesn't cover VERSION"},
		{name: "signed values match", entry: api.ReuseRestore{StepKey: "build", Paths: result.Paths, Values: map[string]string{"version": "1.2.3"}, ExitStatus: &zero, Signature: result.Signature}, verify: true},
		{name: "stored value changed", entry: api.ReuseRestore{StepKey: "build", Paths: result.Paths, Values: map[string]string{"version": "6.6.6"}, Signature: result.Signature}, verify: true, wantReason: "result signature invalid: stored output values don't match"},
		{name: "stored exit status changed", entry: api.ReuseRestore{StepKey: "build", Paths: result.Paths, ExitStatus: &two, Values: map[string]string{"version": "1.2.3"}, Signature: result.Signature}, verify: true, wantReason: "result signature invalid: signed exit status 0, stored 2"},
		{name: "values-only result with a missing signature", entry: api.ReuseRestore{StepKey: "build", Values: map[string]string{"version": "1.2.3"}}, verify: true, wantReason: "result signature missing"},
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

func TestReuseSaveOnFailure(t *testing.T) {
	clearReuseSigningEnv(t)
	t.Setenv("TMPDIR", t.TempDir())
	t.Chdir(t.TempDir())
	storeURL := "file://" + t.TempDir()
	if err := os.WriteFile("report.xml", []byte("<failures/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want, _, err := reuse.TreeDigest("report.xml")
	if err != nil {
		t.Fatal(err)
	}
	outputs := api.ReuseOutputs{Paths: []string{"report.xml"}}
	fake := &fakeReuseAPI{plans: map[string]api.ReusePlan{
		"soft":     {StepKey: "test", Save: true, SaveOnFailure: true, Outputs: outputs},
		"hard":     {StepKey: "test", Save: true, Outputs: outputs},
		"nosave":   {StepKey: "test", SaveOnFailure: true, Outputs: outputs},
		"discard":  {StepKey: "test", Save: true, SaveOnFailure: true, Outputs: api.ReuseOutputs{Paths: []string{"missing/"}}},
		"no-paths": {StepKey: "test", Save: true, SaveOnFailure: true},
	}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	for _, tt := range []struct {
		job         string
		wantPaths   int
		wantDiscard bool
	}{
		{job: "soft", wantPaths: 1},
		{job: "hard"},
		{job: "nosave", wantPaths: 1},
		{job: "discard", wantDiscard: true},
		{job: "no-paths"},
	} {
		t.Run(tt.job, func(t *testing.T) {
			fake.results = nil
			if err := runReuseCommand(t, server.URL, "save", "--job", tt.job, "--exit-status", "2", "--cache-store-url", storeURL); err != nil {
				t.Fatalf("reuse save error = %v", err)
			}
			if len(fake.results) != 1 {
				t.Fatalf("results = %+v, want one", fake.results)
			}
			got := fake.results[0]
			if got.ExitStatus != 2 || len(got.Paths) != tt.wantPaths || (got.DiscardReason != nil) != tt.wantDiscard {
				t.Fatalf("result = %+v, want exit status 2, %d paths, discarded %t", got, tt.wantPaths, tt.wantDiscard)
			}
			if tt.wantPaths > 0 && got.Paths[0].Digest != want {
				t.Errorf("path digest = %s, want %s", got.Paths[0].Digest, want)
			}
		})
	}
}

func TestLogReuseMode(t *testing.T) {
	ptr := func(s string) *string { return &s }
	for _, tt := range []struct {
		mode, reason *string
		want         []string
	}{
		{want: []string{}},
		{mode: ptr("reuse"), want: []string{}},
		{mode: ptr("shadow"), want: []string{"[info] Step reuse: shadow mode, the step runs and its result is compared with the stored result"}},
		{mode: ptr("run_anyway"), reason: ptr("reuse: false in the build"), want: []string{"[info] Step reuse: run anyway mode, the step runs even though a stored result could be reused (reuse: false in the build)"}},
		{mode: ptr("future"), want: []string{"[info] Step reuse: future mode"}},
	} {
		l := logger.NewBuffer()
		logReuseMode(l, &api.ReusePlan{Mode: tt.mode, ModeReason: tt.reason})
		if diff := cmp.Diff(l.Messages, tt.want); diff != "" {
			t.Errorf("logReuseMode(%v) messages (-got +want):\n%s", tt.mode, diff)
		}
	}
}

// fakeNSC is an `nsc` CLI that keeps artifacts in a directory, and logs each
// invocation's arguments.
const fakeNSC = `#!/bin/sh
echo "$*" >> "$FAKE_NSC_DIR/calls.log"
[ "$1" = artifact ] || exit 1
cmd=$2
shift 2
case "$cmd" in
upload)
  mkdir -p "$(dirname "$FAKE_NSC_DIR/store/$2")" && cp "$1" "$FAKE_NSC_DIR/store/$2" ;;
download)
  if [ ! -f "$FAKE_NSC_DIR/store/$1" ]; then echo "artifact $1 not found" >&2; exit 1; fi
  cp "$FAKE_NSC_DIR/store/$1" "$2" ;;
describe)
  if [ ! -f "$FAKE_NSC_DIR/store/$1" ]; then echo "$1 doesn't exist"; exit 2; fi
  printf '{"size":%d,"status":"LIVE"}\n' "$(wc -c < "$FAKE_NSC_DIR/store/$1")" ;;
extend)
  if [ "$1" = --help ]; then echo "  --ensure_minimum duration"; exit 0; fi
  [ -f "$FAKE_NSC_DIR/store/$1" ] || exit 1 ;;
*)
  exit 1 ;;
esac
`

func TestReuseSaveAndRestoreNSC(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake nsc CLI is a shell script")
	}
	clearReuseSigningEnv(t)
	t.Setenv("TMPDIR", t.TempDir())
	nscDir := t.TempDir()
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "nsc"), []byte(fakeNSC), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_NSC_DIR", nscDir)
	const storeURL = "nsc://buildkite-cache-my-cluster"

	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("dist", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("dist", "bin", "app"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wantDist, _, err := reuse.TreeDigest("dist")
	if err != nil {
		t.Fatal(err)
	}

	fake := &fakeReuseAPI{plans: map[string]api.ReusePlan{
		"save": {StepKey: "build", Save: true, Outputs: api.ReuseOutputs{Paths: []string{"dist/"}}},
	}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	if err := runReuseCommand(t, server.URL, "save", "--job", "save", "--exit-status", "0", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse save error = %v", err)
	}
	result := fake.results[0]
	if result.DiscardReason != nil || len(result.Paths) != 1 || result.Paths[0].Digest != wantDist || result.Paths[0].Size == 0 {
		t.Fatalf("result = %+v, want dist/ stored with digest %s", result, wantDist)
	}
	key := result.Paths[0].Key

	// Saving the same output again only extends its expiry.
	if err := runReuseCommand(t, server.URL, "save", "--job", "save", "--exit-status", "0", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse save (again) error = %v", err)
	}

	if err := os.RemoveAll("dist"); err != nil {
		t.Fatal(err)
	}
	fake.plans["restore"] = api.ReusePlan{StepKey: "test", Restore: []api.ReuseRestore{{StepKey: "build", Paths: result.Paths}}}
	if err := runReuseCommand(t, server.URL, "restore", "--job", "restore", "--cache-store-url", storeURL); err != nil {
		t.Fatalf("reuse restore error = %v", err)
	}
	if got, _, err := reuse.TreeDigest("dist"); err != nil || got != wantDist {
		t.Errorf("restored dist digest = %q, %v, want %q", got, err, wantDist)
	}

	// A missing artifact is unavailable.
	missing := result.Paths[0]
	missing.Key = "step-reuse/missing.tar.gz"
	fake.plans["missing"] = api.ReusePlan{StepKey: "test", Restore: []api.ReuseRestore{{StepKey: "build", Paths: []api.ReusePath{missing}}}}
	err = runReuseCommand(t, server.URL, "restore", "--job", "missing", "--cache-store-url", storeURL)
	if serr := new(SilentExitError); !errors.As(err, &serr) || serr.Code() != reuse.ExitCodeInputUnavailable {
		t.Fatalf("reuse restore (missing) error = %v, want exit %d", err, reuse.ExitCodeInputUnavailable)
	}
	if len(fake.unavailable) != 1 || fake.unavailable[0].Reason != "blob not found" {
		t.Errorf("input_unavailable reports = %+v, want one for a missing blob", fake.unavailable)
	}

	calls, err := os.ReadFile(filepath.Join(nscDir, "calls.log"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for line := range strings.Lines(string(calls)) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "artifact extend --help") {
			continue
		}
		// Drop local temporary file paths.
		fields := strings.Fields(line)
		switch fields[1] {
		case "upload":
			fields = slices.Delete(fields, 2, 3)
		case "download":
			fields = slices.Delete(fields, 3, 4)
		}
		got = append(got, strings.Join(fields, " "))
	}
	ns := "--namespace buildkite-cache-my-cluster"
	want := []string{
		"artifact describe " + key + " -o json " + ns,
		"artifact upload " + key + " --expires_in 720h " + ns,
		"artifact describe " + key + " -o json " + ns,
		"artifact extend " + key + " --ensure_minimum 720h " + ns,
		"artifact download " + key + " " + ns,
		"artifact extend " + key + " --ensure_minimum 720h " + ns,
		"artifact download step-reuse/missing.tar.gz " + ns,
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("nsc calls (-got +want):\n%s", diff)
	}
}
