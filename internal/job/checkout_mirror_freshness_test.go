package job

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/shell"
)

// mirrorsTempDir makes a short temp dir for GitMirrorsPath with best-effort
// cleanup. Not t.TempDir(): its path embeds this file's long test names, and
// the deepest snapshot paths (mirrors path + "snapshots" +
// dirForRepository(repo URL) + a pack file name) then exceed Windows'
// 260-character MAX_PATH, failing git with a bare exit status 128. Removal is
// best-effort because on Windows git child processes can hold handles past
// exit, which t.TempDir()'s strict cleanup would fail on.
func mirrorsTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mirrors-")
	if err != nil {
		t.Fatalf("os.MkdirTemp error = %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) }) //nolint:errcheck // Best-effort cleanup.
	return dir
}

// TestUpdateGitMirrorBranchTipFreshDespiteSameNamedTag exercises the
// branchTipFresh invariant end to end through getOrUpdateMirror: when the
// warm-path mirror update claims freshness, refs/heads/<branch> in the
// snapshot must be the canonical branch tip — even when a tag shares the
// branch's name. A bare-name fetch would resolve the tag (refs/tags/ wins),
// leave the branch ref stale, and let snapshot verification vouch for a
// commit against an outdated tip.
func TestUpdateGitMirrorBranchTipFreshDespiteSameNamedTag(t *testing.T) {
	ctx := t.Context()
	sh, repoURL, commit := newFileBackedRepo(t, ctx, "mirror-fresh")

	// Branch "release": a <- b, pushed to canonical.
	base := commit("a.txt")
	firstTip := commit("b.txt")
	if err := sh.Command("git", "branch", "-m", "release").Run(ctx); err != nil {
		t.Fatalf("git branch -m release error = %v", err)
	}
	if err := sh.Command("git", "push", "origin", "release").Run(ctx); err != nil {
		t.Fatalf("git push release error = %v", err)
	}

	// Tag "release" on a divergent commit (a <- t), pushed to canonical.
	if err := sh.Command("git", "checkout", "-b", "tagline", base).Run(ctx); err != nil {
		t.Fatalf("git checkout -b tagline error = %v", err)
	}
	commit("t.txt")
	if err := sh.Command("git", "tag", "release").Run(ctx); err != nil {
		t.Fatalf("git tag release error = %v", err)
	}
	if err := sh.Command("git", "push", "origin", "refs/tags/release").Run(ctx); err != nil {
		t.Fatalf("git push tag release error = %v", err)
	}

	e := New(ExecutorConfig{
		Repository:            repoURL,
		Commit:                firstTip,
		Branch:                "release",
		PullRequest:           "false",
		GitMirrorsPath:        mirrorsTempDir(t),
		GitMirrorsLockTimeout: 30,
		CleanCheckout:         true,
		Phases:                []string{"checkout", "command"},
	})
	e.shell = shell.NewTestShell(t, shell.WithSignalGracePeriod(10*time.Millisecond))

	snapshotBranchTip := func(mirror mirrorReference) string {
		t.Helper()
		out, err := e.shell.Command(
			"git", "--git-dir", mirror.dir, "rev-parse", "refs/heads/release",
		).RunAndCaptureStdout(ctx)
		if err != nil {
			t.Fatalf("rev-parse refs/heads/release in snapshot error = %v", err)
		}
		return strings.TrimSpace(out)
	}

	// First job: fresh clone from canonical.
	mirror, err := e.getOrUpdateMirror(ctx, e.Repository, nil)
	if err != nil {
		t.Fatalf("getOrUpdateMirror() error = %v", err)
	}
	if !mirror.isSnapshot || !mirror.branchTipFresh {
		t.Fatalf("getOrUpdateMirror() = %+v, want a fresh snapshot from a canonical clone", mirror)
	}
	if got := snapshotBranchTip(mirror); got != firstTip {
		t.Errorf("snapshot refs/heads/release = %s, want %s", got, firstTip)
	}

	// The branch advances on canonical: a <- b <- c.
	if err := sh.Command("git", "checkout", "release").Run(ctx); err != nil {
		t.Fatalf("git checkout release error = %v", err)
	}
	newTip := commit("c.txt")
	// The push must name the ref fully: with the tag present, a bare
	// "release" is ambiguous on the pushing side too.
	if err := sh.Command("git", "push", "origin", "refs/heads/release:refs/heads/release").Run(ctx); err != nil {
		t.Fatalf("git push release error = %v", err)
	}

	// Second job for the new commit: the warm-path update must fetch
	// refs/heads/release itself, not the same-named tag.
	e.Commit = newTip
	mirror, err = e.getOrUpdateMirror(ctx, e.Repository, nil)
	if err != nil {
		t.Fatalf("getOrUpdateMirror() error = %v", err)
	}
	if !mirror.isSnapshot || !mirror.branchTipFresh {
		t.Fatalf("getOrUpdateMirror() = %+v, want a fresh snapshot from a warm branch fetch", mirror)
	}
	if got := snapshotBranchTip(mirror); got != newTip {
		t.Errorf("snapshot refs/heads/release = %s, want %s (bare-name fetch resolves the tag and leaves the branch stale)", got, newTip)
	}
}

// TestUpdateGitMirrorNoFreshnessForTagBuilds pins the conservative cases: tag
// builds keep the historical bare-name fetch and must not claim a fresh
// branch tip, and a mirror update that skips the fetch (commit already
// present) must not either.
func TestUpdateGitMirrorNoFreshnessForTagBuilds(t *testing.T) {
	ctx := t.Context()
	sh, repoURL, commit := newFileBackedRepo(t, ctx, "mirror-tag")

	commit("a.txt")
	tip := commit("b.txt")
	if err := sh.Command("git", "branch", "-m", "v1").Run(ctx); err != nil {
		t.Fatalf("git branch -m v1 error = %v", err)
	}
	if err := sh.Command("git", "push", "origin", "v1").Run(ctx); err != nil {
		t.Fatalf("git push v1 error = %v", err)
	}

	e := New(ExecutorConfig{
		Repository:            repoURL,
		Commit:                tip,
		Branch:                "v1",
		Tag:                   "v1",
		PullRequest:           "false",
		GitMirrorsPath:        mirrorsTempDir(t),
		GitMirrorsLockTimeout: 30,
		CleanCheckout:         true,
		Phases:                []string{"checkout", "command"},
	})
	e.shell = shell.NewTestShell(t, shell.WithSignalGracePeriod(10*time.Millisecond))

	// First call clones the mirror; run again with a new commit so the second
	// call takes the warm fetch path with Tag set.
	if _, err := e.getOrUpdateMirror(ctx, e.Repository, nil); err != nil {
		t.Fatalf("getOrUpdateMirror() error = %v", err)
	}
	newTip := commit("c.txt")
	if err := sh.Command("git", "push", "origin", "v1").Run(ctx); err != nil {
		t.Fatalf("git push v1 error = %v", err)
	}
	e.Commit = newTip
	mirror, err := e.getOrUpdateMirror(ctx, e.Repository, nil)
	if err != nil {
		t.Fatalf("getOrUpdateMirror() error = %v", err)
	}
	if !mirror.isSnapshot {
		t.Fatalf("getOrUpdateMirror() = %+v, want a snapshot", mirror)
	}
	if mirror.branchTipFresh {
		t.Errorf("branchTipFresh = true for a tag build's warm fetch, want false")
	}

	// Same commit again: the fetch is skipped, so no freshness either.
	mirror, err = e.getOrUpdateMirror(ctx, e.Repository, nil)
	if err != nil {
		t.Fatalf("getOrUpdateMirror() error = %v", err)
	}
	if mirror.branchTipFresh {
		t.Errorf("branchTipFresh = true when the mirror fetch was skipped, want false")
	}
}
