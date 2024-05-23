package jobapi

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// NewSocketPath generates a path to a socket file (without actually creating the file itself) that can be used with the
// job api.
func NewSocketPath(base string) (string, error) {
	sockNum := rand.IntN(100_000)
	return filepath.Join(base, "job-api", fmt.Sprintf("%d-%d.sock", os.Getpid(), sockNum)), nil
}
