package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/cache/internal/trace"
	"go.opentelemetry.io/otel/attribute"
)

// NscStore implements the Blob interface for NSC artifact storage which uses the nsc CLI tool
// https://namespace.so/docs/reference/cli/artifact-download
// https://namespace.so/docs/reference/cli/artifact-upload
type NscStore struct{}

func NewNscStore() (*NscStore, error) {
	return &NscStore{}, nil
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
	result, err := runCommand(ctx, "", "nsc", "artifact", "upload", filePath, key)
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
	result, err := runCommand(ctx, "", "nsc", "artifact", "download", key, filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to execute nsc download command: %w", err)
	}

	if result.ExitCode != 0 {
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

	return &TransferInfo{
		BytesTransferred: bytesTransferred,
		TransferSpeed:    averageSpeed,
		RequestID:        "", // NSC doesn't expose request IDs
		Duration:         duration,
	}, nil
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
	cmd := exec.Command(args[0], args[1:]...)
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
