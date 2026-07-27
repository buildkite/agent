package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Anchor identifies where a mapping's stored paths are re-resolved at restore time.
// It is deliberately a symbol rather than a resolved path so caches stay portable.
const (
	// AnchorHome marks paths configured with a leading "~" (or "~/"). Restore
	// re-expands them against the restoring environment's home directory.
	AnchorHome = "~"
	// AnchorRoot marks absolute paths. Restore pins them to their exact
	// location, unchanged across machines.
	AnchorRoot = "/"
	// AnchorCWD marks relative paths. Restore resolves them against the job's
	// working directory.
	AnchorCWD = "."
)

// Mapping describes how one configured target path is represented in an archive.
// Each mapping owns a numbered namespace ("_0", "_1", ...) inside the
// archive; entries under that namespace carry the path relative to the
// mapping's anchor.
type Mapping struct {
	// Path is the target path exactly as configured.
	Path string
	// Namespace is the archive prefix that isolates this mapping's entries,
	Namespace string
	// Anchor is one of AnchorHome, AnchorRoot or AnchorCWD.
	Anchor string
	// ResolvedPath is the absolute path on the machine that the target path
	// resolves to.
	ResolvedPath string
}

// homeDir returns the cleaned home directory. os.UserHomeDir returns $HOME
// verbatim on Unix. Cleaning it keeps save classification and
// restore anchor resolution byte-consistent.
func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Clean(home), nil
}

// workingDir returns the cleaned working directory.
func workingDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}
	return filepath.Clean(cwd), nil
}

// PathsToMappings resolves each configured target path into a Mapping.
func PathsToMappings(paths []string) ([]Mapping, error) {
	home, err := homeDir()
	if err != nil {
		return nil, err
	}

	cwd, err := workingDir()
	if err != nil {
		return nil, err
	}

	mappings := make([]Mapping, 0, len(paths))
	for i, path := range paths {
		anchor, resolved := classifyPath(path, home, cwd)
		mappings = append(mappings, Mapping{
			Path:         path,
			Namespace:    fmt.Sprintf("_%d", i),
			Anchor:       anchor,
			ResolvedPath: resolved,
		})
	}

	return mappings, nil
}

// classifyPath determines a configured path's anchor and the absolute path it
// resolves to in the current environment.
func classifyPath(path, home, cwd string) (anchor, resolved string) {
	switch {
	case path == "~" || strings.HasPrefix(path, "~/"):
		rest := strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/")
		return AnchorHome, filepath.Join(home, rest)

	case filepath.IsAbs(path):
		resolved := filepath.Clean(path)
		// Absolute paths are pinned - they restore to their exact
		// location and never follow $HOME — even when they happen to sit under
		// it. The anchor is the path's volume root, so a Windows drive is
		// retained ("C:\"); on POSIX it is always "/".
		return volumeRoot(resolved), resolved

	default:
		return AnchorCWD, filepath.Join(cwd, path)
	}
}

// volumeRoot returns the filesystem/volume root of an absolute path: "/" on
// POSIX, or the drive root ("C:\") on Windows. This is the pinned-absolute
// anchor — storing the volume keeps a Windows drive from being lost while
// still pinning the path to its exact location.
func volumeRoot(p string) string {
	return filepath.VolumeName(p) + string(filepath.Separator)
}

// isRootAnchor reports whether an anchor is a volume/filesystem root — the
// pinned-absolute anchor produced by volumeRoot ("/" on POSIX, "C:\" on
// Windows).
func isRootAnchor(anchor string) bool {
	return anchor != "" && anchor == volumeRoot(anchor)
}

// resolveAnchor returns the absolute base directory an anchor expands to in the
// current environment. It is the inverse of the anchor classification done in
// PathsToMappings and is used at restore time to re-root a stored entry.
func resolveAnchor(anchor string) (string, error) {
	switch {
	case anchor == AnchorHome:
		return homeDir()
	case anchor == AnchorCWD:
		return workingDir()
	case isRootAnchor(anchor):
		// A volume/filesystem root ("/" or "C:\") is already an absolute base.
		return anchor, nil
	default:
		return "", fmt.Errorf("unknown anchor %q", anchor)
	}
}

// ResolveConfigPath normalises a configured target path to the absolute,
// cleaned path it refers to on this machine, using the same classification as
// PathsToMappings.
func ResolveConfigPath(path string) (string, error) {
	// Only resolve the piece of environment the path actually needs: an
	// absolute path depends on neither, and shouldn't fail a restore because
	// (say) the cwd was since deleted.
	var home, cwd string
	var err error

	switch {
	case path == "~" || strings.HasPrefix(path, "~/"):
		home, err = homeDir()
	case !filepath.IsAbs(path):
		cwd, err = workingDir()
	}
	if err != nil {
		return "", fmt.Errorf("failed to resolve config path %q: %w", path, err)
	}

	_, resolved := classifyPath(path, home, cwd)
	return resolved, nil
}
