package job

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

var sshVersionRE = regexp.MustCompile(`OpenSSH(?:[_\s]for[_\s]Windows)?[_\s](\d+)\.(\d+)`)

var (
	errNoPatternMatch = errors.New("version string did not match pattern")
	errMajorNotInt    = errors.New("major version component not an integer")
	errMinorNotInt    = errors.New("minor version component not an integer")
)

// sshSupportsAcceptNew checks if the installed SSH version supports
// StrictHostKeyChecking=accept-new (requires OpenSSH 7.6+, released Oct 2017).
func sshSupportsAcceptNew() (bool, error) {
	major, minor, err := sshVersion()
	if err != nil {
		return false, err
	}
	return major > 7 || (major == 7 && minor >= 6), nil
}

// sshVersion runs "ssh -V" and parses the version number.
// Returns (major, minor, true) on success, (0, 0, false) on failure.
func sshVersion() (major, minor int, err error) {
	output, err := exec.Command("ssh", "-V").CombinedOutput()
	if err != nil {
		return 0, 0, err
	}
	return parseSSHVersion(string(output))
}

// parseSSHVersion parses an SSH version string like "OpenSSH_8.9p1" or "OpenSSH_7.6p1".
// Returns (major, minor, true) on success.
func parseSSHVersion(output string) (major, minor int, err error) {
	matches := sshVersionRE.FindStringSubmatch(output)
	if len(matches) != 3 {
		return 0, 0, fmt.Errorf("%w [%q !~ %q]", errNoPatternMatch, output, sshVersionRE)
	}

	major, err = strconv.Atoi(matches[1])
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", errMajorNotInt, err)
	}

	minor, err = strconv.Atoi(matches[2])
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", errMinorNotInt, err)
	}

	return major, minor, nil
}
