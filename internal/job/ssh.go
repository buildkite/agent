package job

import (
	"os/exec"
	"regexp"
	"strconv"
)

// sshSupportsAcceptNew checks if the installed SSH version supports
// StrictHostKeyChecking=accept-new (requires OpenSSH 7.6+, released Oct 2017).
func sshSupportsAcceptNew() bool {
	major, minor, ok := parseSSHVersion()
	if !ok {
		return false
	}
	return major > 7 || (major == 7 && minor >= 6)
}

// parseSSHVersion runs "ssh -V" and parses the version number.
// Returns (major, minor, true) on success, (0, 0, false) on failure.
func parseSSHVersion() (major, minor int, ok bool) {
	cmd := exec.Command("ssh", "-V")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, false
	}
	return extractSSHVersion(string(output))
}

// extractSSHVersion parses an SSH version string like "OpenSSH_8.9p1" or "OpenSSH_7.6p1".
// Returns (major, minor, true) on success.
func extractSSHVersion(output string) (major, minor int, ok bool) {
	re := regexp.MustCompile(`OpenSSH[_\s](\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(output)
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
