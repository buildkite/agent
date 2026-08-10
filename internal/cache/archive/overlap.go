package archive

import (
	"os"
	"path/filepath"
	"strings"
)

// Overlap detection over a set of resolved target paths. It is shared by both
// save (create.go rejects overlapping targets up front) and restore
// (restore.go re-checks the re-resolved paths, since portable anchors can
// converge on a different machine).

// OverlappingPaths returns the indices of the first pair of resolved paths that
// are nested or equal — by lexical and canonical spelling, case-folded on
// case-insensitive filesystems — or ok=false if none overlap.
func OverlappingPaths(paths []string) (i, j int, ok bool) {
	canonical := make([]string, len(paths))
	insensitive := make([]bool, len(paths))
	for k, p := range paths {
		canonical[k] = canonicalizeForOverlap(p)
		insensitive[k] = pathCaseInsensitive(p)
	}
	for a := 0; a < len(paths); a++ {
		for b := a + 1; b < len(paths); b++ {
			ci := insensitive[a] || insensitive[b]
			if pathsOverlap(paths[a], paths[b], ci) || pathsOverlap(canonical[a], canonical[b], ci) {
				return a, b, true
			}
		}
	}
	return 0, 0, false
}

// pathsOverlap reports whether two paths are nested or equal, folding case when
// caseInsensitive (isUnder stays case-sensitive for its other callers).
func pathsOverlap(a, b string, caseInsensitive bool) bool {
	if caseInsensitive {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	return isUnder(a, b) || isUnder(b, a)
}

// ~/Cache and ~/cache are the same directory on a case-insensitive FS (so they collide)
// but distinct on a case-sensitive one.
// pathCaseInsensitive reports whether the filesystem that will hold p folds
// case. When p already exists it decides from p's own casing by identity, otherwise it
// creates a temp entry in the nearest existing directory and re-cases it. Assumed
// case-sensitive if it can't confirm.
func pathCaseInsensitive(p string) bool {
	if orig, err := os.Lstat(p); err == nil {
		seg := strings.Split(p, string(filepath.Separator))
		for i := len(seg) - 1; i >= 0; i-- {
			if recase(seg[i]) == seg[i] {
				continue
			}
			seg[i] = recase(seg[i])
			alt, err2 := os.Lstat(strings.Join(seg, string(filepath.Separator)))
			return err2 == nil && os.SameFile(orig, alt)
		}
	}

	dir := p
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false // no existing ancestor directory
		}
		dir = parent
	}

	// The "bk-case-probe-" prefix guarantees a letter to re-case (MkdirTemp's
	// random suffix is numeric).
	marker, err := os.MkdirTemp(dir, "bk-case-probe-")
	if err != nil {
		return false
	}
	defer func() { _ = os.Remove(marker) }()

	orig, err1 := os.Stat(marker)
	alt, err2 := os.Stat(filepath.Join(dir, recase(filepath.Base(marker))))
	return err1 == nil && err2 == nil && os.SameFile(orig, alt)
}

// recase returns s with its case flipped (upper, else lower). It returns s
// unchanged when there is no letter to re-case.
func recase(s string) string {
	if up := strings.ToUpper(s); up != s {
		return up
	}
	return strings.ToLower(s)
}

// canonicalizeForOverlap resolves symlinks in a path's parent but preserves the
// final component — matching filepath.Walk, which archives a final symlink as
// the link, not its referent. Falls back to the lexical path on error.
func canonicalizeForOverlap(p string) string {
	dir, base := filepath.Split(p)
	realDir, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return p
	}
	return filepath.Join(realDir, base)
}
