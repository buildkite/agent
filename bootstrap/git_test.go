package bootstrap

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/buildkite/agent/v3/bootstrap/shell"
	"github.com/buildkite/bintest/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestResolvingGitHostAliasesWithFlagSupport(t *testing.T) {
	t.Parallel()

	sh := shell.NewTestShell(t)

	ssh, err := bintest.NewMock("ssh")
	if err != nil {
		t.Fatal(err)
	}
	defer ssh.CheckAndClose(t)

	sh.Env.Set("PATH", filepath.Dir(ssh.Path))

	ssh.
		Expect("-G", "github.com-alias1").
		AndWriteToStdout(`user buildkite
hostname github.com
port 22
addkeystoagent false
addressfamily any
batchmode no
canonicalizefallbacklocal yes
canonicalizehostname false
challengeresponseauthentication yes
checkhostip yes
compression no
controlmaster false
enablesshkeysign no
clearallforwardings no
exitonforwardfailure no
fingerprinthash SHA256
forwardagent no
forwardx11 no
forwardx11trusted no
gatewayports no
gssapiauthentication no
gssapidelegatecredentials no
hashknownhosts no
hostbasedauthentication no
identitiesonly no
kbdinteractiveauthentication yes
nohostauthenticationforlocalhost no
passwordauthentication yes
permitlocalcommand no
proxyusefdpass no
pubkeyauthentication yes
requesttty auto
streamlocalbindunlink no
stricthostkeychecking ask
tcpkeepalive yes
tunnel false
verifyhostkeydns false
visualhostkey no
updatehostkeys false
canonicalizemaxdots 1
connectionattempts 1
forwardx11timeout 1200
numberofpasswordprompts 3
serveralivecountmax 3
serveraliveinterval 0
ciphers chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com
hostkeyalgorithms ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
hostbasedkeytypes ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
kexalgorithms curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1
casignaturealgorithms ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
loglevel INFO
macs umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1
pubkeyacceptedkeytypes ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
xauthlocation xauth
identityfile ~/.ssh/github_rsa
canonicaldomains
globalknownhostsfile /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
userknownhostsfile ~/.ssh/known_hosts ~/.ssh/known_hosts2
sendenv LANG
sendenv LC_*
connecttimeout none
tunneldevice any:any
controlpersist no
escapechar ~
ipqos af21 cs1
rekeylimit 0 0
streamlocalbindmask 0177
syslogfacility USER`).
		AndExitWith(0)

	assert.Equal(t, "github.com", resolveGitHost(sh, "github.com-alias1"))

	ssh.
		Expect("-G", "blargh-no-alias.com").
		AndWriteToStdout(`user buildkite
hostname blargh-no-alias.com
port 22
addkeystoagent false
addressfamily any
batchmode no
canonicalizefallbacklocal yes
canonicalizehostname false
challengeresponseauthentication yes
checkhostip yes
compression no
controlmaster false
enablesshkeysign no
clearallforwardings no
exitonforwardfailure no
fingerprinthash SHA256
forwardagent no
forwardx11 no
forwardx11trusted no
gatewayports no
gssapiauthentication no
gssapidelegatecredentials no
hashknownhosts no
hostbasedauthentication no
identitiesonly no
kbdinteractiveauthentication yes
nohostauthenticationforlocalhost no
passwordauthentication yes
permitlocalcommand no
proxyusefdpass no
pubkeyauthentication yes
requesttty auto
streamlocalbindunlink no
stricthostkeychecking ask
tcpkeepalive yes
tunnel false
verifyhostkeydns false
visualhostkey no
updatehostkeys false
canonicalizemaxdots 1
connectionattempts 1
forwardx11timeout 1200
numberofpasswordprompts 3
serveralivecountmax 3
serveraliveinterval 0
ciphers chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com
hostkeyalgorithms ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
hostbasedkeytypes ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
kexalgorithms curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1
casignaturealgorithms ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
loglevel INFO
macs umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1
pubkeyacceptedkeytypes ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
xauthlocation xauth
identityfile ~/.ssh/github_rsa
canonicaldomains
globalknownhostsfile /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
userknownhostsfile ~/.ssh/known_hosts ~/.ssh/known_hosts2
sendenv LANG
sendenv LC_*
connecttimeout none
tunneldevice any:any
controlpersist no
escapechar ~
ipqos af21 cs1
rekeylimit 0 0
streamlocalbindmask 0177
syslogfacility USER`).
		AndExitWith(0)

	assert.Equal(t, "blargh-no-alias.com", resolveGitHost(sh, "blargh-no-alias.com"))

	ssh.
		Expect("-G", "cool-alias").
		AndWriteToStdout(`user cool-admin
hostname rad-git-host.com
port 443
addkeystoagent false
addressfamily any
batchmode no
canonicalizefallbacklocal yes
canonicalizehostname false
challengeresponseauthentication yes
checkhostip yes
compression no
controlmaster false
enablesshkeysign no
clearallforwardings no
exitonforwardfailure no
fingerprinthash SHA256
forwardagent no
forwardx11 no
forwardx11trusted no
gatewayports no
gssapiauthentication no
gssapidelegatecredentials no
hashknownhosts no
hostbasedauthentication no
identitiesonly no
kbdinteractiveauthentication yes
nohostauthenticationforlocalhost no
passwordauthentication yes
permitlocalcommand no
proxyusefdpass no
pubkeyauthentication yes
requesttty auto
streamlocalbindunlink no
stricthostkeychecking ask
tcpkeepalive yes
tunnel false
verifyhostkeydns false
visualhostkey no
updatehostkeys false
canonicalizemaxdots 1
connectionattempts 1
forwardx11timeout 1200
numberofpasswordprompts 3
serveralivecountmax 3
serveraliveinterval 0
ciphers chacha20-poly1305@openssh.com,aes128-ctr,aes192-ctr,aes256-ctr,aes128-gcm@openssh.com,aes256-gcm@openssh.com
hostkeyalgorithms ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
hostbasedkeytypes ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
kexalgorithms curve25519-sha256,curve25519-sha256@libssh.org,ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group-exchange-sha256,diffie-hellman-group16-sha512,diffie-hellman-group18-sha512,diffie-hellman-group14-sha256,diffie-hellman-group14-sha1
casignaturealgorithms ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
loglevel INFO
macs umac-64-etm@openssh.com,umac-128-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512-etm@openssh.com,hmac-sha1-etm@openssh.com,umac-64@openssh.com,umac-128@openssh.com,hmac-sha2-256,hmac-sha2-512,hmac-sha1
pubkeyacceptedkeytypes ecdsa-sha2-nistp256-cert-v01@openssh.com,ecdsa-sha2-nistp384-cert-v01@openssh.com,ecdsa-sha2-nistp521-cert-v01@openssh.com,ssh-ed25519-cert-v01@openssh.com,rsa-sha2-512-cert-v01@openssh.com,rsa-sha2-256-cert-v01@openssh.com,ssh-rsa-cert-v01@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ssh-rsa
xauthlocation xauth
identityfile ~/.ssh/github_rsa
canonicaldomains
globalknownhostsfile /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2
userknownhostsfile ~/.ssh/known_hosts ~/.ssh/known_hosts2
sendenv LANG
sendenv LC_*
connecttimeout none
tunneldevice any:any
controlpersist no
escapechar ~
ipqos af21 cs1
rekeylimit 0 0
streamlocalbindmask 0177
syslogfacility USER`).
		AndExitWith(0)

	assert.Equal(t, "rad-git-host.com:443", resolveGitHost(sh, "cool-alias"))
}

func TestResolvingGitHostAliasesWithoutFlagSupport(t *testing.T) {
	t.Parallel()

	sh := shell.NewTestShell(t)

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

func TestGitCheckRefFormat(t *testing.T) {
	for ref, expect := range map[string]bool{
		"hello":          true,
		"hello-world":    true,
		"hello/world":    true,
		"--option":       false,
		" leadingspace":  false,
		"has space":      false,
		"has~tilde":      false,
		"has^caret":      false,
		"has:colon":      false,
		"has\007control": false,
		"has\177del":     false,
		"endswithdot.":   false,
		"two..dots":      false,
		"@":              false,
		"back\\slash":    false,
	} {
		t.Run(ref, func(t *testing.T) {
			if gitCheckRefFormat(ref) != expect {
				t.Errorf("gitCheckRefFormat(%q) should be %v", ref, expect)
			}
		})
	}
}

func TestGitCheckoutValidatesRef(t *testing.T) {
	sh := mockRunner()
	defer sh.Check(t)
	err := gitCheckout(&shell.Shell{}, "", "--nope")
	assert.EqualError(t, err, `"--nope" is not a valid git ref format`)
}

func TestGitCheckout(t *testing.T) {
	sh := mockRunner().Expect("git", "checkout", "-f", "-q", "main")
	defer sh.Check(t)
	err := gitCheckout(sh, "-f -q", "main")
	require.NoError(t, err)
}

func TestGitCheckoutSketchyArgs(t *testing.T) {
	sh := mockRunner()
	defer sh.Check(t)
	err := gitCheckout(sh, "-f -q", "  --hello")
	assert.EqualError(t, err, `"  --hello" is not a valid git ref format`)
}

func TestGitClone(t *testing.T) {
	sh := mockRunner().Expect("git", "clone", "-v", "--references", "url", "--", "repo", "dir")
	defer sh.Check(t)
	err := gitClone(sh, "-v --references url", "repo", "dir")
	require.NoError(t, err)
}

func TestGitClean(t *testing.T) {
	sh := mockRunner().Expect("git", "clean", "--foo", "--bar")
	defer sh.Check(t)
	err := gitClean(sh, "--foo --bar")
	require.NoError(t, err)
}

func TestGitCleanSubmodules(t *testing.T) {
	sh := mockRunner().Expect("git", "submodule", "foreach", "--recursive", "git clean --foo --bar")
	defer sh.Check(t)
	err := gitCleanSubmodules(sh, "--foo --bar")
	require.NoError(t, err)
}

func TestGitFetch(t *testing.T) {
	sh := mockRunner().Expect("git", "fetch", "--foo", "--bar", "--", "repo", "ref1", "ref2")
	defer sh.Check(t)
	err := gitFetch(sh, "--foo --bar", "repo", "ref1", "ref2")
	require.NoError(t, err)
}

func mockRunner() *mockShellRunner {
	return &mockShellRunner{}
}

// mockShellRunner implements shellRunner for testing expected calls.
type mockShellRunner struct {
	expect [][]string
	calls  [][]string
}

func (r *mockShellRunner) Expect(cmd string, args ...string) *mockShellRunner {
	r.expect = append(r.expect, append([]string{cmd}, args...))
	return r
}

func (r *mockShellRunner) Run(cmd string, args ...string) error {
	r.calls = append(r.calls, append([]string{cmd}, args...))
	return nil
}

func (r *mockShellRunner) Check(t *testing.T) {
	if !reflect.DeepEqual(r.calls, r.expect) {
		t.Errorf("\nexpected: %q\n     got: %q\n", r.expect, r.calls)
	}
}
