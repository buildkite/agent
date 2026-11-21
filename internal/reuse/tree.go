package reuse

import (
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeebo/blake3"
)

// treeEntry is a file or symlink within an output path.
type treeEntry struct {
	rel  string // forward-slash path relative to the output root
	line string // serialized digest line
}

// TreeDigest digests the content of an output path, which is either a
// directory or a single file (or symlink). Paths within a directory are
// relative to it; a single file is named by its base name. The digest
// depends only on paths, contents, symlink targets and executable bits, not on
// archive bytes or modification times. Directories themselves are ignored.
func TreeDigest(root string) (digest string, files int, err error) {
	var entries []treeEntry

	fi, err := os.Lstat(root)
	if err != nil {
		return "", 0, err
	}

	if fi.IsDir() {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == root || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			e, ok, err := treeLine(filepath.ToSlash(rel), path, info)
			if err != nil {
				return err
			}
			if ok {
				entries = append(entries, e)
			}
			return nil
		})
		if err != nil {
			return "", 0, err
		}
	} else {
		e, ok, err := treeLine(filepath.Base(root), root, fi)
		if err != nil {
			return "", 0, err
		}
		if ok {
			entries = append(entries, e)
		}
	}

	slices.SortFunc(entries, func(a, b treeEntry) int { return strings.Compare(a.rel, b.rel) })

	h := blake3.New()
	for _, e := range entries {
		_, _ = io.WriteString(h, e.line)
	}
	return DigestPrefix + hex.EncodeToString(h.Sum(nil)), len(entries), nil
}

func treeLine(rel, path string, info fs.FileInfo) (treeEntry, bool, error) {
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return treeEntry{}, false, err
		}
		return treeEntry{rel: rel, line: rel + "\x00l\x00-\x00" + leafOf([]byte(target)) + "\n"}, true, nil

	case info.Mode().IsRegular():
		leaf, err := fileLeaf(path)
		if err != nil {
			return treeEntry{}, false, err
		}
		x := "-"
		if info.Mode()&0o111 != 0 {
			x = "x"
		}
		return treeEntry{rel: rel, line: rel + "\x00f\x00" + x + "\x00" + leaf + "\n"}, true, nil
	}
	return treeEntry{}, false, nil
}
