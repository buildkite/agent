// Package shellscript contains helpers for dealing with shell scripts.
package shellscript

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/shellwords"
)

// ShebangLine extracts the shebang line from the file, if present. If the file
// is readable but contains no shebang line, it will return an empty string.
// Non-nil errors only reflect an inability to read the file.
func ShebangLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // File only open for read.
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		// If the scan ended because of EOF, the file is empty and sc.Err = nil.
		// Otherwise sc.Err reflects the error reading the file.
		return "", sc.Err()
	}
	line := sc.Text()
	if !strings.HasPrefix(line, "#!") {
		// Not a shebang line.
		return "", nil
	}
	return line, nil
}

// IsPOSIXShell attempts to detect POSIX-compliant shells (e.g bash, sh, zsh)
// from either a plain command line, or a shebang line.
//
// Examples:
//   - IsPOSIXShell("/bin/sh") == true
//   - IsPOSIXShell("/bin/fish") == false
//   - IsPOSIXShell("#!/usr/bin/env bash") == true
//   - IsPOSIXShell("#!/usr/bin/env python3") == false
func IsPOSIXShell(line string) bool {
	parts, err := shellwords.Split(strings.TrimPrefix(line, "#!"))
	if err != nil || len(parts) == 0 {
		return false
	}

	bin := filepath.Base(parts[0])
	if bin == "env" {
		if len(parts) < 2 {
			return false
		}
		bin = filepath.Base(parts[1])
	}

	switch bin {
	case "bash", "dash", "ksh", "sh", "zsh":
		return true
	default:
		return false
	}
}
