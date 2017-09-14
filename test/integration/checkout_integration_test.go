package integration

import (
	"testing"

	"github.com/lox/bintest"
)

func TestCheckingOutLocalGitProject(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}

	// Actually execute git commands
	git := tester.
		MustMock(t, "git").
		PassthroughToLocalCommand()

	// But assert which ones are called
	git.ExpectAll([][]interface{}{
		{"clone", "-v", "--", tester.Repo.Path, "."},
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

	// required by debug mode
	git.Expect("--version").
		AndWriteToStdout(`git version 2.13.3`).
		AndExitWith(0)

	if err = tester.Run(); err != nil {
		t.Fatal(err)
	}

	if err := tester.CheckMocksAndClose(t); err != nil {
		t.Fatal(err)
	}
}

func TestCheckingOutWithSSHFingerprintVerification(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}

	sshkeygen := tester.MustMock(t, "ssh-keygen")
	sshkeygen.
		Expect("-f", bintest.MatchAny(), "-F", "github.com").
		AndExitWith(0)

	sshkeyscan := tester.MustMock(t, "ssh-keyscan")
	sshkeyscan.
		Expect("github.com").
		AndExitWith(0)

	// run actual git
	if err = tester.LinkLocalCommand("git"); err != nil {
		t.Fatal(err)
	}

	env := []string{
		`BUILDKITE_REPO_SSH_HOST=github.com`,
		`BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION=true`,
	}

	if err = tester.Run(env...); err != nil {
		t.Fatal(err)
	}

	if err := tester.CheckMocksAndClose(t); err != nil {
		t.Fatal(err)
	}
}

func TestCheckingOutWithoutSSHFingerprintVerification(t *testing.T) {
	tester, err := NewBootstrapTester()
	if err != nil {
		t.Fatal(err)
	}

	// run actual git
	if err = tester.LinkLocalCommand("git"); err != nil {
		t.Fatal(err)
	}

	env := []string{
		`BUILDKITE_REPO_SSH_HOST=github.com`,
		`BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION=false`,
	}

	if err = tester.Run(env...); err != nil {
		t.Fatal(err)
	}

	tester.AssertOutputContains(t,
		`Skipping auto SSH fingerprint verification`)

	if err := tester.CheckMocksAndClose(t); err != nil {
		t.Fatal(err)
	}
}
