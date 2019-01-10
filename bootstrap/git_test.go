package bootstrap

import (
	"testing"

	"github.com/buildkite/agent/bootstrap/shell"
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

func TestResolvingGitHostAliases(t *testing.T) {
	t.Parallel()

	sh, err := shell.New()
	if err != nil {
		t.Fatal(err)
	}

	sh.Logger = shell.TestingLogger{t}

	assert.Equal(t, "github.com", resolveGitHost(sh, "github.com-alias1"))
	assert.Equal(t, "blargh-no-alias.com", resolveGitHost(sh, "blargh-no-alias.com"))
}
