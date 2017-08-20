package bootstrap

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/bootstrap/shell"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/nightlyone/lockfile"
)

type knownHosts struct {
	*lockfile.Lockfile
	shell *shell.Shell
	Path  string
}

func findKnownHosts(shell *shell.Shell) (*knownHosts, error) {
	userHomePath, err := homedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("Could not find the current users home directory (%s)", err)
	}

	// Construct paths to the known_hosts file
	sshDirectory := filepath.Join(userHomePath, ".ssh")
	knownHostPath := filepath.Join(sshDirectory, "known_hosts")

	// Ensure a directory exists and known_host file exist
	if !fileExists(knownHostPath) {
		os.MkdirAll(sshDirectory, 0755)
		ioutil.WriteFile(knownHostPath, []byte(""), 0644)
	}

	lockFile := filepath.Join(sshDirectory, "known_hosts.lock")

	// Create a lock on the known_host file so other agents don't try and
	// change it at the same time
	knownHostLock, err := shell.LockFileWithTimeout(lockFile, time.Second*30)
	if err != nil {
		return nil, fmt.Errorf("Could not acquire a lock on `%s`: %v", lockFile, err)
	}

	return &knownHosts{knownHostLock, shell, knownHostPath}, nil
}

func (kh *knownHosts) Add(host string) error {
	// Try and open the existing hostfile in (append_only) mode
	f, err := os.OpenFile(kh.Path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("Could not open \"%s\" for reading (%v)", kh.Path, err)
	}
	defer f.Close()

	sshToolsDir, err := findSSHToolsDir(kh.shell)
	if err != nil {
		return err
	}

	// Grab the generated keys for the repo host
	keygenOutput, err := kh.shell.RunCommandSilentlyAndCaptureOutput(filepath.Join(sshToolsDir, "ssh-keygen"), "-f", kh.Path, "-F", host)
	if err != nil {
		return fmt.Errorf("Could not perform `ssh-keygen` (%s)", err)
	}

	// If the keygen output already contains the host, we can skip!
	if strings.Contains(keygenOutput, host) {
		kh.shell.Commentf("Host \"%s\" already in list of known hosts at \"%s\"", host, kh.Path)
		return nil
	}

	// Scan the key and then write it to the known_host file
	keyscanOutput, err := kh.shell.RunCommandSilentlyAndCaptureOutput(filepath.Join(sshToolsDir, "ssh-keyscan"), host)
	if err != nil {
		return fmt.Errorf("Could not perform `ssh-keyscan` (%s)", err)
	}

	if _, err = fmt.Fprintf(f, "%s\n", keyscanOutput); err != nil {
		return fmt.Errorf("Could not write to \"%s\" (%s)", kh.Path, err)
	}

	kh.shell.Commentf("Added \"%s\" to the list of known hosts at \"%s\"", host, kh.Path)
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

// AddFromRepository takes a git repo url, extracts the host and adds it
func (kh *knownHosts) AddFromRepository(repository string) error {
	// Try and parse the repository URL
	url, err := newGittableURL(repository)
	if err != nil {
		kh.shell.Warningf("Could not parse \"%s\" as a URL - skipping adding host to SSH known_hosts", repository)
		return err
	}

	// Clean up the SSH host and remove any key identifiers. See:
	// git@github.com-custom-identifier:foo/bar.git
	// https://buildkite.com/docs/agent/ssh-keys#creating-multiple-ssh-keys
	var repoSSHKeySwitcherRegex = regexp.MustCompile(`-[a-z0-9\-]+$`)
	host := repoSSHKeySwitcherRegex.ReplaceAllString(url.Host, "")

	if err = kh.Add(host); err != nil {
		return fmt.Errorf("Failed to add `%s` to known_hosts file `%s`: %v'", host, url, err)
	}

	return nil
}

func findSSHToolsDir(sh *shell.Shell) (string, error) {
	// On Windows, ssh-keygen isn't on the $PATH by default, but we know we can find it
	// relative to where git for windows is installed, so try that
	if runtime.GOOS == "windows" {
		gitExecPathOutput, _ := sh.RunCommandSilentlyAndCaptureOutput("git", "--exec-path")
		if len(gitExecPathOutput) > 0 {
			sshToolRelativePaths := [][]string{}
			sshToolRelativePaths = append(sshToolRelativePaths, []string{"..", "..", "..", "usr", "bin"})
			sshToolRelativePaths = append(sshToolRelativePaths, []string{"..", "..", "bin"})

			for _, segments := range sshToolRelativePaths {
				segments = append([]string{gitExecPathOutput}, segments...)
				dir := filepath.Join(segments...)
				if _, err := os.Stat(filepath.Join(dir, "ssh-keygen.exe")); err == nil {
					return dir, nil
				}
			}
		}
	}

	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		return "", fmt.Errorf("Failed to find path for ssh-keygen: %v", err)
	}

	return filepath.Dir(keygen), nil
}
