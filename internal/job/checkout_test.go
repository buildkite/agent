package job

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/race"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/stretchr/testify/require"
)

func TestDefaultCheckoutPhase(t *testing.T) {
	assert := require.New(t)
	ctx := context.Background()

	shell, err := shell.New()
	assert.NoError(err)

	tests := []struct {
		name        string
		executor    *Executor
		projectName string
		checkoutDir string
		refSpec     string
	}{
		{
			name: "Default checkout phase with HEAD commit",
			executor: &Executor{
				shell: shell,
				ExecutorConfig: ExecutorConfig{
					Commit:        "HEAD",
					Branch:        "main",
					CleanCheckout: false,
					GitCleanFlags: "-f -d -x",
				},
			},
			projectName: "project-name-head",
		},
		{
			name: "Default checkout phase with custom refspec",
			executor: &Executor{
				shell: shell,
				ExecutorConfig: ExecutorConfig{
					Commit:        "HEAD",
					Branch:        "main",
					CleanCheckout: false,
					GitCleanFlags: "-f -d -x",
					RefSpec:       "refs/custom",
				},
			},
			projectName: "project-name-refspec",
			refSpec:     "refs/custom",
		},
		{
			name: "Default checkout phase with pull request",
			executor: &Executor{
				shell: shell,
				ExecutorConfig: ExecutorConfig{
					PullRequest:      "124",
					Commit:           "HEAD",
					Branch:           "main",
					CleanCheckout:    false,
					GitCleanFlags:    "-f -d -x",
					PipelineProvider: "github",
				},
			},
			projectName: "project-name-pull-request",
			refSpec:     "refs/pull/124/head",
		},
		{
			name: "Default checkout phase with pull request using merge refspec",
			executor: &Executor{
				shell: shell,
				ExecutorConfig: ExecutorConfig{
					PullRequest:                  "124",
					Commit:                       "HEAD",
					Branch:                       "main",
					CleanCheckout:                false,
					GitCleanFlags:                "-f -d -x",
					PipelineProvider:             "github",
					PullRequestUsingMergeRefspec: true,
				},
			},
			projectName: "project-name-pull-request",
			refSpec:     "refs/pull/124/merge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := require.New(t)

			// configure a global user name and email
			// this is to avoid the git config file being created in the home directory
			// which is not needed for the test
			t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
			t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
			t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
			t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

			s := githttptest.NewServer()
			defer s.Close()

			err = s.CreateRepository(tt.projectName)
			assert.NoError(err)

			out, err := s.InitRepository(tt.projectName)
			if err != nil {
				t.Fatalf("failed to init repository: %v output: %s", err, string(out))
			}

			commit, out, err := s.PushBranch(tt.projectName, "feature-branch")
			if err != nil {
				t.Fatalf("failed to init repository: %v output: %s", err, string(out))
			}

			if tt.refSpec != "" {
				out, err = s.CreateRef(tt.projectName, tt.refSpec, commit)
				if err != nil {
					t.Fatalf("failed to create ref: %v output: %s", err, string(out))
				}
			}

			buildDir, err := os.MkdirTemp("", "build-path-")
			assert.NoError(err)
			t.Cleanup(func() {
				os.RemoveAll(buildDir) //nolint:errcheck // Best-effort cleanup.
			})

			tt.executor.BuildPath = buildDir
			tt.executor.Repository = s.RepoURL(tt.projectName)

			checkoutDir, err := os.MkdirTemp("", "checkout-path-")
			assert.NoError(err)
			t.Cleanup(func() {
				os.RemoveAll(checkoutDir) //nolint:errcheck // Best-effort cleanup.
			})

			shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

			err = tt.executor.defaultCheckoutPhase(ctx)
			assert.NoError(err)
		})
	}
}

func TestSkipCheckout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sh, err := shell.New()
	require.NoError(t, err)

	executor := &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			Repository:   "https://github.com/buildkite/agent.git",
			SkipCheckout: true,
		},
	}

	err = executor.checkout(ctx)
	require.NoError(t, err)
}

func TestDefaultCheckoutPhase_SkipFetchExistingCommits(t *testing.T) {
	t.Parallel()

	assert := require.New(t)
	ctx := context.Background()

	sh, err := shell.New()
	assert.NoError(err)

	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	s := githttptest.NewServer()
	defer s.Close()

	projectName := "project-skip-fetch"

	err = s.CreateRepository(projectName)
	assert.NoError(err)

	out, err := s.InitRepository(projectName)
	if err != nil {
		t.Fatalf("failed to init repository: %v output: %s", err, string(out))
	}

	commit, out, err := s.PushBranch(projectName, "main")
	if err != nil {
		t.Fatalf("failed to push branch: %v output: %s", err, string(out))
	}

	buildDir, err := os.MkdirTemp("", "build-path-")
	assert.NoError(err)
	t.Cleanup(func() {
		os.RemoveAll(buildDir) //nolint:errcheck // Best-effort cleanup.
	})

	checkoutDir, err := os.MkdirTemp("", "checkout-path-")
	assert.NoError(err)
	t.Cleanup(func() {
		os.RemoveAll(checkoutDir) //nolint:errcheck // Best-effort cleanup.
	})

	// First checkout: clone the repo so the commit is available locally
	initialExecutor := &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			Commit:        commit,
			Branch:        "main",
			BuildPath:     buildDir,
			Repository:    s.RepoURL(projectName),
			CleanCheckout: false,
			GitCleanFlags: "-f -d -x",
		},
	}

	sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

	err = initialExecutor.defaultCheckoutPhase(ctx)
	assert.NoError(err)

	// Second checkout: with SkipFetchExistingCommits enabled.
	// The commit is already present locally, so fetch should be skipped.
	skipFetchExecutor := &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			Commit:                   commit,
			Branch:                   "main",
			BuildPath:                buildDir,
			Repository:               s.RepoURL(projectName),
			CleanCheckout:            false,
			GitCleanFlags:            "-f -d -x",
			SkipFetchExistingCommits: true,
		},
	}

	err = skipFetchExecutor.defaultCheckoutPhase(ctx)
	assert.NoError(err)
}

func TestDefaultCheckoutPhase_DelayedRefCreation(t *testing.T) {
	if race.IsRaceTest {
		t.Skip("this test simulates the agent recovering from a race condition, and needs to create one to test it.")
	}

	assert := require.New(t)
	ctx := t.Context()

	shell, err := shell.New()
	assert.NoError(err)

	tt := struct {
		executor    *Executor
		projectName string
		checkoutDir string
		refSpec     string
	}{
		executor: &Executor{
			shell: shell,
			ExecutorConfig: ExecutorConfig{
				PullRequest:      "124",
				Commit:           "HEAD",
				Branch:           "main",
				CleanCheckout:    false,
				GitCleanFlags:    "-f -d -x",
				PipelineProvider: "github",
			},
		},
		projectName: "project-name-pull-request",
		refSpec:     "refs/pull/124/head",
	}

	// configure a global user name and email
	// this is to avoid the git config file being created in the home directory
	// which is not needed for the test
	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	s := githttptest.NewServer()
	defer s.Close()

	err = s.CreateRepository(tt.projectName)
	assert.NoError(err)

	out, err := s.InitRepository(tt.projectName)
	if err != nil {
		t.Fatalf("failed to init repository: %v output: %s", err, string(out))
	}

	commit, out, err := s.PushBranch(tt.projectName, "feature-branch")
	if err != nil {
		t.Fatalf("failed to init repository: %v output: %s", err, string(out))
	}

	buildDir, err := os.MkdirTemp("", "build-path-")
	assert.NoError(err)
	t.Cleanup(func() {
		os.RemoveAll(buildDir) //nolint:errcheck // Best-effort cleanup.
	})

	tt.executor.BuildPath = buildDir
	tt.executor.Repository = s.RepoURL(tt.projectName)

	checkoutDir, err := os.MkdirTemp("", "checkout-path-")
	assert.NoError(err)
	t.Cleanup(func() {
		os.RemoveAll(checkoutDir) //nolint:errcheck // Best-effort cleanup.
	})

	// Concurrently sleep for 5 seconds to delay ref being created
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			// continue below
		}
		out, err = s.CreateRef(tt.projectName, tt.refSpec, commit)
		if err != nil {
			t.Errorf("failed to create ref: %v output: %s", err, string(out))
		}
	}()

	shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

	err = tt.executor.defaultCheckoutPhase(ctx)
	assert.NoError(err)
}
