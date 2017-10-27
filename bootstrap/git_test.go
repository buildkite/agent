package bootstrap

import (
	"testing"
)

func TestParsingGittableRepository(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		Ref    string
		String string
		Host   string
	}{
		// files
		{"/home/vagrant/repo", "file:///home/vagrant/repo", ""},
		{"C:\\Users\\vagrant\\repo", "file:///C:/Users/vagrant/repo", ""},
		{"file:///C:/Users/vagrant/repo", "file:///C:/Users/vagrant/repo", ""},

		// ssh
		{"git@github.com:buildkite/agent.git", "ssh://git@github.com/buildkite/agent.git", "github.com"},
		{"git@github.com-alias1:buildkite/agent.git", "ssh://git@github.com-alias1/buildkite/agent.git", "github.com-alias1"},
		{"ssh://git@scm.xxx:7999/yyy/zzz.git", "ssh://git@scm.xxx:7999/yyy/zzz.git", "scm.xxx:7999"},
		{"ssh://root@git.host.de:4019/var/cache/git/project.git", "ssh://root@git.host.de:4019/var/cache/git/project.git", "git.host.de:4019"},
	}

	for _, tc := range testCases {
		t.Logf("Parsing %s", tc.Ref)
		u, err := ParseGittableURL(tc.Ref)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%#v", u)
		if tc.Host != u.Host {
			t.Fatalf("Expected host %q, got %q", tc.Host, u.Host)
		}
		if tc.String != u.String() {
			t.Fatalf("Expected %q, got %q", tc.String, u.String())
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
