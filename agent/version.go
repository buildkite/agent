package agent

import "runtime"

// You can overridden buildVersion at compile time by using:
//
//  go run -ldflags "-X github.com/buildkite/agent/v3/agent.buildVersion abc" *.go --version
//
// On CI, the binaries are always build with the buildVersion variable set.
//
// Pre-release builds' versions must be in the format `x.y-beta`, `x.y-beta.z` or `x.y-beta.z.a`

var baseVersion string = "3.32.3"
var buildVersion string = ""

func Version() string {
	return baseVersion
}

func BuildVersion() string {
	if buildVersion != "" {
		return buildVersion
	} else {
		return "x"
	}
}

func UserAgent() string {
	return "buildkite-agent/" + Version() + "." + BuildVersion() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}
