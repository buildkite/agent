package bootstrap

import (
	"os"
	"testing"
)

func TestParsingGittableRepository(t *testing.T) {
	t.Parallel()

	wd, _ := os.Getwd()

	var testCases = []struct {
		Ref      string
		Expected string
		IsErr    bool
	}{
		{wd, wd, false},
		{"git@github.com:buildkite/agent.git", "ssh://git@github.com/buildkite/agent.git", false},
		{"git@github.com-alias1:buildkite/agent.git", "ssh://git@github.com-alias1/buildkite/agent.git", false},
		{"ssh://git@scm.xxx:7999/yyy/zzz.git", "ssh://git@scm.xxx:7999/yyy/zzz.git", false},
		{"ssh://root@git.host.de:4019/var/cache/git/project.git", "ssh://root@git.host.de:4019/var/cache/git/project.git", false},
	}

	for _, tc := range testCases {
		u, err := ParseGittableURL(tc.Ref)
		if err != nil {
			t.Fatal(err)
		}
		actual := u.String()
		if tc.Expected != actual {
			t.Fatalf("Expected %q, got %q", tc.Expected, actual)
		}
	}
}

func TestStrippingGitHostAliases(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		Host     string
		Expected string
	}{
		{"github.com-alias1", "github.com"},
		{"blargh-no-alias.com", "blargh-no-alias.com"},
	}

	for _, tc := range testCases {
		actual := stripAliasesFromGitHost(tc.Host)
		if tc.Expected != actual {
			t.Fatalf("Expected %q, got %q", tc.Expected, actual)
		}
	}
}
