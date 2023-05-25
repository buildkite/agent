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
	sockNum := rand.Int63() % 100_000
	return filepath.Join(base, "job-api", fmt.Sprintf("%d-%d.sock", os.Getpid(), sockNum)), nil
}
