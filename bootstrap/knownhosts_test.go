package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/bintest/v3"
)

func TestAddingToKnownHosts(t *testing.T) {
	t.Parallel()

	var testCases = []struct {
		Name       string
		Repository string
		Alias      string
		Host       string
	}{
		{"git url", "git@github.com:buildkite/agent.git", "github.com", "github.com"},
		{"git url with alias", "git@github.com-alias1:buildkite/agent.git", "github.com-alias1", "github.com"},
		{"ssh url with port", "ssh://git@ssh.github.com:443/var/cache/git/project.git", "ssh.github.com:443", "ssh.github.com:443"},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			sh := shell.NewTestShell(t)

			ssh, err := bintest.NewMock("ssh")
			if err != nil {
				t.Fatalf("bintest.NewMock(ssh) error = %v", err)
			}
			defer ssh.CheckAndClose(t)

			path := fmt.Sprintf("%s%c%s", filepath.Dir(ssh.Path), os.PathListSeparator, os.Getenv("PATH"))

			sh.Env.Set("PATH", path)

			ssh.
				Expect("-G", tc.Alias).
				AndWriteToStderr(`unknown option -- G
usage: ssh [-1246AaCfgKkMNnqsTtVvXxYy] [-b bind_address] [-c cipher_spec]
           [-D [bind_address:]port] [-E log_file] [-e escape_char]
           [-F configfile] [-I pkcs11] [-i identity_file]
           [-L [bind_address:]port:host:hostport] [-l login_name] [-m mac_spec]
           [-O ctl_cmd] [-o option] [-p port]
           [-Q cipher | cipher-auth | mac | kex | key]
           [-R [bind_address:]port:host:hostport] [-S ctl_path] [-W host:port]
           [-w local_tun[:remote_tun]] [user@]hostname [command]`).
				AndExitWith(255)

			f, err := os.CreateTemp("", "known-hosts")
			if err != nil {
				t.Fatalf(`os.CreateTemp("", "known-hosts") error = %v`, err)
			}
			defer os.RemoveAll(f.Name())
			if err := f.Close(); err != nil {
				t.Fatalf("f.Close() = %v", err)
			}

			kh := knownHosts{
				Shell: sh,
				Path:  f.Name(),
			}

			exists, err := kh.Contains(tc.Host)
			if err != nil {
				t.Errorf("kh.Contains(%q) error = %v", tc.Host, err)
			}
			if got, want := exists, false; got != want {
				t.Errorf("kh.Contains(%q) = %t, want %t", tc.Host, got, want)
			}

			if err := kh.AddFromRepository(tc.Repository); err != nil {
				t.Errorf("kh.AddFromRespository(%q) = %v", tc.Repository, err)
			}

			exists, err = kh.Contains(tc.Host)
			if err != nil {
				t.Errorf("kh.Contains(%q) error = %v", tc.Host, err)
			}
			if got, want := exists, true; got != want {
				t.Errorf("kh.Contains(%q) = %t, want %t", tc.Host, got, want)
			}
		})
	}
}
