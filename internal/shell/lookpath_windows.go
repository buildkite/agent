// This file (along with its *nix counterpart) have been taken from:
//
// https://github.com/golang/go/blob/master/src/os/exec/lp_windows.go
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

func chkStat(file string) error {
	d, err := os.Stat(file)
	if err != nil {
		return err
	}
	if d.IsDir() {
		return os.ErrPermission
	}
	return nil
}

func hasExt(file string) bool {
	i := strings.LastIndex(file, ".")
	if i < 0 {
		return false
	}
	return strings.LastIndexAny(file, `:\/`) < i
}

func findExecutable(file string, exts []string) (string, error) {
	if len(exts) == 0 {
		return file, chkStat(file)
	}
	if hasExt(file) {
		if chkStat(file) == nil {
			return file, nil
		}
	}
	for _, e := range exts {
		if f := file + e; chkStat(f) == nil {
			return f, nil
		}
	}
	return "", os.ErrNotExist
}

// LookPath searches for an executable binary named file in the directories within the path variable,
// which is a semi-colon delimited path.
// If file contains a slash, it is tried directly
// LookPath also uses PATHEXT environment variable to match a suitable candidate.
// The result may be an absolute path or a path relative to the current directory.
func LookPath(file, path, fileExtensions string) (string, error) {
	var exts []string
	if fileExtensions != "" {
		for _, e := range strings.Split(strings.ToLower(fileExtensions), ";") {
			if e == "" {
				continue
			}
			if e[0] != '.' {
				e = "." + e
			}
			exts = append(exts, e)
		}
	} else {
		exts = []string{".com", ".exe", ".bat", ".cmd"}
	}

	if strings.ContainsAny(file, `:\/`) {
		if f, err := findExecutable(file, exts); err == nil {
			return f, nil
		} else {
			return "", &exec.Error{file, err}
		}
	}
	if f, err := findExecutable(filepath.Join(".", file), exts); err == nil {
		return f, nil
	}
	for _, dir := range filepath.SplitList(path) {
		if f, err := findExecutable(filepath.Join(dir, file), exts); err == nil {
			return f, nil
		}
	}
	return "", &exec.Error{file, exec.ErrNotFound}
}
