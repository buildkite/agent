package job

import (
	"os/exec"
	"regexp"
	"strconv"
)

var sshVersionRE = regexp.MustCompile(`OpenSSH(?:[_\s]for[_\s]Windows)?[_\s](\d+)\.(\d+)`)

// sshSupportsAcceptNew checks if the installed SSH version supports
// StrictHostKeyChecking=accept-new (requires OpenSSH 7.6+, released Oct 2017).
func sshSupportsAcceptNew() bool {
	major, minor, ok := sshVersion()
	if !ok {
		return false
	}
	return major > 7 || (major == 7 && minor >= 6)
}

// sshVersion runs "ssh -V" and parses the version number.
// Returns (major, minor, true) on success, (0, 0, false) on failure.
func sshVersion() (major, minor int, ok bool) {
	output, err := exec.Command("ssh", "-V").CombinedOutput()
	if err != nil {
		return 0, 0, false
	}
	return parseSSHVersion(string(output))
}

// parseSSHVersion parses an SSH version string like "OpenSSH_8.9p1" or "OpenSSH_7.6p1".
// Returns (major, minor, true) on success.
func parseSSHVersion(output string) (major, minor int, ok bool) {
	matches := sshVersionRE.FindStringSubmatch(output)
	if len(matches) < 3 {
		return 0, 0, false
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, 0, false
	}

	minor, err = strconv.Atoi(matches[2])
	if err != nil {
		return 0, 0, false
	}

	return major, minor, true
}
