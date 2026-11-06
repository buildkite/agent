package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/reapi"
	"github.com/buildkite/agent/v4/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if len(os.Args) != 3 || (os.Args[2] != "miss" && os.Args[2] != "hit") {
		return fmt.Errorf("usage: reapi-once-demo /path/to/once miss|hit")
	}
	if os.Getenv("BUILDKITE_AGENT_ACCESS_TOKEN") == "" || os.Getenv("BUILDKITE_BUILD_ID") == "" {
		return fmt.Errorf("a real Buildkite job access token and build ID are required")
	}
	if !strings.HasPrefix(os.Getenv("BUILDKITE_AGENT_CACHE_STORE_URL"), "s3://") {
		return fmt.Errorf("this demo requires the configured real S3 backing store")
	}
	client := api.NewClient(logger.Discard, api.Config{
		Endpoint: os.Getenv("BUILDKITE_AGENT_ENDPOINT"),
		Token:    os.Getenv("BUILDKITE_AGENT_ACCESS_TOKEN"),
	})
	registry := os.Getenv("BUILDKITE_AGENT_CACHE_REGISTRY")
	info, response, err := client.CacheRegistry(ctx, registry)
	if err != nil {
		code := 0
		if response != nil && response.Response != nil {
			code = response.StatusCode
		}
		return fmt.Errorf("live registry access failed (HTTP %d); credentials omitted", code)
	}
	fmt.Printf("Live registry %s: store=%s\n", registry, info.Store)
	s, err := reapi.Listen("127.0.0.1:0")
	if err != nil {
		return err
	}
	defer s.Close()
	token := reapi.NewToken()
	unregister := s.Register(ctx, token, &reapi.Backend{
		API:       client,
		Registry:  registry,
		BucketURL: os.Getenv("BUILDKITE_AGENT_CACHE_STORE_URL"),
	})
	defer unregister()
	fmt.Printf("PR REAPI server: %s (job-local companion; not agent-start lifecycle proof)\n", s.Endpoint())
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	workspace := filepath.Join(root, "once-live-workspace")
	if err := os.RemoveAll(workspace); err != nil {
		return err
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	files := map[string]string{
		"once.toml": fmt.Sprintf(`[infrastructure.cache]
provider = "tuist"
project = "reapi-live-%s"

[infrastructures.tuist]
kind = "tuist"
account = "buildkite"
url = "http://127.0.0.1:9"
`, os.Getenv("BUILDKITE_BUILD_ID")),
		"build.sh": `#!/bin/sh
# once input "input.c"
# once output "output"
echo executed >> executions
cc input.c -o output
echo build-stdout
echo build-stderr >&2
`,
		"input.c": "#include <stdio.h>\nint main(void) { puts(\"restored executable from live Buildkite Cache\"); return 0; }\n",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(data), 0o700); err != nil {
			return err
		}
	}
	local, err := os.MkdirTemp("", "once-independent-local-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(local)
	cmd := exec.CommandContext(ctx, os.Args[1], "--format", "json", "exec", "--script", "/bin/sh", "build.sh")
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(),
		"TUIST_CACHE_ENDPOINT="+s.Endpoint(),
		"TUIST_TOKEN="+token,
		"XDG_CACHE_HOME="+filepath.Join(local, "cache"),
		"XDG_CONFIG_HOME="+filepath.Join(local, "config"),
		"XDG_DATA_HOME="+filepath.Join(local, "data"),
	)
	fmt.Printf("Empty workspace output/.once and fresh XDG roots: %s\n", local)
	output, err := cmd.CombinedOutput()
	fmt.Print(string(output))
	if err != nil {
		return fmt.Errorf("Once failed: %w", err)
	}
	if bytes.Contains(output, []byte("WARN")) || !bytes.Contains(output, []byte(`"cache":"`+os.Args[2]+`"`)) {
		return fmt.Errorf("Once did not report the expected warning-free %s", os.Args[2])
	}
	marker, markerErr := os.ReadFile(filepath.Join(workspace, "executions"))
	if os.Args[2] == "miss" && (markerErr != nil || string(marker) != "executed\n") {
		return fmt.Errorf("initial build did not execute exactly once")
	}
	if os.Args[2] == "hit" && !os.IsNotExist(markerErr) {
		return fmt.Errorf("remote-hit build executed the command locally")
	}
	data, err := os.ReadFile(filepath.Join(workspace, "output"))
	if err != nil {
		return err
	}
	sum := fmt.Sprintf("%x\n", sha256.Sum256(data))
	proof := filepath.Join(root, "once-live-proof", "published.sha256")
	if os.Args[2] == "miss" {
		if err := os.WriteFile(proof, []byte(sum), 0o600); err != nil {
			return err
		}
	} else {
		original, err := os.ReadFile(proof)
		if err != nil || string(original) != sum {
			return fmt.Errorf("restored output differs from the publishing job")
		}
	}
	result, err := exec.CommandContext(ctx, filepath.Join(workspace, "output")).CombinedOutput()
	if err != nil || string(result) != "restored executable from live Buildkite Cache\n" {
		return fmt.Errorf("output executable verification failed")
	}
	fmt.Printf("Verified output: %d bytes, SHA256 %s%s", len(data), sum, result)
	if os.Args[2] == "hit" {
		fmt.Println("VERIFIED: cross-job remote hit; command did not execute; identical executable restored and ran")
	}
	return nil
}
