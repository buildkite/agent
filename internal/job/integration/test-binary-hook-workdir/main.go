package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/buildkite/agent/v4/jobapi"
)

// This file gets built and used as part of the hooks integration test to ensure
// that an unwrapped (binary) hook can change the working directory of subsequent
// phases via the Job API /workdir endpoint.
//
// It creates a subdirectory of its own working directory (the checkout dir),
// resolves it to an absolute, symlink-free path, then asks the executor to use
// it as the working directory for subsequent hooks and the command. It also
// records the expected directory in EXPECTED_WORKDIR so the test's command and
// post-command hooks can assert they run there.
func main() {
	ctx := context.Background()

	c, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		log.Fatalf("error: %v", fmt.Errorf("creating job api client: %w", err))
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("error: %v", fmt.Errorf("getting working directory: %w", err))
	}

	target := filepath.Join(cwd, "binary-hook-subdir")
	if err := os.MkdirAll(target, 0o777); err != nil {
		log.Fatalf("error: %v", fmt.Errorf("creating target directory: %w", err))
	}

	// Resolve symlinks so the path matches what the command hook observes as its
	// working directory (on macOS /tmp and /var are symlinks).
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		log.Fatalf("error: %v", fmt.Errorf("resolving target directory: %w", err))
	}

	if _, err := c.SetWorkdir(ctx, resolved); err != nil {
		log.Fatalf("error: %v", fmt.Errorf("setting workdir: %w", err))
	}

	if _, err := c.EnvUpdate(ctx, &jobapi.EnvUpdateRequest{
		Env: map[string]string{"EXPECTED_WORKDIR": resolved},
	}); err != nil {
		log.Fatalf("error: %v", fmt.Errorf("updating env: %w", err))
	}

	fmt.Println("hi there from the workdir-setting binary hook 📂")
}
