package job

import (
	"context"
	"os"
	"testing"

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
		expectErr   bool
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
			s.Start()
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
			defer os.RemoveAll(buildDir)

			tt.executor.BuildPath = buildDir
			tt.executor.Repository = s.RepoURL(tt.projectName)

			checkoutDir, err := os.MkdirTemp("", "checkout-path-")
			assert.NoError(err)
			defer os.RemoveAll(checkoutDir)

			shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

			err = tt.executor.defaultCheckoutPhase(ctx)
			assert.NoError(err)
		})
	}
}
