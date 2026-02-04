package hook

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
)

// Magic numbers for various types of executable files
var (
	// Linux and many other Unix-like systems use ELF binaries
	ELFMagic = []byte{0x7F, 0x45, 0x4C, 0x46}

	// Windows uses MS-DOS MZ-style executables. This includes PE32 and PE32+ (which is 64-bit 🙃) binaries,
	// which are what modern windowses tend to use.
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
func isBinaryExecutable(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open file %q: %w", path, err)
	}

	defer f.Close() //nolint:errcheck // File is only open for read.

	fileInfo, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("stat file %q: %w", path, err)
	}

	if fileInfo.Size() < 4 {
		// there are less than four bytes in the file, we assume it is an empty file and there's nothing that we can do with it
		return false, nil
	}

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
		if bytes.HasPrefix(firstFour, magicNumber) {
			return true, nil
		}
	}

	return false, nil
}
