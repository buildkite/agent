package key

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTemplate(t *testing.T) {
	t.Run("basic templates", func(t *testing.T) {
		tests := []struct {
			name     string
			key      string
			expected string
		}{
			{
				name:     "simple string",
				key:      "mykey",
				expected: "mykey",
			},
			{
				name:     "string with whitespace",
				key:      "  mykey  ",
				expected: "mykey",
			},
			{
				name:     "invalid template",
				key:      "{{.InvalidField}}",
				expected: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := Template("", tt.key)
				if err != nil {
					t.Fatalf("Template: %v", err)
				}
				if got != tt.expected {
					t.Errorf("Template() = %q, want %q", got, tt.expected)
				}
			})
		}
	})

	t.Run("checksum templates", func(t *testing.T) {
		tests := []struct {
			name     string
			id       string
			key      string
			setup    func() error
			cleanup  func()
			expected string
		}{
			{
				name:     "non-existent file",
				key:      `{{checksum "non-existent-file"}}`,
				expected: "",
			},
			{
				name: "single file",
				id:   "go",
				key:  `{{ id }}-{{checksum "go.mod"}}`,
				setup: func() error {
					return os.WriteFile("go.mod", []byte("test content"), 0o600)
				},
				cleanup: func() {
					_ = os.Remove("go.mod")
				},
				expected: "go-4b9054a7a40e53c2e310fcd6f696c46c6a40dcdfa5b849785a456756ec512660",
			},
			{
				name: "file with os/arch",
				id:   "go",
				key:  `{{ id }}-{{ agent.os }}-{{ agent.arch }}-{{checksum "go.mod"}}`,
				setup: func() error {
					return os.WriteFile("go.mod", []byte("test content"), 0o600)
				},
				cleanup: func() {
					_ = os.Remove("go.mod")
				},
				expected: fmt.Sprintf("go-%s-%s-4b9054a7a40e53c2e310fcd6f696c46c6a40dcdfa5b849785a456756ec512660", runtime.GOOS, runtime.GOARCH),
			},
			{
				name: "file non-recursive (only root)",
				key:  `go-{{checksum "go.mod"}}`,
				setup: func() error {
					if err := os.Mkdir("subdir", 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join("subdir", "go.mod"), []byte("nested content"), 0o600); err != nil {
						return err
					}
					return os.WriteFile("go.mod", []byte("test content"), 0o600)
				},
				cleanup: func() {
					_ = os.Remove("go.mod")
					_ = os.RemoveAll("subdir")
				},
				expected: "go-4b9054a7a40e53c2e310fcd6f696c46c6a40dcdfa5b849785a456756ec512660",
			},
			{
				name: "file recursive (finds all)",
				key:  `go-{{checksum "**/go.mod"}}`,
				setup: func() error {
					if err := os.Mkdir("subdir", 0o755); err != nil {
						return err
					}
					if err := os.WriteFile(filepath.Join("subdir", "go.mod"), []byte("nested content"), 0o600); err != nil {
						return err
					}
					return os.WriteFile("go.mod", []byte("test content"), 0o600)
				},
				cleanup: func() {
					_ = os.Remove("go.mod")
					_ = os.RemoveAll("subdir")
				},
				expected: "go-f2684b75ab846895bcc1d50f4511edeb8fcd86167a8e6e64aeee46afc1576d9c",
			},
			{
				name: "directory recursive",
				key:  `{{checksum "**/testfile"}}`,
				setup: func() error {
					if err := os.Mkdir("testdir", 0o755); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join("testdir", "testfile"), []byte("test content"), 0o600)
				},
				cleanup: func() {
					_ = os.RemoveAll("testdir")
				},
				expected: "4b9054a7a40e53c2e310fcd6f696c46c6a40dcdfa5b849785a456756ec512660",
			},
			{
				name: "directory non-recursive (empty)",
				key:  `{{checksum "testdir"}}`,
				setup: func() error {
					if err := os.Mkdir("testdir", 0o755); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join("testdir", "testfile"), []byte("test content"), 0o600)
				},
				cleanup: func() {
					_ = os.RemoveAll("testdir")
				},
				expected: "",
			},
			{
				name: "file path non-recursive",
				key:  `{{checksum "testdir/Dockerfile.dev"}}`,
				setup: func() error {
					if err := os.Mkdir("testdir", 0o755); err != nil {
						return err
					}
					return os.WriteFile(filepath.Join("testdir", "Dockerfile.dev"), []byte("test content"), 0o600)
				},
				cleanup: func() {
					_ = os.RemoveAll("testdir")
				},
				expected: "4b9054a7a40e53c2e310fcd6f696c46c6a40dcdfa5b849785a456756ec512660",
			},
			{
				name: "glob wildcard patterns",
				key:  `{{checksum "*.mod"}}`,
				setup: func() error {
					if err := os.WriteFile("go.mod", []byte("module test"), 0o600); err != nil {
						return err
					}
					return os.WriteFile("rust.mod", []byte("mod test"), 0o600)
				},
				cleanup: func() {
					_ = os.Remove("go.mod")
					_ = os.Remove("rust.mod")
				},
				expected: "da4c35f2349831611032777269dba5b864abba9a9eabf5c0e4e5b67fb20ff52d",
			},
			{
				name: "glob brace expansion",
				key:  `{{checksum "*.{yml,yaml}"}}`,
				setup: func() error {
					if err := os.WriteFile("config.yml", []byte("test: value"), 0o600); err != nil {
						return err
					}
					return os.WriteFile("data.yaml", []byte("data: value"), 0o600)
				},
				cleanup: func() {
					_ = os.Remove("config.yml")
					_ = os.Remove("data.yaml")
				},
				expected: "2ca0044d8e7c94fa42827867d58c3ff59d7dd5a6c33baf9c075b11e8d690a336",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				tmpDir, err := os.MkdirTemp("", "zstash-test")
				if err != nil {
					t.Fatalf("MkdirTemp: %v", err)
				}
				defer func() {
					_ = os.RemoveAll(tmpDir)
				}()
				if err := os.Chdir(tmpDir); err != nil {
					t.Fatalf("Chdir: %v", err)
				}

				if tt.setup != nil {
					if err := tt.setup(); err != nil {
						t.Fatalf("setup: %v", err)
					}
				}

				if tt.cleanup != nil {
					defer tt.cleanup()
				}

				got, err := Template(tt.id, tt.key)
				if err != nil {
					t.Fatalf("Template: %v", err)
				}
				if got != tt.expected {
					t.Errorf("Template() = %q, want %q", got, tt.expected)
				}
			})
		}
	})
}
