package store

import (
	"path/filepath"
	"strings"
)

// fileURL returns a well-formed file:// URL for an absolute OS path. On
// Windows the path "C:\foo" becomes "file:///C:/foo".
func fileURL(absPath string) string {
	urlPath := filepath.ToSlash(absPath)
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	return "file://" + urlPath
}
