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
		name string
		// mode is BUILDKITE_GIT_FETCH_BASE_BRANCH; "" pins that the zero value is off
		// for programmatic ExecutorConfig consumers.
		mode         string
		branch       string
		pullRequest  string
		skipExisting bool
		env          map[string]string
		// Whether origin/main should have moved to the remote's new tip.
		wantFetched bool
	}{
		{
			name:        "fetches the pull request base branch",
			mode:        GitFetchBaseBranchOptimistic,
			branch:      "feature-branch",
			env:         map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched: true,
		},
		{
			name:        "fetches the pull request base branch under strict",
			mode:        GitFetchBaseBranchStrict,
			branch:      "feature-branch",
			env:         map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched: true,
		},
		{
			name:        "does nothing when off",
			mode:        GitFetchBaseBranchOff,
			branch:      "feature-branch",
			env:         map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched: false,
		},
		{
			name:        "does nothing when unset",
			mode:        "",
			branch:      "feature-branch",
			env:         map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched: false,
		},
		{
			name:        "falls back to the pipeline default branch",
			mode:        GitFetchBaseBranchOptimistic,
			branch:      "feature-branch",
			env:         map[string]string{"BUILDKITE_PIPELINE_DEFAULT_BRANCH": "refs/heads/main"},
			wantFetched: true,
		},
		{
			name:   "prefers the pull request base over the default branch",
			mode:   GitFetchBaseBranchOptimistic,
			branch: "feature-branch",
			env: map[string]string{
				"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main",
				"BUILDKITE_PIPELINE_DEFAULT_BRANCH":  "no-such-branch",
			},
			wantFetched: true,
		},
		{
			name:        "skips the branch being built",
			mode:        GitFetchBaseBranchOptimistic,
			branch:      "main",
			env:         map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched: false,
		},
		{
			// A branch build of the default branch: the build the merge queue runs, and
			// the one a PR build's stale origin/main would otherwise be diffed against.
			// There is nothing to prepare, in strict mode included.
			name:        "skips a default branch build",
			mode:        GitFetchBaseBranchStrict,
			branch:      "main",
			pullRequest: "false",
			env:         map[string]string{"BUILDKITE_PIPELINE_DEFAULT_BRANCH": "main"},
			wantFetched: false,
		},
		{
			name:        "skips when no base branch is known",
			mode:        GitFetchBaseBranchOptimistic,
			branch:      "feature-branch",
			env:         map[string]string{},
			wantFetched: false,
		},
		{
			// The job asked for the base branch, so it gets it whether or not its own
			// commit still needs fetching.
			name:         "fetches even when the job's own fetch is skipped",
			mode:         GitFetchBaseBranchOptimistic,
			branch:       "feature-branch",
			skipExisting: true,
			env:          map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "main"},
			wantFetched:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBaseBranchFixture(t)

			e := newBaseBranchFetchExecutor(t, f)
			e.Branch = tt.branch
			if tt.pullRequest != "" {
				e.PullRequest = tt.pullRequest
			}
			e.GitFetchBaseBranch = tt.mode
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

// Strict mode exists because a best-effort fetch cannot promise a current base
// branch: the guarantee requires failing closed on every way the fetch can not
// happen, both a base branch that no longer resolves and a job that names none.
func TestFetchSourceBaseBranchStrictFailures(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		wantIs error
	}{
		{
			name: "a base branch that no longer resolves fails the checkout",
			env:  map[string]string{"BUILDKITE_PULL_REQUEST_BASE_BRANCH": "deleted-branch"},
		},
		{
			// Nothing names a base branch, so a diff taken later would read whatever
			// origin/<something> an earlier build left in the checkout directory.
			// Retrying can't discover a base branch, so the checkout fails fast on it.
			name:   "no base branch at all fails the checkout",
			env:    map[string]string{},
			wantIs: errNoBaseBranchToFetch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newBaseBranchFixture(t)

			e := newBaseBranchFetchExecutor(t, f)
			e.Branch = "feature-branch"
			e.GitFetchBaseBranch = GitFetchBaseBranchStrict
			for k, v := range tt.env {
				e.shell.Env.Set(k, v)
			}

			start := time.Now()
			err := e.fetchSource(t.Context(), false, nil)
			if err == nil {
				t.Fatal("e.fetchSource(ctx, false, nil) error = nil, want an error")
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("e.fetchSource(ctx, false, nil) error = %v, want it to wrap %v", err, tt.wantIs)
			}
			// Neither failure is worth retrying, and both are reached on every job
			// once configured, so both must be quick: retrying the missing ref
			// through the whole budget takes about a minute and a half.
			if elapsed := time.Since(start); elapsed > 30*time.Second {
				t.Errorf("e.fetchSource(ctx, false, nil) took %s, want it to fail without exhausting the retry budget", elapsed)
			}
		})
	}
}

// The reason to retry at all: an outage at the remote is the usual way this fetch
// fails in CI, and a single attempt would leave a stale ref behind — silently under
// optimistic, and as a failed job under strict.
func TestFetchSourceBaseBranchRetriesWhileTheRemoteIsUnavailable(t *testing.T) {
	tests := []struct {
		mode string
		// failRequests rejects that many requests before the server serves git
		// again. optimistic's budget is small by design, so it is given a blip it
		// can cover; strict is given more than optimistic would survive, pinning
		// that its budget really is the larger one.
		failRequests int
	}{
		{mode: GitFetchBaseBranchOptimistic, failRequests: 1},
		{mode: GitFetchBaseBranchStrict, failRequests: 3},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			f := newBaseBranchFixture(t)

			e := newBaseBranchFetchExecutor(t, f)
			e.Branch = "feature-branch"
			e.GitFetchBaseBranch = tt.mode
			e.shell.Env.Set("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "main")

			f.server.FailNextRequests(tt.failRequests)

			if err := e.fetchSource(t.Context(), false, nil); err != nil {
				t.Fatalf("e.fetchSource(ctx, false, nil) error = %v, want nil", err)
			}
			if got := gitRevParseForBaseBranchTest(t, f.checkout, "refs/remotes/origin/main"); got != f.currentMain {
				t.Errorf("origin/main = %q, want %q (stale = %q)", got, f.currentMain, f.staleMain)
			}
		})
	}
}

// optimistic has already decided the job proceeds either way, so a remote that
// stays down must not hold it up for anything like the time strict would spend.
func TestFetchSourceBaseBranchOptimisticGivesUpQuickly(t *testing.T) {
	f := newBaseBranchFixture(t)

	e := newBaseBranchFetchExecutor(t, f)
	e.Branch = "feature-branch"
	e.GitFetchBaseBranch = GitFetchBaseBranchOptimistic
	e.shell.Env.Set("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "main")

	// More than any budget here would survive. This calls fetchBaseBranch rather
	// than fetchSource because a remote this far down also fails the job's own
	// fetch, and the budget is what's under test.
	f.server.FailNextRequests(100)

	start := time.Now()
	if err := e.fetchBaseBranch(t.Context(), GitFetchBaseBranchOptimistic, e.GitFetchFlags); err != nil {
		t.Fatalf("e.fetchBaseBranch(ctx, optimistic, flags) error = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("e.fetchBaseBranch(ctx, optimistic, flags) took %s, want optimistic to give up promptly", elapsed)
	}
	if got := gitRevParseForBaseBranchTest(t, f.checkout, "refs/remotes/origin/main"); got != f.staleMain {
		t.Errorf("origin/main = %q, want the stale %q left in place", got, f.staleMain)
	}
}

// An invalid mode arriving from job env must not silently disable the fetch a job
// asked for, so it fails the checkout rather than degrading to off.
func TestFetchSourceBaseBranchRejectsInvalidMode(t *testing.T) {
	f := newBaseBranchFixture(t)

	e := newBaseBranchFetchExecutor(t, f)
	e.Branch = "feature-branch"
	e.GitFetchBaseBranch = "yes-please"
	e.shell.Env.Set("BUILDKITE_PULL_REQUEST_BASE_BRANCH", "main")

	err := e.fetchSource(t.Context(), false, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid git fetch base branch mode") {
		t.Fatalf("e.fetchSource(ctx, false, nil) error = %v, want an invalid mode error", err)
	}
}

// Under optimistic, a base branch that no longer resolves must not fail the
// checkout: the job's own source is already fetched by then, and only a diff taken
// later is affected.
func TestFetchSourceBaseBranchMissingIsNotFatalWhenOptimistic(t *testing.T) {
	f := newBaseBranchFixture(t)

	e := newBaseBranchFetchExecutor(t, f)
	e.Branch = "feature-branch"
	e.GitFetchBaseBranch = GitFetchBaseBranchOptimistic
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
	e.GitFetchBaseBranch = GitFetchBaseBranchOptimistic
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
