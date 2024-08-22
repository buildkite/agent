//go:build !unix

package job

import "os"

// hardRemoveAll only does more than os.RemoveAll on Unix-likes.
func hardRemoveAll(path string) error {
	return os.RemoveAll(path)
}
