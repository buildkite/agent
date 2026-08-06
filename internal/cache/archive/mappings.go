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
	// AnchorCWD marks relative paths. Restore resolves them against the job's
	// working directory.
	AnchorCWD = "."
	// Absolute paths are pinned to a volume/filesystem root ("/" on POSIX, a
	// drive root like "C:\" on Windows); those anchors are produced by
	// volumeRoot and recognised by isRootAnchor rather than a named constant.
)

// Mapping describes how one configured target path maps into an archive: a
// numbered namespace ("_0", "_1", ...) whose entries are relative to the anchor.
type Mapping struct {
	// Path is the target path exactly as configured.
	Path string
	// Namespace is the archive prefix that isolates this mapping's entries ("_0").
	Namespace string
	// Anchor is the portable symbol stored in the manifest (see consts above).
	Anchor string
	// base is the absolute directory Anchor resolves to on a machine.
	base string
	// resolved is the absolute path the target resolves to on this machine.
	resolved string
}

// ResolvedPath is the absolute path on this machine the target resolves to.
func (m Mapping) ResolvedPath() string { return m.resolved }

// archiveLayout returns the directory quickzip archives from (chroot) and the
// entry-name prefix prepended so each stored entry is "<namespace>/..". Every
// anchor chroots at its base (home, cwd, or the volume root), so quickzip names
// entries relative to that base and the prefix only adds the namespace.
func (m Mapping) archiveLayout() (chroot, prefix string) {
	return m.base, m.Namespace + "/"
}

// homeDir returns the cleaned home directory (os.UserHomeDir returns $HOME
// verbatim), keeping save and restore resolution consistent.
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
		anchor, base, resolved := classifyAndResolvePath(path, home, cwd)
		// A target can't be a bare volume/filesystem root ("/" or "C:\")
		if isRootAnchor(anchor) && resolved == base {
			return nil, fmt.Errorf("cannot cache the volume/filesystem root %q", resolved)
		}
		mappings = append(mappings, Mapping{
			Path:      path,
			Namespace: fmt.Sprintf("_%d", i),
			Anchor:    anchor,
			base:      base,
			resolved:  resolved,
		})
	}

	return mappings, nil
}

// classifyAndResolvePath determines a configured path's anchor, the absolute
// base directory that anchor resolves to, and the absolute path the target
// resolves to in the current environment.
func classifyAndResolvePath(path, home, cwd string) (anchor, base, resolved string) {
	switch {
	case path == "~" || strings.HasPrefix(path, "~/"):
		rest := strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/")
		return AnchorHome, home, filepath.Join(home, rest)

	case filepath.IsAbs(path):
		resolved := filepath.Clean(path)
		// Absolute paths are pinned — never follow $HOME, even under it. The
		// anchor is the volume root, so a Windows drive is retained.
		root := volumeRoot(resolved)
		return root, root, resolved

	default:
		return AnchorCWD, cwd, filepath.Join(cwd, path)
	}
}

// volumeRoot returns the volume/filesystem root of an absolute path: "/" on
// POSIX, a drive root ("C:\") on Windows. This is the pinned-absolute anchor.
func volumeRoot(p string) string {
	return filepath.VolumeName(p) + string(filepath.Separator)
}

// isRootAnchor reports whether an anchor is a volume/filesystem root ("/" or
// "C:\") — the pinned-absolute anchor produced by volumeRoot.
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

	_, _, resolved := classifyAndResolvePath(path, home, cwd)
	return resolved, nil
}
