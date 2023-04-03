// Package shellscript contains helpers for dealing with shell scripts.
package shellscript

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildkite/shellwords"
)

// Magic numbers for various types of executable files
var (
	// Linux and many other Unix-like systems use ELF binaries
	ELFMagic = []byte{0x7F, 0x45, 0x4C, 0x46}

	// Windows uses MS-DOS MZ-style executables. This includes PE32 and PE32+ (which is 64-bit 🙃) binaries,
	// which a are what modern windowses tend to use.
	MZMagic = []byte{0x4D, 0x5A}

	// macOS uses Mach-O binaries. There are two variants: fat and thin.
	// Thin binaries are further divided into 32-bit and 64-bit variants,
	// which are furtherer subdivided by processor endianess. It's a mess.

	// For "fat" binaries, which contain multiple architectures
	MachOFatMagic = []byte{0xCA, 0xFE, 0xBA, 0xBE}

	// For "thin" binaries, which contain a single architecture
	// (ie 32 and 64 bit variants, on top of different processor architectures)
	MachOThin32Magic = []byte{0xFE, 0xED, 0xFA, 0xCE}
	MachOThin64Magic = []byte{0xFE, 0xED, 0xFA, 0xCF}

	// Mach-O thin binaries can also be in reverse byte order, apparently?
	// I think this is for big endian processors like PowerPC, in which case we don't need these here,
	// but we might as well include them for completeness.
	MachOThin32ReverseMagic = []byte{0xCE, 0xFA, 0xED, 0xFE}
	MachOThin64ReverseMagic = []byte{0xCF, 0xFA, 0xED, 0xFE}

	BinaryMagicks = [][]byte{
		ELFMagic,
		MZMagic,
		MachOFatMagic,
		MachOThin32Magic,
		MachOThin64Magic,
		MachOThin32ReverseMagic,
		MachOThin64ReverseMagic,
	}
)

// ExecutableType returns the type of file executable at the given path.
// The file at the given path is assumed to be an executable, and executability is not checked.
func IsBinaryExecutable(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open file %q: %w", path, err)
	}

	defer f.Close()
	r := bufio.NewReader(f)
	firstFour, err := r.Peek(4)
	if err != nil {
		return false, fmt.Errorf("reading first four bytes of file %q: %w", path, err)
	}

	if len(firstFour) < 4 {
		// there are less than four bytes in the file, there's nothing that we can do with it
		return false, nil
	}

	for _, magicNumber := range BinaryMagicks {
		if sliceHasPrefix(firstFour, magicNumber) {
			return true, nil
		}
	}

	return false, nil
}

func sliceHasPrefix(s, prefix []byte) bool {
	return len(s) >= len(prefix) && bytes.Equal(s[:len(prefix)], prefix)
}

// ShebangLine extracts the shebang line from the file, if present. If the file
// is readable but contains no shebang line, it will return an empty string.
// Non-nil errors only reflect an inability to read the file.
func ShebangLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
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
