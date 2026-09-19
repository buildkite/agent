package job

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/shell"
)

func TestCheckoutLFSMirror(t *testing.T) {
	if err := exec.Command("git", "lfs", "version").Run(); err != nil {
		t.Skip("git-lfs is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")
	runGitForMirrorTest(t, "", "lfs", "install", "--skip-repo")

	for _, test := range []struct {
		name          string
		mode          string
		clean         bool
		noMirror      bool
		skipUpdate    bool
		missing       bool
		customStorage bool
		mirrorStorage bool
		lfsConfig     bool
		sparseMode    SparseCheckoutMode
		paths         []string
	}{
		{name: "reference", mode: "reference"},
		{name: "dissociate", mode: "dissociate"},
		{name: "snapshot", mode: "reference", clean: true},
		{name: "no mirror", noMirror: true},
		{name: "skip update", mode: "reference", skipUpdate: true},
		{name: "missing mirror", mode: "reference", skipUpdate: true, missing: true},
		{name: "checkout LFS config", mode: "reference", lfsConfig: true},
		{name: "custom checkout storage", mode: "reference", customStorage: true},
		{name: "custom mirror storage", mode: "reference", mirrorStorage: true},
		{name: "cone", mode: "reference", sparseMode: SparseCheckoutModeCone, paths: []string{"included"}},
		{name: "cone dissociate", mode: "dissociate", sparseMode: SparseCheckoutModeCone, paths: []string{"included"}},
		{name: "non-cone", mode: "reference", sparseMode: SparseCheckoutModeNoCone, paths: []string{"/included/", "!/included/excluded.bin"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			canonical := filepath.Join(root, "canonical.git")
			source := filepath.Join(root, "source")
			runGitForMirrorTest(t, "", "init", "--bare", canonical)
			runGitForMirrorTest(t, "", "init", "--initial-branch=main", source)
			runGitForMirrorTest(t, source, "lfs", "install", "--local")
			runGitForMirrorTest(t, source, "lfs", "track", "*.bin")
			write := func(name string, data []byte) {
				t.Helper()
				path := filepath.Join(source, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			write("included/asset.bin", []byte("old main-branch payload"))
			runGitForMirrorTest(t, source, "add", ".")
			runGitForMirrorTest(t, source, "commit", "-m", "Initial fixture\n\nCo-authored-by: Codex <noreply@openai.com>")
			runGitForMirrorTest(t, source, "remote", "add", "origin", canonical)
			runGitForMirrorTest(t, source, "push", "origin", "main")
			runGitForMirrorTest(t, canonical, "symbolic-ref", "HEAD", "refs/heads/main")
			runGitForMirrorTest(t, source, "checkout", "-b", "feature")
			asset := []byte("LFS payload from the selected job commit")
			excluded := []byte("excluded LFS payload")
			outside := []byte("outside the sparse directory")
			write("included/asset.bin", asset)
			write("included/excluded.bin", excluded)
			write("outside/asset.bin", outside)
			if test.lfsConfig {
				// This config exists only on the job's branch, not mirror HEAD.
				write(".lfsconfig", []byte("[lfs]\n\tfetchinclude = included/asset.bin\n"))
			}
			runGitForMirrorTest(t, source, "add", ".")
			runGitForMirrorTest(t, source, "commit", "-m", "Job fixture\n\nCo-authored-by: Codex <noreply@openai.com>")
			runGitForMirrorTest(t, source, "push", "origin", "feature")
			commit := gitOutputForRemoteCheckoutTest(t, source, "rev-parse", "HEAD")
			e := newOnHostMirrorExecutor(t, canonical, commit)
			e.Branch = "feature"
			e.BuildPath = filepath.Join(root, "build")
			e.GitLFSEnabled = true
			e.GitMirrorCheckoutMode = test.mode
			e.GitCleanFlags = "-ffxdq"
			if test.customStorage {
				e.GitCloneFlags = "--config lfs.storage=custom-lfs"
			}
			e.CleanCheckout = test.clean
			e.GitMirrorsSkipUpdate = test.skipUpdate
			e.GitSparseCheckoutMode = test.sparseMode.String()
			e.GitSparseCheckoutPaths = test.paths
			e.shell.Env.Set("GIT_LFS_SKIP_SMUDGE", "1")
			mirror := expectedOnHostMirrorDir(e)
			if test.noMirror {
				e.GitMirrorsPath = ""
			} else if (test.skipUpdate && !test.missing) || test.mirrorStorage {
				runGitForMirrorTest(t, "", "clone", "--mirror", canonical, mirror)
			}
			if test.mirrorStorage {
				runGitForMirrorTest(t, mirror, "config", "lfs.storage", "custom-lfs")
			}
			shared := !test.noMirror && !test.skipUpdate && !test.customStorage && !test.mirrorStorage
			objectPath := func(store string, data []byte) string {
				oid := fmt.Sprintf("%x", sha256.Sum256(data))
				return filepath.Join(store, "lfs", "objects", oid[:2], oid[2:4], oid)
			}
			for attempt := range 2 {
				if attempt == 1 {
					if !shared {
						break
					}
					// A fresh checkout must succeed using only cached LFS objects.
					if err := os.RemoveAll(filepath.Join(canonical, "lfs")); err != nil {
						t.Fatal(err)
					}
				}
				checkout := filepath.Join(root, fmt.Sprintf("checkout-%d", attempt))
				e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkout)
				if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
					t.Fatalf("checkout %d: %v", attempt, err)
				}
				got, err := os.ReadFile(filepath.Join(checkout, "included", "asset.bin"))
				if err != nil || !bytes.Equal(got, asset) {
					t.Fatalf("materialized asset = %q, %v; want %q", got, err, asset)
				}
				if shared {
					if got, err := os.ReadFile(objectPath(mirror, asset)); err != nil || !bytes.Equal(got, asset) {
						t.Fatalf("mirror asset = %q, %v; want %q", got, err, asset)
					}
				} else if _, err := os.Stat(filepath.Join(mirror, "lfs")); !os.IsNotExist(err) {
					t.Fatalf("fallback modified mirror LFS storage: %v", err)
				}
				if test.sparseMode == SparseCheckoutModeCone || test.lfsConfig {
					if _, err := os.Stat(objectPath(mirror, outside)); !os.IsNotExist(err) {
						t.Fatalf("fetch cached an out-of-scope object: %v", err)
					}
				}
				if len(test.paths) > 0 {
					if _, err := os.Stat(filepath.Join(checkout, "outside", "asset.bin")); !os.IsNotExist(err) {
						t.Fatalf("checkout materialized a sparse-excluded path: %v", err)
					}
				}
				if test.sparseMode == SparseCheckoutModeNoCone {
					if _, err := os.Stat(filepath.Join(checkout, "included", "excluded.bin")); !os.IsNotExist(err) {
						t.Fatalf("checkout materialized a negated sparse path: %v", err)
					}
				}
				lfsEnv := gitOutputForRemoteCheckoutTest(t, checkout, "lfs", "env")
				storage := "lfs"
				if test.customStorage {
					storage = "custom-lfs"
				}
				if !strings.Contains(lfsEnv, "LocalMediaDir="+filepath.Join(checkout, ".git", storage, "objects")) {
					t.Fatalf("checkout no longer owns its LFS storage:\n%s", lfsEnv)
				}
			}
		})
	}
}

func TestCheckoutLFSMirrorLockCancellation(t *testing.T) {
	e := newOnHostMirrorExecutor(t, "https://example.com/repo.git", "HEAD")
	mirror := expectedOnHostMirrorDir(e)
	lock, err := e.shell.LockFile(t.Context(), mirror+".updatelock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Unlock() })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var commands [][]string
	args := gitLFSFetchCheckoutArgs{Shell: shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&commands))}
	if err := e.fetchAndCheckoutLFS(ctx, mirror, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchAndCheckoutLFS() = %v, want cancellation", err)
	}
	for _, command := range commands {
		for _, arg := range command {
			if arg == "lfs" {
				t.Fatalf("LFS command ran without the lock: %v", command)
			}
		}
	}
}
