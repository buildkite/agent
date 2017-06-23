// +build !windows

// This file (along with it's Windows counterpart) have been taken from:
//
// https://github.com/golang/go/blob/master/src/os/exec/lp.go
//
// Their implemenations are exactly the same, however in this version - the
// paths to search in (along with file extensions to look at) can be
// customized.

package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func findExecutable(file string) error {
	d, err := os.Stat(file)
	if err != nil {
		return err
	}
	if m := d.Mode(); !m.IsDir() && m&0111 != 0 {
		return nil
	}
	return os.ErrPermission
}

// Note that `fileExtensions` are ignored in the *nix implementation of `lookPath`
// (they're used in the Windows version however!)
func lookPath(file string, path string, fileExtensions string) (string, error) {
	if strings.Contains(file, "/") {
		err := findExecutable(file)
		if err == nil {
			return file, nil
		}
		return "", &exec.Error{file, err}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			// Unix shell semantics: path element "" means "."
			dir = "."
		}
		path := filepath.Join(dir, file)
		if err := findExecutable(path); err == nil {
			return path, nil
		}
	}
	return "", &exec.Error{file, exec.ErrNotFound}
}
