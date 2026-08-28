package job

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/internal/job/githttptest"
	"github.com/buildkite/agent/v4/internal/shell"
)

// The reused checkout is the case GitFetchBaseBranch exists for: the job's own fetch
// asks for refs/pull/N/head and the commit, so refs/remotes/origin/<base> keeps
// whatever an earlier build in the same directory left there.
func TestFetchSourceBaseBranch(t *testing.T) {
	tests := []struct {
		name            string
		fetchBaseBranch bool
		branch          string
		skipExisting    bool
		env             map[string]string
		// Whether origin/main should have moved to the remote's new tip.
		wantFetched bool
	}{
		{
			name:            "fetches the pull request base branch",
			fetchBaseBranch: true,
			branch:          "feature-branch",
			env:             map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched:     true,
		},
		{
			name:            "does nothing when disabled",
			fetchBaseBranch: false,
			branch:          "feature-branch",
			env:             map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched:     false,
		},
		{
			name:            "falls back to the pipeline default branch",
			fetchBaseBranch: true,
			branch:          "feature-branch",
			env:             map[string]string{"BUILDKITE_PIPELINE_DEFAULT_BRANCH": "refs/heads/main"},
			wantFetched:     true,
		},
		{
			name:            "prefers the pull request base over the default branch",
			fetchBaseBranch: true,
			branch:          "feature-branch",
			env: map[string]string{
				"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main",
				"BUILDKITE_PIPELINE_DEFAULT_BRANCH":  "no-such-branch",
			},
			wantFetched: true,
		},
		{
			name:            "skips the branch being built",
			fetchBaseBranch: true,
			branch:          "main",
			env:             map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched:     false,
		},
		{
			name:            "skips when no base branch is known",
			fetchBaseBranch: true,
			branch:          "feature-branch",
			env:             map[string]string{},
			wantFetched:     false,
		},
		{
			// The job asked for the base branch, so it gets it whether or not its own
			// commit still needs fetching.
			name:            "fetches even when the job's own fetch is skipped",
			fetchBaseBranch: true,
			branch:          "feature-branch",
			skipExisting:    true,
			env:             map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBaseBranchFixture(t)

			e := newBaseBranchFetchExecutor(t, f)
			e.Branch = tt.branch
			e.GitFetchBaseBranch = tt.fetchBaseBranch
			e.GitSkipFetchExistingCommits = tt.skipExisting
			for k, v := range tt.env {
				e.shell.Env.Set(k, v)
			}

			if err := e.fetchSource(t.Context(), false, nil); err != nil {
				t.Fatalf("e.fetchSource(ctx, false, nil) error = %v, want nil", err)
			}

			want := f.staleMain
			if tt.wantFetched {
				want = f.currentMain
			}
			if got := gitRevParseForBaseBranchTest(t, f.checkout, "refs/remotes/origin/main"); got != want {
				t.Errorf("origin/main = %q, want %q (stale = %q, current = %q)",
					got, want, f.staleMain, f.currentMain)
			}
		})
	}
}

// A base branch that no longer resolves must not fail the checkout: the job's own
// source is already fetched by then, and only a diff taken later is affected.
func TestFetchSourceBaseBranchMissingIsNotFatal(t *testing.T) {
	f := newBaseBranchFixture(t)

	e := newBaseBranchFetchExecutor(t, f)
	e.Branch = "feature-branch"
	e.GitFetchBaseBranch = true
	e.shell.Env.Set("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "deleted-branch")

	if err := e.fetchSource(t.Context(), false, nil); err != nil {
		t.Fatalf("e.fetchSource(ctx, false, nil) error = %v, want nil", err)
	}
	if got := gitRevParseForBaseBranchTest(t, f.checkout, "refs/remotes/origin/deleted-branch"); got != "" {
		t.Errorf("origin/deleted-branch = %q, want it to be absent", got)
	}
}

// The refspec is explicit and forced for two reasons a plain `git fetch origin main`
// would leave to chance: the remote-tracking ref must land even where
// remote.origin.fetch does not map it, and it must follow a base branch that was
// force-pushed rather than stopping at a non-fast-forward rejection.
func TestFetchSourceBaseBranchUpdatesRefWithoutRefmapAndAfterForcePush(t *testing.T) {
	f := newBaseBranchFixture(t)

	// A checkout that maps nothing back into refs/remotes/origin/*.
	runGitForBaseBranchTest(t, f.checkout, "config", "--unset-all", "remote.origin.fetch")

	e := newBaseBranchFetchExecutor(t, f)
	e.Branch = "feature-branch"
	e.GitFetchBaseBranch = true
	e.shell.Env.Set("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "main")

	if err := e.fetchSource(t.Context(), false, nil); err != nil {
		t.Fatalf("e.fetchSource(ctx, false, nil) error = %v, want nil", err)
	}
	if got := gitRevParseForBaseBranchTest(t, f.checkout, "refs/remotes/origin/main"); got != f.currentMain {
		t.Fatalf("origin/main = %q, want %q", got, f.currentMain)
	}

	// Rewind the base branch, which is what a force-push looks like from here.
	if out, err := f.server.CreateRef("base-branch", "refs/heads/main", f.staleMain); err != nil {
		t.Fatalf("s.CreateRef(refs/heads/main) error = %v, output: %s", err, string(out))
	}

	if err := e.fetchSource(t.Context(), false, nil); err != nil {
		t.Fatalf("e.fetchSource(ctx, false, nil) after force-push error = %v, want nil", err)
	}
	if got := gitRevParseForBaseBranchTest(t, f.checkout, "refs/remotes/origin/main"); got != f.staleMain {
		t.Errorf("origin/main = %q after the base branch was rewound, want %q", got, f.staleMain)
	}
}

// baseBranchFixture is a remote whose main has moved on, and a checkout that has not
// heard about it — cloned before the move, and holding the pull request's commit.
type baseBranchFixture struct {
	server      *githttptest.Server
	repository  string
	checkout    string
	commit      string
	staleMain   string
	currentMain string
}

func newBaseBranchFixture(t *testing.T) *baseBranchFixture {
	t.Helper()

	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	const repoName = "base-branch"

	s := githttptest.NewServer()
	t.Cleanup(s.Close)

	if err := s.CreateRepository(repoName); err != nil {
		t.Fatalf("s.CreateRepository(%q) error = %v, want nil", repoName, err)
	}
	if out, err := s.InitRepository(repoName); err != nil {
		t.Fatalf("s.InitRepository(%q) error = %v, output: %s", repoName, err, string(out))
	}

	commit, out, err := s.PushBranch(repoName, "feature-branch")
	if err != nil {
		t.Fatalf("s.PushBranch(%q, feature-branch) error = %v, output: %s", repoName, err, string(out))
	}
	if out, err := s.CreateRef(repoName, "refs/pull/124/head", commit); err != nil {
		t.Fatalf("s.CreateRef(%q, refs/pull/124/head) error = %v, output: %s", repoName, err, string(out))
	}

	f := &baseBranchFixture{
		server:     s,
		repository: s.RepoURL(repoName),
		commit:     commit,
	}

	// Clone before main moves, as a reused checkout would have.
	f.checkout = cloneForBaseBranchTest(t, f.repository)
	f.staleMain = gitRevParseForBaseBranchTest(t, f.checkout, "refs/remotes/origin/main")
	if f.staleMain == "" {
		t.Fatal("the clone has no origin/main to go stale")
	}

	// Move main, without touching any ref this job's own fetch asks for.
	advanced, out, err := s.PushBranch(repoName, "advanced-main")
	if err != nil {
		t.Fatalf("s.PushBranch(%q, advanced-main) error = %v, output: %s", repoName, err, string(out))
	}
	if out, err := s.CreateRef(repoName, "refs/heads/main", advanced); err != nil {
		t.Fatalf("s.CreateRef(%q, refs/heads/main) error = %v, output: %s", repoName, err, string(out))
	}
	f.currentMain = advanced

	if f.staleMain == f.currentMain {
		t.Fatal("main did not move, so a stale origin/main is indistinguishable from a fetched one")
	}

	return f
}

func newBaseBranchFetchExecutor(t *testing.T, f *baseBranchFixture) *Executor {
	t.Helper()

	e := New(ExecutorConfig{
		Repository:       f.repository,
		Commit:           f.commit,
		PullRequest:      "124",
		PipelineProvider: "github",
		GitFetchFlags:    "-v --prune",
	})
	e.shell = shell.NewTestShell(t, shell.WithSignalGracePeriod(10*time.Millisecond))
	if err := e.shell.Chdir(f.checkout); err != nil {
		t.Fatalf("e.shell.Chdir(%q) error = %v, want nil", f.checkout, err)
	}
	t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
			e.checkoutRoot = nil
		}
	})
	return e
}

func cloneForBaseBranchTest(t *testing.T, repository string) string {
	t.Helper()

	// os.MkdirTemp rather than t.TempDir(): git child processes can outlive their
	// exit on Windows, which strict cleanup would fail on.
	checkout, err := os.MkdirTemp("", "checkout-path-")
	if err != nil {
		t.Fatalf("os.MkdirTemp(checkout-path-) error = %v, want nil", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(checkout) //nolint:errcheck // Best-effort cleanup.
	})

	cmd := exec.Command("git", "clone", "--", repository, checkout)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git clone %q error = %v, output: %s", repository, err, string(out))
	}
	return checkout
}

func runGitForBaseBranchTest(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v error = %v, output: %s", args, err, string(out))
	}
}

func gitRevParseForBaseBranchTest(t *testing.T, dir, rev string) string {
	t.Helper()

	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", rev)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// --quiet makes "no such ref" an exit code rather than a message.
			return ""
		}
		t.Fatalf("git rev-parse %q error = %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}
