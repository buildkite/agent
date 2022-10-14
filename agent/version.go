package agent

import "runtime"

// You can overridden buildVersion at compile time by using:
//
//  go run -ldflags "-X github.com/buildkite/agent/v3/agent.buildVersion abc" *.go --version
//
// On CI, the binaries are always build with the buildVersion variable set.
//
// Pre-release builds' versions must be in the format `x.y-beta`, `x.y-beta.z` or `x.y-beta.z.a`

var baseVersion string = "3.39.1"

// This comment is needed to prevent formatters from combining this `var` with the one above
// a step in the pipeline parses this file (as text) for lines of the form `var baseVersion string = `
// See .builkite/steps/extract-base-version-metadata.sh:4
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
