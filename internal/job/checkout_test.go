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
					RefSpec:       "refs/pull/124/head",
				},
			},
			projectName: "project-name-refspec",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := require.New(t)

			t.Setenv("GIT_CONFIG_GLOBAL", "~/.gitconfig.empty")

			s := githttptest.NewServer()
			s.Start()
			defer s.Close()

			err = s.CreateRepository(tt.projectName)
			assert.NoError(err)

			err = s.InitRepository(tt.projectName)
			assert.NoError(err)

			commit, err := s.PushBranch(tt.projectName, "feature-branch")
			assert.NoError(err)

			// create a custom ref to test
			err = s.CreateRef(tt.projectName, "refs/pull/124/head", commit)
			assert.NoError(err)

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
