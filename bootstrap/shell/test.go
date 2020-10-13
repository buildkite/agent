package shell

import (
	"io/ioutil"
	"os"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/env"
)

// NewTestShell creates a minimal shell suitable for tests.
func NewTestShell(t *testing.T) *Shell {
	sh, err := New()
	if err != nil {
		t.Fatal(err)
	}

	sh.Logger = DiscardLogger
	sh.Writer = ioutil.Discard

	if os.Getenv(`DEBUG_SHELL`) == "1" {
		sh.Logger = TestingLogger{T: t}
	}

	// Windows requires certain env variables to be present
	if runtime.GOOS == "windows" {
		sh.Env = env.FromSlice([]string{
			//			"PATH=" + os.Getenv("PATH"),
			"SystemRoot=" + os.Getenv("SystemRoot"),
			"WINDIR=" + os.Getenv("WINDIR"),
			"COMSPEC=" + os.Getenv("COMSPEC"),
			"PATHEXT=" + os.Getenv("PATHEXT"),
			"TMP=" + os.Getenv("TMP"),
			"TEMP=" + os.Getenv("TEMP"),
			"ProgramData=" + os.Getenv("ProgramData"),
		})
	} else {
		sh.Env = env.New()
	}

	return sh
}
