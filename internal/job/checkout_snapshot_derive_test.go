package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/osutil"
	"github.com/buildkite/agent/v4/internal/shell"
)

// newSnapshotDeriveExecutor builds an executor configured the way the
// snapshot-derive path requires: on-host mirrors enabled, clean checkout, and
// a command phase (so updateGitMirror produces a per-job snapshot).
func newSnapshotDeriveExecutor(t *testing.T, repository, commit string) *Executor {
	t.Helper()
	checkout := t.TempDir()
	e := New(ExecutorConfig{
		Repository:            repository,
		Commit:                commit,
		Branch:                "feature-branch",
		PullRequest:           "false",
		GitMirrorsPath:        t.TempDir(),
		GitMirrorsLockTimeout: 30,
		GitCloneMirrorFlags:   "-v",
		CleanCheckout:         true,
		Phases:                []string{"checkout", "command"},
	})
	e.shell = shell.NewTestShell(t, shell.WithSignalGracePeriod(10*time.Millisecond))
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkout)
	if err := e.createCheckoutDir(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
			e.checkoutRoot = nil
		}
	})
	return e
}

// resolvePathForTest canonicalizes a path for comparison: it resolves
// symlinks (e.g. /var vs /private/var on macOS) and, on Windows, 8.3 short
// names (e.g. BUILDK~1) to their long form.
func resolvePathForTest(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("resolving path %q: %v", path, err)
	}
	return resolved
}

// alternatesContainDir reports whether any entry in the alternates file
// content refers to dir, comparing canonicalized paths (case-insensitively,
// for Windows).
func alternatesContainDir(t *testing.T, content, dir string) bool {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		entry := strings.TrimSpace(line)
		if entry == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(filepath.FromSlash(entry))
		if err != nil {
			continue
		}
		if strings.EqualFold(resolved, dir) {
			return true
		}
	}
	return false
}

func TestPrepareCheckoutWorkdirDerivesFromSnapshotWithoutCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e := newSnapshotDeriveExecutor(t, canonical.RepoURL("canonical"), commit)

	mirror, err := e.getOrUpdateMirror(t.Context(), e.Repository, nil)
	if err != nil {
		t.Fatalf("getOrUpdateMirror() error = %v", err)
	}
	if !mirror.isSnapshot {
		t.Fatalf("mirror = %+v, want a per-job snapshot for a clean checkout", mirror)
	}
	if err := e.createCheckoutDir(); err != nil {
		t.Fatal(err)
	}

	// From here on, the checkout must not need the canonical repository.
	canonical.Close()

	derived, err := e.prepareCheckoutWorkdir(t.Context(), nil, sparseCheckout{}, mirror, []string{"-v"}, false)
	if err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if !derived {
		t.Fatal("prepareCheckoutWorkdir() derived = false, want checkout derived from snapshot")
	}

	if got, want := gitOutputForMirrorTest(t, filepath.Join(e.shell.Getwd(), ".git"), "config", "--get", "remote.origin.url"), e.Repository; got != want {
		t.Errorf("remote.origin.url = %q, want canonical %q", got, want)
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Error("derived checkout is missing the build commit")
	}

	alternates := filepath.Join(e.shell.Getwd(), ".git", "objects", "info", "alternates")
	content, err := os.ReadFile(alternates)
	if err != nil {
		t.Fatalf("derived checkout has no alternates file (--no-local not effective?): %v", err)
	}
	// Git canonicalizes the path it writes to the alternates file: forward
	// slashes and resolved (long) names on Windows, while mirror.dir may use
	// backslashes and 8.3 short names (e.g. BUILDK~1 from TMP). Resolve both
	// sides before comparing.
	if want, got := resolvePathForTest(t, filepath.Join(mirror.dir, "objects")), string(content); !alternatesContainDir(t, got, want) {
		t.Errorf("alternates = %q, want reference to snapshot objects %q", got, want)
	}

	// The build branch's remote-tracking ref must not be stale relative to the
	// build's own commit (syncOriginBranchRef).
	if got, want := gitOutputForMirrorTest(t, filepath.Join(e.shell.Getwd(), ".git"), "rev-parse", "refs/remotes/origin/feature-branch"), commit; got != want {
		t.Errorf("refs/remotes/origin/feature-branch = %q, want build commit %q", got, want)
	}

	// The subsequent fetch is skipped: it must succeed with canonical down.
	if err := e.fetchSource(t.Context(), false, true, nil); err != nil {
		t.Fatalf("fetchSource() error = %v, want fetch skipped after snapshot derive", err)
	}
}

func TestPrepareCheckoutWorkdirSnapshotDeriveFailureFallsBackToCanonical(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e := newSnapshotDeriveExecutor(t, canonical.RepoURL("canonical"), commit)

	// A snapshot path that does not exist: the derive clone fails.
	mirror := mirrorReference{dir: filepath.Join(t.TempDir(), "missing.git"), isSnapshot: true}

	derived, err := e.prepareCheckoutWorkdir(t.Context(), nil, sparseCheckout{}, mirror, []string{"-v"}, false)
	if err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v, want fail-open canonical clone", err)
	}
	if derived {
		t.Fatal("prepareCheckoutWorkdir() derived = true, want canonical fallback")
	}
	if !hasGitCommit(t.Context(), e.shell, ".git", commit) {
		t.Error("canonical fallback clone is missing the build commit")
	}
	// The broken snapshot must not be retained as a reference.
	if osutil.FileExists(filepath.Join(e.shell.Getwd(), ".git", "objects", "info", "alternates")) {
		t.Error("canonical fallback clone retains an alternate into the failed snapshot")
	}
}

func TestFetchSourceSnapshotDeriveKeepsCustomRefspecFetch(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := canonical.CreateRef("canonical", "refs/foo/bar", commit); err != nil {
		t.Fatalf("CreateRef() error = %v\n%s", err, out)
	}
	e := newSnapshotDeriveExecutor(t, canonical.RepoURL("canonical"), commit)
	// A src:dst refspec creates a local ref that job code may read. The
	// snapshot-derive fetch skip must not apply to custom refspec builds.
	e.RefSpec = "+refs/foo/bar:refs/foo/bar"

	mirror, err := e.getOrUpdateMirror(t.Context(), e.Repository, nil)
	if err != nil {
		t.Fatalf("getOrUpdateMirror() error = %v", err)
	}
	if !mirror.isSnapshot {
		t.Fatalf("mirror = %+v, want a per-job snapshot for a clean checkout", mirror)
	}
	if err := e.createCheckoutDir(); err != nil {
		t.Fatal(err)
	}

	derived, err := e.prepareCheckoutWorkdir(t.Context(), nil, sparseCheckout{}, mirror, []string{"-v"}, false)
	if err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if !derived {
		t.Fatal("prepareCheckoutWorkdir() derived = false, want checkout derived from snapshot")
	}

	checkoutGitDir := filepath.Join(e.shell.Getwd(), ".git")
	if gitRefExistsForMirrorTest(checkoutGitDir, "refs/foo/bar") {
		t.Fatal("refs/foo/bar exists in the derived checkout before fetch; test cannot prove the fetch ran")
	}

	// Even though the commit is already present via the snapshot, the custom
	// refspec fetch must still run so its local dst ref is created.
	if err := e.fetchSource(t.Context(), false, true, nil); err != nil {
		t.Fatalf("fetchSource() error = %v", err)
	}
	if got, want := gitOutputForMirrorTest(t, checkoutGitDir, "rev-parse", "refs/foo/bar"), commit; got != want {
		t.Errorf("refs/foo/bar = %q, want build commit %q (custom refspec fetch was skipped?)", got, want)
	}
}

func TestPrepareCheckoutWorkdirIncompatibleFlagsUseSnapshotAsReferenceOnly(t *testing.T) {
	canonical := newOnHostMirrorHTTPRepo(t, "canonical")
	commit, _, err := canonical.PushBranch("canonical", "feature-branch")
	if err != nil {
		t.Fatal(err)
	}
	e := newSnapshotDeriveExecutor(t, canonical.RepoURL("canonical"), commit)

	mirror, err := e.getOrUpdateMirror(t.Context(), e.Repository, nil)
	if err != nil {
		t.Fatalf("getOrUpdateMirror() error = %v", err)
	}
	if !mirror.isSnapshot {
		t.Fatalf("mirror = %+v, want a per-job snapshot for a clean checkout", mirror)
	}
	if err := e.createCheckoutDir(); err != nil {
		t.Fatal(err)
	}

	// --depth requires the canonical remote to own the shallow boundary, so
	// the checkout falls back to today's canonical clone with the snapshot as
	// a --reference.
	derived, err := e.prepareCheckoutWorkdir(t.Context(), nil, sparseCheckout{}, mirror, []string{"--depth=1"}, false)
	if err != nil {
		t.Fatalf("prepareCheckoutWorkdir() error = %v", err)
	}
	if derived {
		t.Fatal("prepareCheckoutWorkdir() derived = true, want canonical clone for shallow flags")
	}
	if got, want := gitOutputForMirrorTest(t, filepath.Join(e.shell.Getwd(), ".git"), "config", "--get", "remote.origin.url"), e.Repository; got != want {
		t.Errorf("remote.origin.url = %q, want canonical %q", got, want)
	}
	// The snapshot is deleted at the end of the job, so a reference to it must
	// always be dissociated, regardless of the checkout mode.
	if osutil.FileExists(filepath.Join(e.shell.Getwd(), ".git", "objects", "info", "alternates")) {
		t.Error("fallback clone retains an alternate into the per-job snapshot")
	}
}

func TestCloneFlagsAllowSnapshotDerive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		flags []string
		want  bool
	}{
		{name: "no flags", flags: nil, want: true},
		{name: "verbose", flags: []string{"-v"}, want: true},
		{name: "filter", flags: []string{"--filter=blob:none"}, want: true},
		{name: "sparse", flags: []string{"--sparse"}, want: true},
		{name: "no tags", flags: []string{"--no-tags"}, want: true},
		{name: "config", flags: []string{"--config", "pack.threads=2"}, want: true},
		{name: "depth", flags: []string{"--depth=1"}, want: false},
		{name: "depth separate value", flags: []string{"--depth", "1"}, want: false},
		{name: "shallow since", flags: []string{"--shallow-since=2024-01-01"}, want: false},
		{name: "single branch", flags: []string{"--single-branch"}, want: false},
		{name: "branch", flags: []string{"--branch", "main"}, want: false},
		{name: "branch short", flags: []string{"-b", "main"}, want: false},
		{name: "origin", flags: []string{"--origin", "upstream"}, want: false},
		{name: "origin short", flags: []string{"-o", "upstream"}, want: false},
		{name: "local", flags: []string{"--local"}, want: false},
		{name: "mirror", flags: []string{"--mirror"}, want: false},
		{name: "bare", flags: []string{"--bare"}, want: false},
		{name: "bundle uri", flags: []string{"--bundle-uri=https://example.com/b"}, want: false},
		{name: "recurse submodules", flags: []string{"--recurse-submodules"}, want: false},
		{name: "separate git dir", flags: []string{"--separate-git-dir=/elsewhere"}, want: false},
		{name: "mixed compatible and not", flags: []string{"-v", "--depth=1"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cloneFlagsAllowSnapshotDerive(tc.flags); got != tc.want {
				t.Errorf("cloneFlagsAllowSnapshotDerive(%v) = %t, want %t", tc.flags, got, tc.want)
			}
		})
	}
}
