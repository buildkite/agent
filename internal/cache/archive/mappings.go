package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Mapping represents a mapping of a file path to a destination path, including
// the chroot path and whether the path is relative or not.
type Mapping struct {
	Path         string
	ResolvedPath string
	RelativePath string
	Chroot       string
	Relative     bool
}

// PathsToMappings takes a slice of file paths and returns a slice of Mapping
// structs, which contain information about the destination path, chroot path,
// and whether the path is relative or not.
//
// Paths are anchored as follows:
//   - "~/..." and absolute paths under the home directory are stored
//     home-relative (chroot is the home directory), so home caches stay
//     portable across agents running as different users.
//   - Other absolute paths are not supported.
//   - Everything else is relative to the current working directory.
func PathsToMappings(paths []string) ([]Mapping, error) {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	pathMappings := make([]Mapping, 0, len(paths))
	for _, path := range paths {
		mapping, err := pathToMapping(path, homedir)
		if err != nil {
			return nil, err
		}
		pathMappings = append(pathMappings, mapping)
	}

	return pathMappings, nil
}

func pathToMapping(path, homedir string) (Mapping, error) {
	mapping := Mapping{
		Path:         path,
		ResolvedPath: path,
		RelativePath: path,
		Relative:     true,
	}

	switch {
	case strings.HasPrefix(path, "~/"):
		mapping.ResolvedPath = filepath.Join(homedir, path[2:])
		if !IsUnder(mapping.ResolvedPath, homedir) {
			return Mapping{}, fmt.Errorf("path escapes the home directory: %s", path)
		}
		mapping.Relative = false
		mapping.Chroot = homedir

		rel, err := filepath.Rel(homedir, mapping.ResolvedPath)
		if err != nil {
			return Mapping{}, fmt.Errorf("failed to get relative path: %w", err)
		}
		mapping.RelativePath = rel

	case filepath.IsAbs(path) && IsUnder(path, homedir):
		mapping.Relative = false
		mapping.Chroot = homedir

		rel, err := filepath.Rel(homedir, path)
		if err != nil {
			return Mapping{}, fmt.Errorf("failed to get relative path: %w", err)
		}
		mapping.RelativePath = rel

	case filepath.IsAbs(path):
		return Mapping{}, fmt.Errorf("absolute paths outside the home directory are not supported: %s", path)

	default:
		// On Windows, rooted ("\etc" or "/etc") and drive-relative
		// ("C:cache") paths are not absolute, but they are not relative to
		// the working directory either: their meaning depends on the
		// process's current drive and directory state.
		if runtime.GOOS == "windows" && (filepath.VolumeName(path) != "" || strings.HasPrefix(path, `\`) || strings.HasPrefix(path, "/")) {
			return Mapping{}, fmt.Errorf("rooted and drive-relative paths are not supported on Windows: %s", path)
		}

		cwd, err := os.Getwd()
		if err != nil {
			return Mapping{}, fmt.Errorf("failed to get working directory: %w", err)
		}

		// Relative paths must stay within the working directory: the
		// archiver cannot include files outside the chroot, so a
		// parent-relative path like "../cache" could never be saved.
		if !IsUnder(filepath.Join(cwd, path), cwd) {
			return Mapping{}, fmt.Errorf("relative path escapes the working directory: %s", path)
		}
		mapping.Chroot = cwd
	}

	return mapping, nil
}

// IsUnder reports whether path is dir itself or a descendant of dir. Unlike a
// plain prefix check, it does not treat a sibling like "/home/user2" as being
// under "/home/user".
func IsUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func ResolveHomeDir(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		return filepath.Join(homedir, path[2:]), nil
	}
	return path, nil
}
