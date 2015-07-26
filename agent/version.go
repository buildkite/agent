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
	if buildVersion != "" {
		return fmt.Sprintf("%s.%s", baseVersion, buildVersion)
	} else {
		return baseVersion
	}
}
