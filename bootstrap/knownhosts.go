package bootstrap

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/buildkite/agent/bootstrap/shell"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
)

type knownHosts struct {
	Shell *shell.Shell
	Path  string
}

func findKnownHosts(sh *shell.Shell) (*knownHosts, error) {
	userHomePath, err := homedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("Could not find the current users home directory (%s)", err)
	}

	// Construct paths to the known_hosts file
	sshDirectory := filepath.Join(userHomePath, ".ssh")
	knownHostPath := filepath.Join(sshDirectory, "known_hosts")

	// Ensure ssh directory exists
	if err := os.MkdirAll(sshDirectory, 0700); err != nil {
		return nil, err
	}

	// Ensure file exists
	if _, err := os.Stat(knownHostPath); err != nil {
		f, err := os.OpenFile(knownHostPath, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, errors.Wrapf(err, "Could not create %q", knownHostPath)
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
	}

	return &knownHosts{Shell: sh, Path: knownHostPath}, nil
}

func (kh *knownHosts) Contains(host string) (bool, error) {
	// Grab the generated keys for the repo host
	keygenOutput, err := sshKeygen(kh.Shell, kh.Path, host)

	// Returns an error and no output if host isn't in there
	if err != nil && keygenOutput == "" {
		return false, nil
	} else if err != nil {
		return false, err
	}

	return strings.Contains(keygenOutput, host), nil
}

func (kh *knownHosts) Add(host string) error {
	// If the keygen output already contains the host, we can skip!
	if contains, _ := kh.Contains(host); contains {
		kh.Shell.Commentf("Host \"%s\" already in list of known hosts at \"%s\"", host, kh.Path)
		return nil
	}

	// Scan the key and then write it to the known_host file
	keyscanOutput, err := sshKeyScan(kh.Shell, host)
	if err != nil {
		return errors.Wrap(err, "Could not perform `ssh-keyscan`")
	}

	// Try and open the existing hostfile in (append_only) mode
	f, err := os.OpenFile(kh.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0700)
	if err != nil {
		return errors.Wrapf(err, "Could not open %q for appending", kh.Path)
	}
	defer f.Close()

	if _, err = fmt.Fprintf(f, "%s\n", keyscanOutput); err != nil {
		return errors.Wrapf(err, "Could not write to %q", kh.Path)
	}

	return nil
}

var hasSchemePattern = regexp.MustCompile("^[^:]+://")
var scpLikeURLPattern = regexp.MustCompile("^([^@]+@)?([^:]+):/?(.+)$")

func newGittableURL(ref string) (*url.URL, error) {
	if !hasSchemePattern.MatchString(ref) && scpLikeURLPattern.MatchString(ref) {
		matched := scpLikeURLPattern.FindStringSubmatch(ref)
		user := matched[1]
		host := matched[2]
		path := matched[3]

		ref = fmt.Sprintf("ssh://%s%s/%s", user, host, path)
	}

	return url.Parse(ref)
}

// Clean up the SSH host and remove any key identifiers. See:
// git@github.com-custom-identifier:foo/bar.git
// https://buildkite.com/docs/agent/ssh-keys#creating-multiple-ssh-keys
var gitHostAliasRegexp = regexp.MustCompile(`-[a-z0-9\-]+$`)
func stripAliasesFromGitHost(host string) (string) {
	return gitHostAliasRegexp.ReplaceAllString(host, "")
}

// AddFromRepository takes a git repo url, extracts the host and adds it
func (kh *knownHosts) AddFromRepository(repository string) error {
	// Try and parse the repository URL
	url, err := newGittableURL(repository)
	if err != nil {
		kh.Shell.Warningf("Could not parse \"%s\" as a URL - skipping adding host to SSH known_hosts", repository)
		return err
	}

	host := stripAliasesFromGitHost(url.Hostname())

	if err = kh.Add(host); err != nil {
		return fmt.Errorf("Failed to add `%s` to known_hosts file `%s`: %v'", host, url, err)
	}

	return nil
}
