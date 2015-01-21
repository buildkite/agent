package buildkite

import "fmt"

// buildVersion can be overriden at compile time by using:
//
//  go run -ldflags "-X github.com/buildkite/agent/buildkite.buildVersion abc" *.go --version

var baseVersion string = "1.0-beta.7"
var buildVersion string = ""

func Version() string {
	if buildVersion != "" {
		return fmt.Sprintf("%s.%s", baseVersion, buildVersion)
	} else {
		return baseVersion
	}
}
