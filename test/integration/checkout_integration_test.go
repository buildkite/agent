package integration

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lox/bintest"
)

func bootstrapPath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "templates/bootstrap.sh")
}

func expectCheckoutGitCommands(git *bintest.Mock, repo string) {
	git.ExpectAll([][]interface{}{
		{"clone", "-v", "--", repo, "."},
		{"clean", "-fdq"},
		{"submodule", "foreach", "--recursive", "git", "clean", "-fdq"},
		{"fetch", "-v", "origin", "master"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"submodule", "sync", "--recursive"},
		{"submodule", "update", "--init", "--recursive", "--force"},
		{"submodule", "foreach", "--recursive", "git", "reset", "--hard"},
		{"clean", "-fdq"},
		{"submodule", "foreach", "--recursive", "git", "clean", "-fdq"},
		{"show", "HEAD", "-s", "--format=fuller", "--no-color"},
		{"branch", "--contains", "HEAD", "--no-color"},
	})

	git.Expect("--version").
		AndWriteToStdout(`git version 2.13.3`).
		AndExitWith(0)
}

func expectCheckoutBuildkiteAgentCommands(agent *bintest.Mock) {
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(1)
	agent.
		Expect("meta-data", "set", "buildkite:git:commit", bintest.MatchAny()).
		AndExitWith(0)
	agent.
		Expect("meta-data", "set", "buildkite:git:branch", bintest.MatchAny()).
		AndExitWith(0)
}

func TestCheckingOutLocalGitProject(t *testing.T) {
	tester, err := NewBootstrapTesterWithGitRepository(bootstrapPath())
	if err != nil {
		t.Fatal(err)
	}

	env := []string{
		`BUILDKITE_AGENT_NAME=test-agent`,
		`BUILDKITE_PROJECT_SLUG=test-project`,
		`BUILDKITE_REPO=` + tester.Repo.Path,
		`BUILDKITE_PULL_REQUEST=`,
		`BUILDKITE_PROJECT_PROVIDER=git`,
		`BUILDKITE_COMMIT=HEAD`,
		`BUILDKITE_BRANCH=master`,
		`BUILDKITE_COMMAND_EVAL=true`,
		`BUILDKITE_COMMAND=magic llamas`,
		`BUILDKITE_JOB_ID=1111-1111-1111-1111`,
		`BUILDKITE_ARTIFACT_PATHS=`,
	}

	agent := tester.Mock("buildkite-agent", t)
	expectCheckoutBuildkiteAgentCommands(agent)

	magic := tester.Mock("magic", t)
	magic.
		Expect("llamas").
		AndWriteToStdout("llamas rock\n").
		AndExitWith(0)

	git := tester.Mock("git", t).PassthroughToLocalCommand()
	expectCheckoutGitCommands(git, tester.Repo.Path)

	if err = tester.Run(env...); err != nil {
		t.Fatal(err)
	}

	if err := tester.CheckMocksAndClose(t); err != nil {
		t.Fatal(err)
	}
}

func TestCheckingOutFiresCommitHooks(t *testing.T) {
	tester, err := NewBootstrapTesterWithGitRepository(bootstrapPath())
	if err != nil {
		t.Fatal(err)
	}

	env := []string{
		`BUILDKITE_AGENT_NAME=test-agent`,
		`BUILDKITE_PROJECT_SLUG=test-project`,
		`BUILDKITE_REPO=` + tester.Repo.Path,
		`BUILDKITE_PULL_REQUEST=`,
		`BUILDKITE_PROJECT_PROVIDER=git`,
		`BUILDKITE_COMMIT=HEAD`,
		`BUILDKITE_BRANCH=master`,
		`BUILDKITE_COMMAND_EVAL=true`,
		`BUILDKITE_COMMAND=magic llamas`,
		`BUILDKITE_JOB_ID=1111-1111-1111-1111`,
		`BUILDKITE_ARTIFACT_PATHS=`,
	}

	agent := tester.Mock("buildkite-agent", t)
	expectCheckoutBuildkiteAgentCommands(agent)

	magic := tester.Mock("magic", t)
	magic.
		Expect("llamas").
		AndWriteToStdout("llamas rock\n").
		AndExitWith(0)

	git := tester.Mock("git", t).PassthroughToLocalCommand()
	expectCheckoutGitCommands(git, tester.Repo.Path)

	preCommandHook := tester.Hook("pre-command", t)
	preCommandHook.
		Expect().
		AndWriteToStdout("ran pre-command hook\n").
		AndExitWith(0)

	postCommandHook := tester.Hook("post-command", t)
	postCommandHook.
		Expect().
		AndWriteToStdout("ran post-command hook\n").
		AndExitWith(0)

	if err = tester.Run(env...); err != nil {
		t.Fatal(err)
	}

	if err := tester.CheckMocksAndClose(t); err != nil {
		t.Fatal(err)
	}
}

func TestCheckingOutPublicGithubProjectWithSSHFingerprintVerification(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	tester, err := NewBootstrapTester(bootstrapPath())
	if err != nil {
		t.Fatal(err)
	}

	repo := `https://github.com/buildkite/bash-example.git`
	env := []string{
		`BUILDKITE_AGENT_NAME=test-agent`,
		`BUILDKITE_PROJECT_SLUG=test-project`,
		`BUILDKITE_REPO=` + repo,
		`BUILDKITE_PULL_REQUEST=`,
		`BUILDKITE_PROJECT_PROVIDER=git`,
		`BUILDKITE_COMMIT=HEAD`,
		`BUILDKITE_BRANCH=master`,
		`BUILDKITE_COMMAND_EVAL=true`,
		`BUILDKITE_COMMAND=magic llamas`,
		`BUILDKITE_JOB_ID=1111-1111-1111-1111`,
		`BUILDKITE_ARTIFACT_PATHS=`,
		`BUILDKITE_REPO_SSH_HOST=github.com`,
		`BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION=true`,
	}

	agent := tester.Mock("buildkite-agent", t)
	expectCheckoutBuildkiteAgentCommands(agent)

	magic := tester.Mock("magic", t)
	magic.
		Expect("llamas").
		AndWriteToStdout("llamas rock\n").
		AndExitWith(0)

	sshkeygen := tester.Mock("ssh-keygen", t)
	sshkeygen.
		Expect("-f", bintest.MatchAny(), "-F", "github.com").
		AndExitWith(0)

	sshkeyscan := tester.Mock("ssh-keyscan", t)
	sshkeyscan.
		Expect("github.com").
		AndExitWith(0)

	git := tester.Mock("git", t).PassthroughToLocalCommand()
	expectCheckoutGitCommands(git, repo)

	if err = tester.Run(env...); err != nil {
		t.Fatal(err)
	}

	tester.AssertOutputContains(t,
		`This is an example of a pre-command hook from .buildkite/hooks/pre-command`)

	tester.AssertOutputContains(t,
		`This is an example of a post-command hook from .buildkite/hooks/post-command`)

	if err := tester.CheckMocksAndClose(t); err != nil {
		t.Fatal(err)
	}
}

func TestCheckingOutPublicGithubProjectWithoutSSHFingerprints(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping test in short mode")
	}

	tester, err := NewBootstrapTester(bootstrapPath())
	if err != nil {
		t.Fatal(err)
	}

	repo := `https://github.com/buildkite/bash-example.git`
	env := []string{
		`BUILDKITE_AGENT_NAME=test-agent`,
		`BUILDKITE_PROJECT_SLUG=test-project`,
		`BUILDKITE_REPO=` + repo,
		`BUILDKITE_PULL_REQUEST=`,
		`BUILDKITE_PROJECT_PROVIDER=git`,
		`BUILDKITE_COMMIT=HEAD`,
		`BUILDKITE_BRANCH=master`,
		`BUILDKITE_COMMAND_EVAL=true`,
		`BUILDKITE_COMMAND=magic llamas`,
		`BUILDKITE_JOB_ID=1111-1111-1111-1111`,
		`BUILDKITE_ARTIFACT_PATHS=`,
		`BUILDKITE_REPO_SSH_HOST=github.com`,
		`BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION=false`,
	}

	agent := tester.Mock("buildkite-agent", t)
	expectCheckoutBuildkiteAgentCommands(agent)

	magic := tester.Mock("magic", t)
	magic.
		Expect("llamas").
		AndWriteToStdout("llamas rock\n").
		AndExitWith(0)

	git := tester.Mock("git", t).PassthroughToLocalCommand()
	expectCheckoutGitCommands(git, repo)

	if err = tester.Run(env...); err != nil {
		t.Fatal(err)
	}

	tester.AssertOutputContains(t, `Skipping auto SSH fingerprint verification`)

	if err := tester.CheckMocksAndClose(t); err != nil {
		t.Fatal(err)
	}
}
