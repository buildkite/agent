package bootstrap

import (
	"path/filepath"
	"testing"

	"github.com/buildkite/bintest"
	"github.com/stretchr/testify/assert"
)

func TestParsingGittableRepositoryFromFilesPaths(t *testing.T) {
	t.Parallel()

	u, err := parseGittableURL(`/home/vagrant/repo`)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, `file:///home/vagrant/repo`, u.String())
	assert.Empty(t, u.Host)

	u, err = parseGittableURL(`file:///C:/Users/vagrant/repo`)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, `file:///C:/Users/vagrant/repo`, u.String())
	assert.Empty(t, u.Host)
}

func TestParsingGittableRepositoryFromGitURLs(t *testing.T) {
	t.Parallel()

	u, err := parseGittableURL(`git@github.com:buildkite/agent.git`)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, `ssh://git@github.com/buildkite/agent.git`, u.String())
	assert.Equal(t, `github.com`, u.Host)
}

func TestParsingGittableRepositoryFromGitURLsWithAliases(t *testing.T) {
	t.Parallel()

	u, err := parseGittableURL(`git@github.com-alias1:buildkite/agent.git`)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, `ssh://git@github.com-alias1/buildkite/agent.git`, u.String())
	assert.Equal(t, `github.com-alias1`, u.Host)
}

func TestParsingGittableRepositoryFromSSHURLsWithPorts(t *testing.T) {
	t.Parallel()

	u, err := parseGittableURL(`ssh://git@scm.xxx:7999/yyy/zzz.git`)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, `ssh://git@scm.xxx:7999/yyy/zzz.git`, u.String())
	assert.Equal(t, `scm.xxx:7999`, u.Host)

	u, err = parseGittableURL(`ssh://root@git.host.de:4019/var/cache/git/project.git`)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, `ssh://root@git.host.de:4019/var/cache/git/project.git`, u.String())
	assert.Equal(t, `git.host.de:4019`, u.Host)
}

func TestResolvingGitHostAliasesWithoutFlagSupport(t *testing.T) {
	t.Parallel()

	sh := newTestShell(t)

	ssh, err := bintest.NewMock("ssh")
	if err != nil {
		t.Fatal(err)
	}
	defer ssh.CheckAndClose(t)

	sh.Env.Set("PATH", filepath.Dir(ssh.Path))

	ssh.
		Expect("-G", "github.com-alias1").
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

	assert.Equal(t, "github.com", resolveGitHost(sh, "github.com-alias1"))

	ssh.
		Expect("-G", "blargh-no-alias.com").
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

	assert.Equal(t, "blargh-no-alias.com", resolveGitHost(sh, "blargh-no-alias.com"))
}
