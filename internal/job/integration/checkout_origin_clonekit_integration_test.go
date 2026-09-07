package integration

import (
	"strings"
	"testing"

	"github.com/buildkite/bintest/v3"
)

func TestOriginCloneKitWithDefaultMirrorFlags(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"acme/repo.git", "git/acme/repo.git"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			if err := tester.EnableGitMirrors(); err != nil {
				t.Fatal(err)
			}
			repository := "https://origin.cursor.com/" + path

			git := tester.MustMock(t, "git")
			git.IgnoreUnexpectedInvocations()
			// Reach the credential boundary without making external requests. A failed
			// lookup must fall back to the canonical mirror clone in the same attempt.
			git.Expect("-c", "credential.useHttpPath=true", "-c", "credential.helper=",
				"-c", bintest.MatchAny(), "-c", "http.extraHeader=", "-c", "protocol.version=2",
				"credential", "fill").
				WithStdin("protocol=https\nhost=origin.cursor.com\npath=" + path + "\n\n").
				AndExitWith(1)
			git.Expect("clone", "--mirror", "-v", "--", repository, bintest.MatchAny()).AndExitWith(0)

			// Deliberately do not override BUILDKITE_GIT_CLONE_MIRROR_FLAGS: exercise
			// the real bootstrap CLI default, rather than a zero-value ExecutorConfig.
			tester.RunAndCheck(t,
				"BUILDKITE_AGENT_EXPERIMENT=origin-clonekit",
				"BUILDKITE_REPO="+repository,
				"BUILDKITE_SSH_KEYSCAN=false",
			)
			if !strings.Contains(tester.Output, "Origin CloneKit: error (credentials)") {
				t.Fatalf("CloneKit was not attempted with default mirror flags:\n%s", tester.Output)
			}
		})
	}
}
