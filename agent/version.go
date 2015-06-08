package agent

import "fmt"
import "strings"

// You can overridden buildVersion at compile time by using:
//
//  go run -ldflags "-X github.com/buildkite/agent/agent.buildVersion abc" *.go --version
//
// On CI, the binaries are always build with the buildVersion variable set.

var baseVersion string = "1.0-beta.37"
var buildVersion string = ""

func Version() string {
	// Only output the build version if a pre-release
	if strings.Contains(baseVersion, "beta") || strings.Contains(baseVersion, "alpha") {
		// Use a default buildVersion if one doesn't exist
		actualBuildVersion := buildVersion
		if actualBuildVersion == "" {
			actualBuildVersion = "x"
		}

		return fmt.Sprintf("%s.%s", baseVersion, actualBuildVersion)
	} else {
		return baseVersion
	}
}
