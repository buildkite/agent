package agent

import "fmt"

// You can overridden buildVersion at compile time by using:
//
//  go run -ldflags "-X github.com/buildkite/agent/agent.buildVersion abc" *.go --version
//
// On CI, the binaries are always build with the buildVersion variable set.

var baseVersion string = "2.0.3"
var buildVersion string = ""

func Version() string {
	// Use a default buildVersion if one doesn't exist
	actualBuildVersion := buildVersion
	if actualBuildVersion == "" {
		actualBuildVersion = "x"
	}

	return fmt.Sprintf("%s.%s", baseVersion, actualBuildVersion)
}
