package shell

import (
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/env"
)

// NewTestShell creates a minimal shell suitable for tests.
func NewTestShell(t *testing.T) *Shell {
	sh, err := New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}

	sh.Logger = DiscardLogger
	sh.Writer = io.Discard

	if os.Getenv(`DEBUG_SHELL`) == "1" {
		sh.Logger = TestingLogger{T: t}
		sh.Writer = os.Stdout
	}

	// Windows requires certain env variables to be present
	if runtime.GOOS == "windows" {
		sh.Env = env.FromMap(map[string]string{
			//"PATH":        os.Getenv("PATH"),
			"SystemRoot":  os.Getenv("SystemRoot"),
			"WINDIR":      os.Getenv("WINDIR"),
			"COMSPEC":     os.Getenv("COMSPEC"),
			"PATHEXT":     os.Getenv("PATHEXT"),
			"TMP":         os.Getenv("TMP"),
			"TEMP":        os.Getenv("TEMP"),
			"ProgramData": os.Getenv("ProgramData"),
		})
	} else {
		sh.Env = env.FromMap(map[string]string{
			"PATH": os.Getenv("PATH"),
		})
	}

	return sh
}
