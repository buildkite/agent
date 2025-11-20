//go:build !windows
// +build !windows

// This file (along with its Windows counterpart) have been taken from:
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
	if m := d.Mode(); !m.IsDir() && m&0o111 != 0 {
		return nil
	}
	return os.ErrPermission
}

// LookPath searches for an executable binary named file in the directories within the path variable,
// which is a colon delimited path.
// If file contains a slash, it is tried directly
func LookPath(file, path, fileExtensions string) (string, error) {
	if strings.Contains(file, "/") {
		err := findExecutable(file)
		if err == nil {
			return file, nil
		}
		return "", &exec.Error{Name: file, Err: err}
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
	return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
}
