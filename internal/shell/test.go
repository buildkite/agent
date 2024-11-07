package shell

import (
	"io"
	"os"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v3/env"
)

// NewTestShell creates a shell with suitable defaults for tests. Note that it
// still really for real executes commands, unless WithDryRun(true) is passed.
// By default is set up with minimal env vars, and throws away stdout (unless
// DEBUG_SHELL is set or WithStdout is passed).
func NewTestShell(t *testing.T, opts ...NewShellOpt) *Shell {
	t.Helper()

	logger := Logger(DiscardLogger)
	stdout := io.Discard
	if os.Getenv(`DEBUG_SHELL`) == "1" {
		logger = TestingLogger{T: t}
		stdout = os.Stdout
	}

	var environ *env.Environment
	switch runtime.GOOS {
	case "windows":
		// Windows requires certain env variables to be present
		environ = env.FromMap(map[string]string{
			"PATH":        os.Getenv("PATH"),
			"SystemRoot":  os.Getenv("SystemRoot"),
			"WINDIR":      os.Getenv("WINDIR"),
			"COMSPEC":     os.Getenv("COMSPEC"),
			"PATHEXT":     os.Getenv("PATHEXT"),
			"TMP":         os.Getenv("TMP"),
			"TEMP":        os.Getenv("TEMP"),
			"ProgramData": os.Getenv("ProgramData"),
			"USERPROFILE": os.Getenv("USERPROFILE"),
		})

	default:
		environ = env.FromMap(map[string]string{
			"PATH": os.Getenv("PATH"),
		})
	}

	opts = append([]NewShellOpt{
		WithEnv(environ),
		WithLogger(logger),
		WithStdout(stdout),
	}, opts...)

	sh, err := New(opts...)
	if err != nil {
		t.Fatalf("shell.New(opts...) error = %v", err)
	}
	return sh
}
