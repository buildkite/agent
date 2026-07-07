package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNscStore_Interface(t *testing.T) {
	// This test ensures that NscStore properly implements the Blob interface
	var _ Blob = (*NscStore)(nil)
}

func TestValidateFilePath(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple path",
			filePath:    "test.txt",
			expectError: false,
		},
		{
			name:        "valid relative path",
			filePath:    "dir/subdir/file.txt",
			expectError: false,
		},
		{
			name:        "valid absolute path",
			filePath:    "/tmp/test.txt",
			expectError: false,
		},
		{
			name:        "empty path",
			filePath:    "",
			expectError: true,
			errorMsg:    "file path cannot be empty",
		},
		{
			name:        "path with semicolon",
			filePath:    "file;rm -rf /",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: ;",
		},
		{
			name:        "path with ampersand",
			filePath:    "file&malicious",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: &",
		},
		{
			name:        "path with pipe",
			filePath:    "file|cat /etc/passwd",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: |",
		},
		{
			name:        "path with backtick",
			filePath:    "file`whoami`",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: `",
		},
		{
			name:        "path with dollar sign",
			filePath:    "file$(whoami)",
			expectError: true,
			errorMsg:    "file path contains potentially dangerous character: $",
		},
		{
			name:        "path traversal attempt",
			filePath:    "../../../etc/passwd",
			expectError: true,
			errorMsg:    "file path contains path traversal sequence",
		},
		{
			name:        "path with quotes",
			filePath:    `file"test"`,
			expectError: true,
			errorMsg:    `file path contains potentially dangerous character: "`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFilePath(tt.filePath)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("validateFilePath: %v", err)
				}
			}
		})
	}
}

func TestValidateKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple key",
			key:         "mykey",
			expectError: false,
		},
		{
			name:        "valid key with path",
			key:         "builds/123/artifacts/report.txt",
			expectError: false,
		},
		{
			name:        "valid key with dots and underscores",
			key:         "test_file.tar.gz",
			expectError: false,
		},
		{
			name:        "valid key with hyphens",
			key:         "build-artifact-v1.0.0",
			expectError: false,
		},
		{
			name:        "empty key",
			key:         "",
			expectError: true,
			errorMsg:    "key cannot be empty",
		},
		{
			name:        "key too long",
			key:         string(make([]byte, 257)), // 257 characters
			expectError: true,
			errorMsg:    "key too long (max 256 characters)",
		},
		{
			name:        "key with invalid characters",
			key:         "key with spaces",
			expectError: true,
			errorMsg:    "key contains invalid characters",
		},
		{
			name:        "key with special characters",
			key:         "key@domain.com",
			expectError: true,
			errorMsg:    "key contains invalid characters",
		},
		{
			name:        "key with path traversal",
			key:         "../secret",
			expectError: true,
			errorMsg:    "key contains potentially dangerous pattern: ../",
		},
		{
			name:        "key with command injection attempt",
			key:         "file&&rm-rf",
			expectError: true,
			errorMsg:    "key contains invalid characters", // regex catches it first
		},
		{
			name:        "key with backtick",
			key:         "file`whoami`",
			expectError: true,
			errorMsg:    "key contains invalid characters", // regex catches it first
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateKey(tt.key)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Fatalf("validateKey: %v", err)
				}
			}
		})
	}
}

func TestRunCommandValidation(t *testing.T) {
	ctx := t.Context()

	// Test empty args
	result, err := runCommand(ctx, "" /* no args */)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no command provided") {
		t.Errorf("error %q does not contain %q", err.Error(), "no command provided")
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestParseNscNamespace(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		namespace string
		wantErr   bool
	}{
		{name: "nsc with namespace", url: "nsc://my-namespace", namespace: "my-namespace"},
		{name: "not nsc", url: "s3://my-bucket", wantErr: true},
		{name: "nsc without namespace", url: "nsc://", wantErr: true},
		{name: "invalid url", url: "nsc://host:notaport", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, err := parseNscNamespace(tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseNscNamespace(%q) err = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if namespace != tt.namespace {
				t.Errorf("parseNscNamespace(%q) namespace = %q, want %q", tt.url, namespace, tt.namespace)
			}
		})
	}
}

// fakeRunner records the args of the last command and returns a successful result.
func fakeRunner(captured *[]string) commandRunner {
	return func(_ context.Context, _ string, args ...string) (*CommandResult, error) {
		*captured = args
		return &CommandResult{}, nil
	}
}

func TestNscStore_PassesNamespace(t *testing.T) {
	ctx := t.Context()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var captured []string
	store := &NscStore{namespace: "my-namespace", run: fakeRunner(&captured)}

	if _, err := store.Upload(ctx, testFile, "key"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	wantArgs := []string{"nsc", "artifact", "upload", testFile, "key", "--expires_in", "24h", "--namespace", "my-namespace"}
	if diff := cmp.Diff(wantArgs, captured); diff != "" {
		t.Errorf("upload args mismatch (-want +got):\n%s", diff)
	}
}

func TestNewNscStore_RequiresNamespace(t *testing.T) {
	if _, err := NewNscStore("nsc://"); err == nil {
		t.Error(`NewNscStore("nsc://"): expected error, got nil`)
	}
}

// TestNscStore_ValidationShortCircuits ensures unsafe inputs are rejected before
// the CLI is ever invoked.
func TestNscStore_ValidationShortCircuits(t *testing.T) {
	ctx := t.Context()
	ran := false
	store := &NscStore{namespace: "ns", run: func(context.Context, string, ...string) (*CommandResult, error) {
		ran = true
		return &CommandResult{}, nil
	}}

	if _, err := store.Upload(ctx, "invalid;path", "valid-key"); err == nil {
		t.Error("Upload with unsafe path: expected error, got nil")
	}
	if _, err := store.Download(ctx, "invalid key with spaces", "dest.txt"); err == nil {
		t.Error("Download with unsafe key: expected error, got nil")
	}
	if ran {
		t.Error("nsc CLI should not run when input validation fails")
	}
}

func TestNewBlobStore_NscScheme(t *testing.T) {
	blob, err := NewBlobStore(t.Context(), AgentManaged, "nsc://my-namespace")
	if err != nil {
		t.Fatalf("NewBlobStore: %v", err)
	}
	nsc, ok := blob.(*NscStore)
	if !ok {
		t.Fatalf("NewBlobStore returned %T, want *NscStore", blob)
	}
	if nsc.namespace != "my-namespace" {
		t.Errorf("namespace = %q, want %q", nsc.namespace, "my-namespace")
	}
}

// TestNscStore_Integration runs integration tests if NSC CLI is available
// This test can be skipped if NSC is not installed or configured
func TestNscStore_Integration(t *testing.T) {
	// Skip this test if NSC_INTEGRATION_TEST environment variable is not set
	if os.Getenv("NSC_INTEGRATION_TEST") == "" {
		t.Skip("Skipping NSC integration test (set NSC_INTEGRATION_TEST=1 to run)")
	}

	// "main" is the nsc CLI's default namespace.
	store, err := NewNscStore("nsc://main")
	if err != nil {
		t.Fatalf("NewNscStore: %v", err)
	}

	ctx := t.Context()

	// Create temporary directories and files
	tmpDir, err := os.MkdirTemp("", "nsc-integration-test")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test-upload.txt")
	testContent := "Hello from NSC integration test!"
	err = os.WriteFile(testFile, []byte(testContent), 0o600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Test upload
	key := "integration-test/test-file.txt"
	transferInfo, err := store.Upload(ctx, testFile, key)
	if err != nil {
		t.Fatalf("Upload should succeed with valid NSC setup: %v", err)
	}

	if transferInfo.BytesTransferred <= 0 {
		t.Errorf("expected BytesTransferred > 0, got %d", transferInfo.BytesTransferred)
	}
	if transferInfo.TransferSpeed <= 0.0 {
		t.Errorf("expected TransferSpeed > 0, got %f", transferInfo.TransferSpeed)
	}
	if transferInfo.Duration <= 0 {
		t.Errorf("expected Duration > 0, got %v", transferInfo.Duration)
	}

	// Test download
	downloadFile := filepath.Join(tmpDir, "test-download.txt")
	transferInfo, err = store.Download(ctx, key, downloadFile)
	if err != nil {
		t.Fatalf("Download should succeed: %v", err)
	}

	if transferInfo.BytesTransferred <= 0 {
		t.Errorf("expected BytesTransferred > 0, got %d", transferInfo.BytesTransferred)
	}

	// Verify downloaded content
	downloadedContent, err := os.ReadFile(downloadFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(downloadedContent) != testContent {
		t.Errorf("downloaded content mismatch: got %q, want %q", string(downloadedContent), testContent)
	}

	t.Logf("Upload: %d bytes at %.2f MB/s in %v",
		transferInfo.BytesTransferred,
		transferInfo.TransferSpeed,
		transferInfo.Duration)
}

// TestNscStore_DownloadNotFound checks the store-specific not-found mapping:
// a "not found" CLI failure surfaces as store.ErrBlobNotFound (so restore can
// invalidate the stale entry), while other failures stay generic errors.
func TestNscStore_DownloadNotFound(t *testing.T) {
	ctx := t.Context()
	dest := filepath.Join(t.TempDir(), "dest")

	t.Run("stderr not found maps to ErrBlobNotFound", func(t *testing.T) {
		store := &NscStore{namespace: "ns", run: func(context.Context, string, ...string) (*CommandResult, error) {
			return &CommandResult{ExitCode: 1, Stderr: "Error: artifact not found"}, nil
		}}
		_, err := store.Download(ctx, "valid-key", dest)
		if !errors.Is(err, ErrBlobNotFound) {
			t.Fatalf("Download err = %v, want ErrBlobNotFound", err)
		}
	})

	t.Run("other failures are not ErrBlobNotFound", func(t *testing.T) {
		store := &NscStore{namespace: "ns", run: func(context.Context, string, ...string) (*CommandResult, error) {
			return &CommandResult{ExitCode: 1, Stderr: "connection refused"}, nil
		}}
		_, err := store.Download(ctx, "valid-key", dest)
		if err == nil {
			t.Fatal("Download: expected error, got nil")
		}
		if errors.Is(err, ErrBlobNotFound) {
			t.Errorf("Download err = %v, should not be ErrBlobNotFound", err)
		}
	})
}
