package job

import (
	"context"
	"fmt"
	"strings"

	"github.com/buildkite/agent/v4/internal/shell"
)

// gitVer is the major.minor version of a git binary. Patch levels are dropped:
// no feature the agent gates on has ever landed in a patch release.
type gitVer struct {
	major, minor int
}

func (v gitVer) String() string { return fmt.Sprintf("%d.%d", v.major, v.minor) }

// atLeast reports whether v is at least min.
func (v gitVer) atLeast(min gitVer) bool {
	if v.major != min.major {
		return v.major > min.major
	}
	return v.minor >= min.minor
}

// gitVersion returns the version of the local git binary.
//
// Prefer probing for a capability, or attempting the command and degrading on
// failure, over gating on a version: that's what the rest of the agent does
// (see the `git lfs version` probe in checkout, and resolveGitHost's `ssh -G`
// fallback). Version gating is for the cases where git accepts an unsupported
// option as data instead of failing, so there's nothing to probe.
func gitVersion(ctx context.Context, sh *shell.Shell) (gitVer, error) {
	output, err := sh.Command("git", "--version").RunAndCaptureStdout(ctx)
	if err != nil {
		return gitVer{}, err
	}

	v, ok := parseGitVersion(strings.TrimSpace(output))
	if !ok {
		return gitVer{}, fmt.Errorf("parsing git version from %q", strings.TrimSpace(output))
	}
	return v, nil
}

// parseGitVersion extracts the major and minor version from `git --version`
// output, tolerating vendor suffixes such as "git version 2.26.0.windows.1".
func parseGitVersion(output string) (gitVer, bool) {
	var v gitVer
	if _, err := fmt.Sscanf(output, "git version %d.%d", &v.major, &v.minor); err != nil {
		return gitVer{}, false
	}
	return v, true
}
