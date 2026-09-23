package job

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
		name              string
		mode              string
		clean             bool
		noMirror          bool
		skipUpdate        bool
		missing           bool
		customStorage     bool
		mirrorStorage     bool
		lfsConfig         bool
		conditionalConfig bool
		commitRef         string
		refSpec           string
		prRef             string
		existingMirror    bool
		advanceBranch     bool
		sparseMode        SparseCheckoutMode
		paths             []string
	}{
		{name: "reference", mode: "reference"},
		{name: "dissociate", mode: "dissociate"},
		{name: "snapshot", mode: "reference", clean: true},
		{name: "no mirror", noMirror: true},
		{name: "skip update", mode: "reference", skipUpdate: true},
		{name: "missing mirror", mode: "reference", skipUpdate: true, missing: true},
		{name: "checkout LFS config", mode: "reference", lfsConfig: true},
		{name: "sparse checkout LFS config", mode: "reference", lfsConfig: true, sparseMode: SparseCheckoutModeNoCone, paths: []string{"/included/", "!/included/excluded.bin"}},
		{name: "workspace conditional LFS config", mode: "reference", conditionalConfig: true},
		{name: "HEAD fresh mirror", mode: "reference", commitRef: "HEAD"},
		{name: "HEAD existing mirror", mode: "reference", commitRef: "HEAD", existingMirror: true},
		{name: "HEAD custom refspec fresh mirror", mode: "reference", commitRef: "HEAD", refSpec: "+refs/heads/feature:refs/buildkite/job"},
		{name: "HEAD custom refspec existing mirror", mode: "reference", commitRef: "HEAD", refSpec: "+refs/heads/feature:refs/buildkite/job", existingMirror: true},
		{name: "HEAD PR fresh mirror", mode: "reference", commitRef: "HEAD", prRef: "head"},
		{name: "HEAD PR existing mirror", mode: "reference", commitRef: "HEAD", prRef: "head", existingMirror: true},
		{name: "HEAD PR merge fresh mirror", mode: "reference", commitRef: "HEAD", prRef: "merge"},
		{name: "HEAD PR merge existing mirror", mode: "reference", commitRef: "HEAD", prRef: "merge", existingMirror: true},
		{name: "named tag", mode: "reference", commitRef: "v1"},
		{name: "pinned commit overrides refspec", mode: "reference", refSpec: "+refs/heads/main:refs/buildkite/job", existingMirror: true},
		{name: "branch moves after prefetch", mode: "reference", commitRef: "HEAD", advanceBranch: true},
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
			write(".lfsconfig", []byte("[lfs]\n\tfetchinclude = absent.bin\n"))
			runGitForMirrorTest(t, source, "add", ".")
			runGitForMirrorTest(t, source, "commit", "-m", "Initial fixture\n\nCo-authored-by: Codex <noreply@openai.com>")
			runGitForMirrorTest(t, source, "remote", "add", "origin", canonical)
			runGitForMirrorTest(t, source, "push", "origin", "main")
			runGitForMirrorTest(t, canonical, "symbolic-ref", "HEAD", "refs/heads/main")
			runGitForMirrorTest(t, source, "checkout", "-b", "feature")
			if err := os.Remove(filepath.Join(source, ".lfsconfig")); err != nil {
				t.Fatal(err)
			}
			asset := []byte("LFS payload from the selected job commit")
			excluded := []byte("excluded LFS payload")
			outside := []byte("outside the sparse directory")
			write("included/asset.bin", asset)
			write("included/excluded.bin", excluded)
			write("outside/asset.bin", outside)
			if test.lfsConfig {
				// This config exists only on the job's branch, not mirror HEAD.
				write(".lfsconfig", []byte("[lfs]\n\tfetchinclude = included/asset.bin\n"))
			} else if test.conditionalConfig {
				// Workspace configuration must override this endpoint for the
				// checkout to succeed, rather than merely resolving to origin.
				write(".lfsconfig", []byte("[lfs]\n\turl = file:///unavailable-lfs-repository\n"))
				runGitForMirrorTest(t, source, "config", "lfs.url", canonical)
			}
			runGitForMirrorTest(t, source, "add", ".")
			runGitForMirrorTest(t, source, "commit", "-m", "Job fixture\n\nCo-authored-by: Codex <noreply@openai.com>")
			runGitForMirrorTest(t, source, "push", "origin", "feature")
			commit := gitOutputForRemoteCheckoutTest(t, source, "rev-parse", "HEAD")
			e := newOnHostMirrorExecutor(t, canonical, commit)
			e.Branch = "feature"
			if test.commitRef != "" {
				e.Commit = test.commitRef
			}
			if test.commitRef == "v1" {
				runGitForMirrorTest(t, canonical, "tag", "v1", commit)
			}
			if test.refSpec != "" {
				e.RefSpec = test.refSpec
				e.Branch = "main"
			}
			if test.prRef != "" {
				e.PullRequest = "123"
				e.PipelineProvider = "github"
				e.PullRequestUsingMergeRefspec = test.prRef == "merge"
				e.Branch = "main"
				runGitForMirrorTest(t, canonical, "update-ref", "refs/pull/123/"+test.prRef, commit)
			}
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
			} else if (test.skipUpdate && !test.missing) || test.mirrorStorage || test.conditionalConfig || test.existingMirror {
				runGitForMirrorTest(t, "", "clone", "--mirror", canonical, mirror)
			}
			if test.existingMirror {
				// Leave HEAD, the local branch, and FETCH_HEAD at the wrong
				// commit; the update must use the job ref it actually fetches.
				runGitForMirrorTest(t, mirror, "update-ref", "refs/heads/feature", "refs/heads/main")
				runGitForMirrorTest(t, mirror, "fetch", "origin", "main")
			}
			if test.mirrorStorage {
				runGitForMirrorTest(t, mirror, "config", "lfs.storage", "custom-lfs")
			}
			if test.conditionalConfig {
				// The mirror cannot access LFS; only the future workspace's
				// conditional include supplies the working endpoint.
				runGitForMirrorTest(t, mirror, "config", "lfs.url", filepath.Join(root, "unavailable.git"))
				config := filepath.Join(root, "workspace-lfs.config")
				runGitForMirrorTest(t, "", "config", "--file", config, "lfs.url", canonical)
				runGitForMirrorTest(t, "", "config", "--global", "includeIf.gitdir:"+filepath.ToSlash(root)+"/checkout-*/.git.path", config)
			}
			shared := !test.noMirror && !test.skipUpdate && !test.mirrorStorage && !test.conditionalConfig
			objectPath := func(store string, data []byte) string {
				oid := fmt.Sprintf("%x", sha256.Sum256(data))
				return filepath.Join(store, "lfs", "objects", oid[:2], oid[2:4], oid)
			}
			cachedAsset := asset
			if shared {
				// Populate the cache during the initial mirror update, before
				// any workspace exists. Neither later checkout can use remote LFS.
				if _, err := e.getOrUpdateMirrorDir(t.Context(), canonical, nil); err != nil {
					t.Fatal(err)
				}
				if got, err := os.ReadFile(objectPath(mirror, asset)); err != nil || !bytes.Equal(got, asset) {
					t.Fatalf("initial mirror update asset = %q, %v; want %q", got, err, asset)
				}
				if test.advanceBranch {
					asset = []byte("LFS payload added after mirror prefetch")
					write("included/asset.bin", asset)
					runGitForMirrorTest(t, source, "add", ".")
					runGitForMirrorTest(t, source, "commit", "-m", "Advance fixture\n\nCo-authored-by: Codex <noreply@openai.com>")
					runGitForMirrorTest(t, source, "push", "origin", "feature")
					// Keep the prefetched mirror at the old revision while the
					// workspace resolves the branch again, as if it just moved.
					e.GitMirrorsSkipUpdate = true
				} else if err := os.RemoveAll(filepath.Join(canonical, "lfs")); err != nil {
					t.Fatal(err)
				}
			}
			for attempt := range 2 {
				if attempt == 1 {
					if !shared || test.advanceBranch {
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
					if got, err := os.ReadFile(objectPath(mirror, cachedAsset)); err != nil || !bytes.Equal(got, cachedAsset) {
						t.Fatalf("mirror asset = %q, %v; want %q", got, err, cachedAsset)
					}
					if test.advanceBranch {
						if _, err := os.Stat(objectPath(mirror, asset)); !os.IsNotExist(err) {
							t.Fatalf("new payload unexpectedly present in mirror: %v", err)
						}
					}
				} else if _, err := os.Stat(objectPath(mirror, asset)); !os.IsNotExist(err) {
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
