package cache

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/buildkite/agent/v3/internal/cache/archive"
)

// checkPathsExist validates that all paths exist on the filesystem
func checkPathsExist(paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths provided")
	}

	for _, path := range paths {
		resolved, err := archive.ResolveHomeDir(path)
		if err != nil {
			return fmt.Errorf("failed to resolve home dir for %q: %w", path, err)
		}

		if _, err := os.Stat(resolved); os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", path)
		}
	}

	return nil
}

// validateTargetPaths checks that every target path is supported by the
// archive mapping rules and would be accepted by cleanPath. Save and Restore
// both run it at their validating stage: Save so it never creates an entry
// that could not be restored, Restore so an invalid path fails the whole
// operation before anything is destructively cleaned.
func validateTargetPaths(paths []string) error {
	if _, err := archive.PathsToMappings(paths); err != nil {
		return err
	}

	for _, path := range paths {
		extractedPath, err := archive.ResolveHomeDir(path)
		if err != nil {
			return fmt.Errorf("failed to resolve home dir for %q: %w", path, err)
		}
		if _, err := validateCleanPath(extractedPath); err != nil {
			return err
		}
	}

	return nil
}

// validateCleanPath checks that dir is safe to remove and returns its cleaned
// form. It refuses paths whose removal could be catastrophic: the filesystem
// root, drive roots, the home directory, top-level directories such as /etc,
// and relative paths escaping the working directory.
func validateCleanPath(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("cleanPath: empty directory path")
	}

	clean := filepath.Clean(dir)

	// Refuse to delete root or current directory
	if clean == "." || clean == string(os.PathSeparator) {
		return "", fmt.Errorf("cleanPath: refusing to remove %q", clean)
	}

	// On Windows, also check for drive roots like "C:\"
	if runtime.GOOS == "windows" && len(clean) == 3 && clean[1] == ':' && clean[2] == '\\' {
		return "", fmt.Errorf("cleanPath: refusing to remove drive root %q", clean)
	}

	// Refuse to delete home directory
	if home, err := os.UserHomeDir(); err == nil {
		if clean == filepath.Clean(home) {
			return "", fmt.Errorf("cleanPath: refusing to remove home directory %q", clean)
		}
	}

	// Absolute paths may point anywhere on the filesystem, so refuse to
	// delete top-level directories such as /etc or /usr: require absolute
	// paths to be at least two components deep.
	if filepath.IsAbs(clean) {
		root := filepath.VolumeName(clean) + string(os.PathSeparator)
		rel, err := filepath.Rel(root, clean)
		if err != nil || !strings.Contains(rel, string(os.PathSeparator)) {
			return "", fmt.Errorf("cleanPath: refusing to remove top-level path %q", clean)
		}
	}

	// On Windows a rooted path like "\etc" or a drive-relative path like
	// "C:cache" is not absolute, but not relative to the working directory
	// either; refuse it.
	if runtime.GOOS == "windows" && !filepath.IsAbs(clean) && (filepath.VolumeName(clean) != "" || strings.HasPrefix(clean, string(os.PathSeparator))) {
		return "", fmt.Errorf("cleanPath: refusing to remove rooted path %q", clean)
	}

	// Refuse to delete the working directory or any of its ancestors
	// (e.g. ".." or an absolute path containing the working directory),
	// which would delete the working directory itself.
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("cleanPath: failed to resolve %q: %w", clean, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cleanPath: failed to get working directory: %w", err)
	}
	if archive.IsUnder(cwd, abs) {
		return "", fmt.Errorf("cleanPath: refusing to remove the working directory or an ancestor of it: %q", clean)
	}

	return clean, nil
}

// cleanPath removes a directory tree for a configured cache path.
// It handles Go module cache directories that have 0555 permissions by
// making them writable before removal.
func cleanPath(ctx context.Context, dir string) error {
	clean, err := validateCleanPath(dir)
	if err != nil {
		return err
	}

	// Module cache has 0555 directories; make them writable in order to remove content.
	if err := makeTreeWritable(ctx, clean); err != nil {
		return err
	}

	// Check context again before potentially long RemoveAll
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := os.RemoveAll(clean); err != nil {
		return fmt.Errorf("cleanPath: failed to remove %q: %w", clean, err)
	}

	return nil
}

// makeTreeWritable walks `clean` and chmods every directory to 0755 so that
// the subsequent os.RemoveAll can delete read-only entries (e.g. Go module
// cache). The os.Root handle is closed before returning so that the caller
// can remove `clean` on platforms (Windows) that disallow removing a
// directory with an open handle.
func makeTreeWritable(ctx context.Context, clean string) error {
	root, err := os.OpenRoot(clean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cleanPath: open root %q: %w", clean, err)
	}
	defer func() { _ = root.Close() }()

	err = fs.WalkDir(root.FS(), ".", func(relPath string, info fs.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if walkErr != nil {
			slog.Debug("cleanPath: error walking path", "path", relPath, "err", walkErr)
			return nil
		}

		if info.IsDir() {
			if chmodErr := root.Chmod(relPath, 0o755); chmodErr != nil {
				return fmt.Errorf("chmod %q: %w", relPath, chmodErr)
			}
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return fmt.Errorf("cleanPath: error preparing %q for removal: %w", clean, err)
	}
	return nil
}
