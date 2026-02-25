package job

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/shell"
	"golang.org/x/crypto/ssh/knownhosts"
)

type knownHosts struct {
	Shell *shell.Shell
	Path  string
}

func findKnownHosts(sh *shell.Shell) (*knownHosts, error) {
	userHomePath, err := osutil.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not find the current users home directory (%s)", err)
	}

	// Construct paths to the known_hosts file
	sshDirectory := filepath.Join(userHomePath, ".ssh")
	knownHostPath := filepath.Join(sshDirectory, "known_hosts")

	// Ensure ssh directory exists
	if err := os.MkdirAll(sshDirectory, 0o700); err != nil {
		return nil, err
	}

	// Ensure file exists
	if _, err := os.Stat(knownHostPath); err != nil {
		f, err := os.OpenFile(knownHostPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create %q: %w", knownHostPath, err)
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
	}

	return &knownHosts{Shell: sh, Path: knownHostPath}, nil
}

func (kh *knownHosts) Contains(host string) (bool, error) {
	file, err := os.Open(kh.Path)
	if err != nil {
		return false, err
	}
	defer file.Close() //nolint:errcheck // read-only file; close error is inconsequential

	normalized := knownhosts.Normalize(host)

	// There don't appear to be any libraries to parse known_hosts that don't also want to
	// validate the IP's and host keys. Shelling out to ssh-keygen doesn't support custom ports
	// so I guess we'll do it ourselves.
	//
	// known_host format is defined at https://man.openbsd.org/sshd#SSH_KNOWN_HOSTS_FILE_FORMAT
	// A basic example is:
	// # Comments allowed at start of line
	// closenet,...,192.0.2.53 1024 37 159...93 closenet.example.net
	// cvs.example.net,192.0.2.10 ssh-rsa AAAA1234.....=
	// # A hashed hostname
	// |1|JfKTdBh7rNbXkVAQCRp4OQoPfmI=|USECr3SWf1JUPsms5AqfD5QfxkM= ssh-rsa
	// AAAA1234.....=
	// # A revoked key
	// @revoked * ssh-rsa AAAAB5W...
	// # A CA key, accepted for any host in *.mydomain.com or *.mydomain.org
	// @cert-authority *.mydomain.org,*.mydomain.com ssh-rsa AAAAB5W...
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), " ")
		if len(fields) != 3 {
			continue
		}
		for addr := range strings.SplitSeq(fields[0], ",") {
			if addr == normalized || addr == knownhosts.HashHostname(normalized) {
				return true, nil
			}
		}
	}

	return false, nil
}

func (kh *knownHosts) Add(ctx context.Context, host string) error {
	// Use a lockfile to prevent parallel processes stepping on each other
	lockCtx, canc := context.WithTimeout(ctx, 30*time.Second)
	defer canc()
	lock, err := kh.Shell.LockFile(lockCtx, kh.Path+".lock")
	if err != nil {
		return err
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			kh.Shell.Warningf("Failed to release known_hosts file lock: %#v", err)
		}
	}()

	// If the keygen output already contains the host, we can skip!
	if contains, _ := kh.Contains(host); contains {
		kh.Shell.Commentf("Host %q already in list of known hosts at \"%s\"", host, kh.Path)
		return nil
	}

	// Scan the key and then write it to the known_host file
	keyscanOutput, err := sshKeyScan(ctx, kh.Shell, host)
	if err != nil {
		return fmt.Errorf("could not `ssh-keyscan`: %w", err)
	}

	kh.Shell.Commentf("Added host %q to known hosts at \"%s\"", host, kh.Path)

	// Try and open the existing hostfile in (append_only) mode
	f, err := os.OpenFile(kh.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o700)
	if err != nil {
		return fmt.Errorf("could not open %q for appending: %w", kh.Path, err)
	}
	defer f.Close() //nolint:errcheck // Best-effort cleanup - primary Close error is checked below.

	if _, err := fmt.Fprintf(f, "%s\n", keyscanOutput); err != nil {
		return fmt.Errorf("could not write to %q: %w", kh.Path, err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("could not close %q: %w", kh.Path, err)
	}
	return nil
}

// AddFromRepository takes a git repo url, extracts the host and adds it
func (kh *knownHosts) AddFromRepository(ctx context.Context, repository string) error {
	u, err := parseGittableURL(repository)
	if err != nil {
		kh.Shell.Warningf("Could not parse %q as a URL - skipping adding host to SSH known_hosts", repository)
		return err
	}

	// We only need to keyscan ssh repository urls
	if u.Scheme != "ssh" {
		return nil
	}

	host := resolveGitHost(ctx, kh.Shell, u.Host)

	if err := kh.Add(ctx, host); err != nil {
		return fmt.Errorf("failed to add %q to known_hosts file %q: %w", host, u, err)
	}

	return nil
}
