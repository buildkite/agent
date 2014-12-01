package buildbox

import "fmt"

// buildVersion can be overriden at compile time by using:
//
//  go run -ldflags "-X github.com/buildbox/agent/buildbox.buildVersion abc" *.go --version

var baseVersion string = "1.0-beta.6"
var buildVersion string = ""

func Version() string {
	if buildVersion != "" {
		return fmt.Sprintf("%s.%s", baseVersion, buildVersion)
	} else {
		return baseVersion
	}
}
