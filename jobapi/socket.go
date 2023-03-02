package jobapi

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/mitchellh/go-homedir"
)

var random = rand.New(rand.NewSource(time.Now().UnixNano()))

// NewSocketPath generates a path to a socket file (without actually creating the file itself) that can be used with the
// job api.
// These files are located in a subdirectory of the home dir (`~/.buildkite-agent/sockets/job-api` on unix machines),
// and are named after the PID of the process that runs this function, with a random number appended to the end.
//
// We use the home directory because we want to have strong file permissioning on the sockets - the traditional place to
// put sockets is in /run or /var/run, but these directories require the user to be root to create files in them, which
// isn't something that we can guarantee (or recommend) that users do with the agent.
//
// The other option is to use /tmp, but this is not a great option because files in /tmp are world-writable by default,
// and we don't want other users to be snooping on our socket if we can avoid it
func NewSocketPath() (string, error) {
	home, err := homedir.Dir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}

	path := filepath.Join(home, ".buildkite-agent", "sockets", "job-api")
	err = os.MkdirAll(path, 0700)
	if err != nil {
		return "", fmt.Errorf("creating socket directory: %w", err)
	}

	sockNum := random.Int63() % 100_000
	return filepath.Join(path, fmt.Sprintf("%d-%d.sock", os.Getpid(), sockNum)), nil
}
