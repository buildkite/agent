package version

import (
	"regexp"
	"testing"
)

func TestVersionIsValid(t *testing.T) {
	// Making sure the version string matches `1.2.3`, `1.2.3-beta`, `1.2.3-beta.1` or `1.2-beta.1`
	if got, want := Version(), regexp.MustCompile(`\A(?:\d+\.){1,2}\d+(?:-[a-zA-Z]+(?:\.\d+)?)?\z`); !want.MatchString(got) {
		t.Errorf("Version() = %q, want string matching %q", got, want)
	}
}
