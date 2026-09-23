package cache

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cleanupMount reports whether dir itself is mounted, rejecting nested mounts
// before cleanup can chmod or delete files on them. This is a snapshot, not
// protection against concurrent mount changes or writers.
func cleanupMount(dir string) (bool, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, nil // Preserve final-symlink and regular-file handling.
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return false, err
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return false, fmt.Errorf("inspect mounts for %q: %w", dir, err)
	}
	defer func() { _ = f.Close() }()

	// mountinfo escapes whitespace and backslashes in path fields. Unlike
	// device-ID comparisons, this also identifies same-filesystem bind mounts.
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	mounted := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			return false, fmt.Errorf("inspect mounts for %q: malformed mountinfo entry", dir)
		}
		mount := unescape.Replace(fields[4])
		if mount == resolved {
			mounted = true
		} else if strings.HasPrefix(mount, strings.TrimSuffix(resolved, "/")+"/") {
			return false, fmt.Errorf("refusing to clean %q: contains nested mount %q", dir, mount)
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("inspect mounts for %q: %w", dir, err)
	}
	return mounted, nil
}
