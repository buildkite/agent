// Package version provides the agent version strings.
package version

import (
	_ "embed"
	"runtime"
	"strings"
)

// You can overridden buildVersion at compile time by using:
//
//  go run -ldflags "-X github.com/buildkite/agent/v3/agent.buildVersion=abc" . --version
//
// On CI, the binaries are always build with the buildVersion variable set.
//
// Pre-release builds' versions must be in the format `x.y-beta`, `x.y-beta.z` or `x.y-beta.z.a`

//go:embed VERSION
var baseVersion string
var buildVersion string

func Version() string {
	return strings.TrimSpace(baseVersion)
}

func BuildVersion() string {
	if buildVersion == "" {
		return "x"
	}
	return buildVersion
}

func UserAgent() string {
	return "buildkite-agent/" + Version() + "." + BuildVersion() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}
