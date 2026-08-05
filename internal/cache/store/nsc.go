package store

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/cache/internal/trace"
	"go.opentelemetry.io/otel/attribute"
)

// nscScheme is the URL scheme that routes an agent-managed cache store to NSC.
const nscScheme = "nsc"

// nscDefaultExpiry is the artifact lifetime used both when uploading a cache
// entry (--expires_in) and when refreshing it on access (--ensure_minimum).
// Cache entries are content-addressed and short-lived, so we cap storage growth
// rather than relying on NSC's no-expiry default. Every restore pushes the
// expiry back out to this duration from now, keeping hot caches alive while
// letting cold ones expire.
const nscDefaultExpiry = "24h"

// commandRunner executes an external command. It is a seam so tests can assert
// the arguments passed to the nsc CLI without invoking the real binary.
type commandRunner func(ctx context.Context, workingDir string, args ...string) (*CommandResult, error)

// NscStore implements the Blob interface for NSC artifact storage which uses the nsc CLI tool
// https://namespace.so/docs/reference/cli/artifact-download
// https://namespace.so/docs/reference/cli/artifact-upload
type NscStore struct {
	// namespace is taken from the nsc://<namespace> cache store URL and passed
	// as --namespace to the nsc CLI. It is always non-empty.
	namespace string
	run       commandRunner
}

// NewNscStore creates a store backed by the nsc CLI. The namespace is parsed
// from the nsc://<namespace> cache store URL and is required.
func NewNscStore(bucketURL string) (*NscStore, error) {
	namespace, err := parseNscNamespace(bucketURL)
	if err != nil {
		return nil, err
	}
	return &NscStore{namespace: namespace, run: runCommand}, nil
}

// parseNscNamespace extracts the namespace from an nsc://<namespace> cache store
// URL. It errors on a non-nsc URL or a missing namespace.
func parseNscNamespace(bucketURL string) (string, error) {
	u, err := url.Parse(bucketURL)
	if err != nil {
		return "", fmt.Errorf("invalid cache store URL %q: %w", bucketURL, err)
	}
	if u.Scheme != nscScheme {
		return "", fmt.Errorf("expected %s:// cache store URL, got %q", nscScheme, bucketURL)
	}
	if u.Host == "" {
		return "", fmt.Errorf("nsc:// URL must include a namespace, e.g. nsc://my-namespace")
	}
	return u.Host, nil
}

// artifactArgs builds the nsc CLI argument list, including --namespace.
func (n *NscStore) artifactArgs(args ...string) []string {
	cmd := append([]string{"nsc", "artifact"}, args...)
	return append(cmd, "--namespace", n.namespace)
}

// validateFilePath validates that a file path is safe for use in commands
func validateFilePath(filePath string) error {
	if filePath == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	// Clean the path to normalize it
	cleanPath := filepath.Clean(filePath)

	// Check for potentially dangerous characters that could be used for command injection.
	// Backslash is the path separator on Windows so it must be allowed there.
	dangerousChars := []string{";", "&", "|", "`", "$", "(", ")", "{", "}", "[", "]", "<", ">", "\"", "'"}
	if runtime.GOOS != "windows" {
		dangerousChars = append(dangerousChars, "\\")
	}
	for _, char := range dangerousChars {
		if strings.Contains(cleanPath, char) {
			return fmt.Errorf("file path contains potentially dangerous character: %s", char)
		}
	}

	// Check for path traversal attempts
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("file path contains path traversal sequence")
	}

	return nil
}

// validateKey validates that an artifact key is safe for use in commands
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}

	// Check length - reasonable limit for artifact keys
	if len(key) > 256 {
		return fmt.Errorf("key too long (max 256 characters)")
	}

	// NSC artifact keys should be alphanumeric with some safe special characters
	// Allow: alphanumeric, hyphens, underscores, dots, forward slashes
	validKeyPattern := regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
	if !validKeyPattern.MatchString(key) {
		return fmt.Errorf("key contains invalid characters (only alphanumeric, ., _, /, - are allowed)")
	}

	// Check for potentially dangerous patterns
	dangerousPatterns := []string{"../", "./", "//", "&&", "||", ";", "`", "$"}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(key, pattern) {
			return fmt.Errorf("key contains potentially dangerous pattern: %s", pattern)
		}
	}

	return nil
}

func (n *NscStore) Upload(ctx context.Context, filePath, key string) (*TransferInfo, error) {
	_, span := trace.Start(ctx, "NscStore.Upload")
	defer span.End()

	// Validate input parameters to prevent command injection
	if err := validateFilePath(filePath); err != nil {
		return nil, fmt.Errorf("invalid file path: %w", err)
	}
	if err := validateKey(key); err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	start := time.Now()

	// Execute nsc artifact upload command
	result, err := n.run(ctx, "", n.artifactArgs("upload", filePath, key, "--expires_in", nscDefaultExpiry)...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute nsc upload command: %w", err)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("nsc upload failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	// Get file size for transfer info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	duration := time.Since(start)
	bytesTransferred := fileInfo.Size()
	averageSpeed := calculateTransferSpeedMBps(bytesTransferred, duration)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesTransferred),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.String("nsc_key", key),
	)

	return &TransferInfo{
		BytesTransferred: bytesTransferred,
		TransferSpeed:    averageSpeed,
		RequestID:        "", // NSC doesn't expose request IDs
		Duration:         duration,
	}, nil
}

func (n *NscStore) Download(ctx context.Context, key, filePath string) (*TransferInfo, error) {
	_, span := trace.Start(ctx, "NscStore.Download")
	defer span.End()

	// Validate input parameters to prevent command injection
	if err := validateKey(key); err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}
	if err := validateFilePath(filePath); err != nil {
		return nil, fmt.Errorf("invalid file path: %w", err)
	}

	start := time.Now()

	// Execute nsc artifact download command
	result, err := n.run(ctx, "", n.artifactArgs("download", key, filePath)...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute nsc download command: %w", err)
	}

	if result.ExitCode != 0 {
		// Map a missing or expired artifact to ErrBlobNotFound so the restore
		// treats it as a cache miss (invalidating the stale entry and continuing)
		// rather than failing the build.
		if strings.Contains(result.Stderr, "not found") ||
			strings.Contains(result.Stderr, "has expired") {
			return nil, fmt.Errorf("%w: nsc key %s: %s", ErrBlobNotFound, key, strings.TrimSpace(result.Stderr))
		}
		return nil, fmt.Errorf("nsc download failed with exit code %d: %s", result.ExitCode, result.Stderr)
	}

	// Get file size for transfer info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get downloaded file info: %w", err)
	}

	duration := time.Since(start)
	bytesTransferred := fileInfo.Size()
	averageSpeed := calculateTransferSpeedMBps(bytesTransferred, duration)

	span.SetAttributes(
		attribute.Int64("bytes_transferred", bytesTransferred),
		attribute.String("transfer_speed", fmt.Sprintf("%.2fMB/s", averageSpeed)),
		attribute.String("nsc_key", key),
	)

	// Refresh the artifact's TTL on access so hot caches stay alive, mirroring
	// the S3 store's self-CopyObject refresh. Unlike CopyObject this is cheap,
	// so we refresh on every restore rather than gating it behind a minimum
	// interval.
	n.refreshExpiry(ctx, key)

	return &TransferInfo{
		BytesTransferred: bytesTransferred,
		TransferSpeed:    averageSpeed,
		RequestID:        "", // NSC doesn't expose request IDs
		Duration:         duration,
	}, nil
}

// refreshExpiry pushes the artifact's expiry out to at least nscDefaultExpiry
// from now via `nsc artifact extend --ensure_minimum`. Using --ensure_minimum
// (rather than the additive --by) makes the refresh idempotent, so calling it
// on every restore keeps a hot cache alive without growing its expiry unbounded.
//
// This is best-effort: any failure is logged and swallowed so a restore never
// fails because its TTL could not be refreshed.
func (n *NscStore) refreshExpiry(ctx context.Context, key string) {
	if !n.extendSupported(ctx) {
		// `nsc artifact extend` is still being rolled out by Namespace. Update
		// the nsc CLI as a contingency so the command becomes available.
		slog.Debug("nsc artifact extend unavailable, updating nsc CLI")
		if _, err := n.run(ctx, "", "nsc", "version", "update"); err != nil {
			slog.Warn("failed to update nsc CLI, skipping cache TTL refresh (non-fatal)",
				"key", key, "error", err)
			return
		}
	}

	result, err := n.run(ctx, "", n.artifactArgs("extend", key, "--ensure_minimum", nscDefaultExpiry)...)
	switch {
	case err != nil:
		slog.Warn("failed to refresh cache TTL, continuing (non-fatal)", "key", key, "error", err)
	case result.ExitCode != 0:
		slog.Warn("failed to refresh cache TTL, continuing (non-fatal)",
			"key", key, "exit_code", result.ExitCode, "stderr", result.Stderr)
	default:
		slog.Debug("refreshed cache TTL", "key", key)
	}
}

// extendSupported reports whether the installed nsc CLI supports
// `nsc artifact extend`. During Namespace's rollout of this command, older CLIs
// exit non-zero for its --help.
func (n *NscStore) extendSupported(ctx context.Context) bool {
	result, err := n.run(ctx, "", "nsc", "artifact", "extend", "--help")
	return err == nil && result.ExitCode == 0 &&
		strings.Contains(result.Stdout, "--ensure_minimum")
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func runCommand(ctx context.Context, workingDir string, args ...string) (*CommandResult, error) {
	_, span := trace.Start(ctx, "runCommand")
	defer span.End()

	// Validate that args is not empty to prevent panic
	if len(args) == 0 {
		return nil, fmt.Errorf("no command provided")
	}

	span.SetAttributes(attribute.StringSlice("command", args))

	cr := &CommandResult{}

	// #nosec G204 - args are validated by callers (validateFilePath, validateKey)
	// and this function is internal to the store package with controlled usage
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ() // inherit the environment

	if workingDir != "" {
		cmd.Dir = workingDir
	}

	err := cmd.Run()
	if err != nil {
		span.RecordError(err)
		if exitError, ok := err.(*exec.ExitError); ok {
			cr.ExitCode = exitError.ExitCode()
		} else {
			return nil, err
		}
	}

	cr.Stdout = stdout.String()
	cr.Stderr = stderr.String()

	return cr, nil
}
