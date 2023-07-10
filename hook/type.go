package hook

import (
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/internal/shellscript"
)

const (
	TypeShell   = "shell"
	TypeBinary  = "binary"
	TypeScript  = "script" // Something with a shebang that isn't a shell script, but is an interpretable something-or-other
	TypeUnknown = "unknown"
)

func Type(path string) (string, error) {
	_, err := os.Open(path)
	if err != nil {
		return TypeUnknown, fmt.Errorf("opening hook: %w", err)
	}

	isBinary, err := isBinaryExecutable(path)
	if err != nil {
		return TypeUnknown, fmt.Errorf("determining if %q is a binary: %w", path, err)
	}

	if isBinary {
		return TypeBinary, nil
	}

	shebangLine, err := shellscript.ShebangLine(path)
	if err != nil {
		return TypeUnknown, fmt.Errorf("reading shebang line for hook: %w", err)
	}

	switch {
	case shellscript.IsPOSIXShell(shebangLine):
		return TypeShell, nil

	case shebangLine != "" && !shellscript.IsPOSIXShell(shebangLine):
		return TypeScript, nil

	case shebangLine == "":
		return TypeShell, nil // Assume shell if no shebang line is present and it's not a binary

	default:
		return TypeUnknown, fmt.Errorf(`the buildkite agent wasn't able to determine the type of hook it was trying to run.
This is a bug, please contact support@buildkite.com and/or submit an issue on the github.com/buildkite/agent repo with the following information:
Hook path: %q
Shebang line: %q
Is binary?: %t`, path, shebangLine, isBinary)
	}
}
