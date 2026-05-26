package archive

import (
	"fmt"
	"os"
	"path/filepath"
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

// PathsToMappings takes a slice of file paths and returns a slice of Mapping structs,
// which contain information about the destination path, chroot path, and whether
// the path is relative or not. It handles paths starting with "~/" by replacing
// them with the user's home directory.
func PathsToMappings(paths []string) ([]Mapping, error) {
	pathMappings := make([]Mapping, 0, len(paths))

	for _, path := range paths {
		mapping := Mapping{
			Path:         path,
			ResolvedPath: path,
			RelativePath: path,
			Relative:     true,
		}

		homedir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}

		if strings.HasPrefix(path, "~/") {
			mapping.ResolvedPath = filepath.Join(homedir, path[2:])
			mapping.Relative = false

			rel, err := filepath.Rel(homedir, mapping.ResolvedPath)
			if err != nil {
				return nil, fmt.Errorf("failed to get relative path: %w", err)
			}

			mapping.RelativePath = rel
		} else if filepath.IsAbs(path) && strings.HasPrefix(path, homedir) {
			mapping.Relative = false

			rel, err := filepath.Rel(homedir, path)
			if err != nil {
				return nil, fmt.Errorf("failed to get relative path: %w", err)
			}

			mapping.RelativePath = rel
		}

		chroot, err := chrootPath(mapping.ResolvedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get chroot path: %w", err)
		}

		mapping.Chroot = chroot

		pathMappings = append(pathMappings, mapping)
	}

	return pathMappings, nil
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

func chrootPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return os.UserHomeDir()
	}

	// get the current working directory
	return os.Getwd()
}
