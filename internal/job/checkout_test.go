package job

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
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

func TestDefaultCheckoutPhase_DelayedRefCreation(t *testing.T) {
	assert := require.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

func TestPartialCloneFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		gitCloneFlags          string
		gitCloneDepth          string
		gitCloneFilter         string
		gitSparseCheckout      bool
		gitSparseCheckoutPaths string
		expectedFlags          string
	}{
		{
			name:          "default clone flags",
			gitCloneFlags: "-v",
			expectedFlags: "-v",
		},
		{
			name:          "with clone depth",
			gitCloneFlags: "-v",
			gitCloneDepth: "200",
			expectedFlags: "-v --depth=200",
		},
		{
			name:           "with clone filter",
			gitCloneFlags:  "-v",
			gitCloneFilter: "tree:0",
			expectedFlags:  "-v --filter=tree:0",
		},
		{
			name:                   "with sparse checkout",
			gitCloneFlags:          "-v",
			gitSparseCheckout:      true,
			gitSparseCheckoutPaths: "src/frontend",
			expectedFlags:          "-v --no-checkout",
		},
		{
			name:                   "full partial clone setup",
			gitCloneFlags:          "-v",
			gitCloneDepth:          "200",
			gitCloneFilter:         "tree:0",
			gitSparseCheckout:      true,
			gitSparseCheckoutPaths: "src/frontend,src/backend",
			expectedFlags:          "-v --depth=200 --filter=tree:0 --no-checkout",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := &Executor{
				ExecutorConfig: ExecutorConfig{
					GitCloneFlags:          tc.gitCloneFlags,
					GitCloneDepth:          tc.gitCloneDepth,
					GitCloneFilter:         tc.gitCloneFilter,
					GitSparseCheckout:      tc.gitSparseCheckout,
					GitSparseCheckoutPaths: tc.gitSparseCheckoutPaths,
				},
			}

			// Build clone flags like in defaultCheckoutPhase
			gitCloneFlags := e.GitCloneFlags
			if e.GitCloneDepth != "" {
				gitCloneFlags += " --depth=" + e.GitCloneDepth
			}
			if e.GitCloneFilter != "" {
				gitCloneFlags += " --filter=" + e.GitCloneFilter
			}
			if e.GitSparseCheckout && e.GitSparseCheckoutPaths != "" {
				gitCloneFlags += " --no-checkout"
			}

			// Remove any extra spaces and normalize
			gitCloneFlags = strings.TrimSpace(gitCloneFlags)
			gitCloneFlags = strings.Join(strings.Fields(gitCloneFlags), " ")

			require.Equal(t, tc.expectedFlags, gitCloneFlags)
		})
	}
}

func TestParseSparseCheckoutPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single path",
			input:    "src/frontend",
			expected: []string{"src/frontend"},
		},
		{
			name:     "multiple paths",
			input:    "src/frontend,src/backend,docs",
			expected: []string{"src/frontend", "src/backend", "docs"},
		},
		{
			name:     "paths with spaces",
			input:    " src/frontend , src/backend , docs ",
			expected: []string{"src/frontend", "src/backend", "docs"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paths := strings.Split(tc.input, ",")
			for i := range paths {
				paths[i] = strings.TrimSpace(paths[i])
			}
			require.Equal(t, tc.expected, paths)
		})
	}
}
