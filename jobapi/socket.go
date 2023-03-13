package jobapi

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// NewSocketPath generates a path to a socket file (without actually creating the file itself) that can be used with the
// job api.
func NewSocketPath(base string) (string, error) {
	path := filepath.Join(base, "job-api")
	err := os.MkdirAll(path, 0700)
	if err != nil {
		return "", fmt.Errorf("creating socket directory: %w", err)
	}

	sockNum := rand.Int63() % 100_000
	return filepath.Join(path, fmt.Sprintf("%d-%d.sock", os.Getpid(), sockNum)), nil
}
