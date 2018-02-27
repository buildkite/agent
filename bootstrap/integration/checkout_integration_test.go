package integration

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lox/bintest"
)

func TestCheckingOutLocalGitProject(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	env := []string{
		"BUILDKITE_GIT_CLONE_FLAGS=-v",
		"BUILDKITE_GIT_CLEAN_FLAGS=-fdq",
	}

	// Actually execute git commands, but with expectations
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	git.ExpectAll([][]interface{}{
		{"rev-parse"},
		{"clone", "-v", "--", tester.Repo.Path, "."},
		{"clean", "-fdq"},
		{"submodule", "foreach", "--recursive", "git", "clean", "-fdq"},
		{"fetch", "-v", "--prune", "origin", "master"},
		{"checkout", "-f", "FETCH_HEAD"},
		{"submodule", "sync", "--recursive"},
		{"submodule", "update", "--init", "--recursive", "--force"},
		{"submodule", "foreach", "--recursive", "git", "reset", "--hard"},
		{"clean", "-fdq"},
		{"submodule", "foreach", "--recursive", "git", "clean", "-fdq"},
		{"submodule", "foreach", "--recursive", "git", "ls-remote", "--get-url"},
		{"--no-pager", "show", "HEAD", "-s", "--format=fuller", "--no-color"},
		{"--no-pager", "branch", "--contains", "HEAD", "--no-color"},
	})

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(1)
	agent.
		Expect("meta-data", "set", "buildkite:git:commit", bintest.MatchAny()).
		AndExitWith(0)
	agent.
		Expect("meta-data", "set", "buildkite:git:branch", bintest.MatchAny()).
		AndExitWith(0)

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutSetsCorrectGitMetadataAndSendsItToBuildkite(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(1)

	agent.
		Expect("meta-data", "set", "buildkite:git:commit",
			bintest.MatchPattern(`^commit`)).
		AndExitWith(0)

	agent.
		Expect("meta-data", "set", "buildkite:git:branch",
			bintest.MatchPattern(`^\* \(HEAD detached at FETCH_HEAD\)`)).
		AndExitWith(0)

	tester.RunAndCheck(t)
}

func TestCheckingOutWithSSHFingerprintVerification(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		AndExitWith(0)

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	git := tester.MustMock(t, "git")
	git.IgnoreUnexpectedInvocations().PassthroughToLocalCommand()

	git.Expect("clone", "-v", "--", "https://github.com/buildkite/bash-example.git", ".").
		AndExitWith(0)

	env := []string{
		`BUILDKITE_REPO=https://github.com/buildkite/bash-example.git`,
		`BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION=true`,
	}

	tester.RunAndCheck(t, env...)
}

func TestCheckingOutWithoutSSHFingerprintVerification(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	tester.MustMock(t, "ssh-keyscan").
		Expect("github.com").
		NotCalled()

	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	env := []string{
		`BUILDKITE_REPO=https://github.com/buildkite/bash-example.git`,
		`BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION=false`,
	}

	tester.RunAndCheck(t, env...)
}

func TestCleaningAnExistingCheckout(t *testing.T) {
	t.Parallel()

	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Create an existing checkout
	out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, tester.CheckoutDir())
	if err != nil {
		t.Fatalf("Clone failed with %s", out)
	}
	err = ioutil.WriteFile(filepath.Join(tester.CheckoutDir(), "test.txt"), []byte("llamas"), 0700)
	if err != nil {
		t.Fatalf("Write failed with %s", out)
	}

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.RunAndCheck(t)

	_, err = os.Stat(filepath.Join(tester.CheckoutDir(), "test.txt"))
	if os.IsExist(err) {
		t.Fatalf("test.txt still exitst")
	}
}

func TestForcingACleanCheckout(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Mock out the meta-data calls to the agent after checkout
	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.RunAndCheck(t, "BUILDKITE_CLEAN_CHECKOUT=true")

	if !strings.Contains(tester.Output, "Cleaning pipeline checkout") {
		t.Fatalf("Should have removed checkout dir")
	}
}

func TestCheckoutOnAnExistingRepositoryWithoutAGitFolder(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()

	// Create an existing checkout
	out, err := tester.Repo.Execute("clone", "-v", "--", tester.Repo.Path, tester.CheckoutDir())
	if err != nil {
		t.Fatalf("Clone failed with %s", out)
	}

	if err = os.RemoveAll(filepath.Join(tester.CheckoutDir(), ".git", "refs")); err != nil {
		t.Fatal(err)
	}

	agent := tester.MustMock(t, "buildkite-agent")
	agent.
		Expect("meta-data", "exists", "buildkite:git:commit").
		AndExitWith(0)

	tester.RunAndCheck(t)
}
