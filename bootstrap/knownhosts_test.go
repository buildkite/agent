package bootstrap

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
)

func TestAddingToKnownHosts(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		Name       string
		Repository string
		Host       string
	}{
		{"git url", "git@github.com:buildkite/agent.git", "github.com"},
		{"git url with alias", "git@github.com-alias1:buildkite/agent.git", "github.com"},
		{"ssh url with port", "ssh://git@ssh.github.com:443/var/cache/git/project.git", "ssh.github.com:443"},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			sh, err := shell.New()
			if err != nil {
				t.Fatal(err)
			}

			// sh.Debug = true
			// sh.Logger = &shell.TestingLogger{T: t}

			f, err := ioutil.TempFile("", "known-hosts")
			if err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			defer os.RemoveAll(f.Name())

			kh := knownHosts{
				Shell: sh,
				Path:  f.Name(),
			}

			exists, err := kh.Contains(tc.Host)
			if err != nil {
				t.Fatal(err)
			}
			if exists {
				t.Fatalf("Host %q shouldn't exist yet in known_hosts", tc.Host)
			}

			if err := kh.AddFromRepository(tc.Repository); err != nil {
				t.Fatal(err)
			}

			exists, err = kh.Contains(tc.Host)
			if err != nil {
				t.Fatal(err)
			}
			if !exists {
				t.Fatalf("Host %q should exist in known_hosts", tc.Host)
			}
		})
	}
}
