package archive

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsToMappings_AbsolutePathUnderHome(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	paths := []string{
		filepath.Join(home, ".go-build"),
		filepath.Join(home, "go", "pkg", "mod"),
	}

	mappings, err := PathsToMappings(paths)
	if err != nil {
		t.Fatalf("PathsToMappings: %v", err)
	}
	if len(mappings) != 2 {
		t.Fatalf("len(mappings) = %d, want 2", len(mappings))
	}

	if got, want := mappings[0].RelativePath, ".go-build"; got != want {
		t.Errorf("mappings[0].RelativePath = %v, want %v", got, want)
	}
	if got, want := mappings[0].ResolvedPath, filepath.Join(home, ".go-build"); got != want {
		t.Errorf("mappings[0].ResolvedPath = %v, want %v", got, want)
	}
	if mappings[0].Relative {
		t.Errorf("mappings[0].Relative = true, want false")
	}

	if got, want := mappings[1].RelativePath, filepath.Join("go", "pkg", "mod"); got != want {
		t.Errorf("mappings[1].RelativePath = %v, want %v", got, want)
	}
	if got, want := mappings[1].ResolvedPath, filepath.Join(home, "go", "pkg", "mod"); got != want {
		t.Errorf("mappings[1].ResolvedPath = %v, want %v", got, want)
	}
	if mappings[1].Relative {
		t.Errorf("mappings[1].Relative = true, want false")
	}
}

func TestResolveHomeDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		expected string
		wantErr  bool
	}{
		{
			name:     "path with tilde prefix",
			path:     "~/documents/test.txt",
			expected: filepath.Join(homeDir, "documents/test.txt"),
			wantErr:  false,
		},
		{
			name:     "path without tilde",
			path:     "/absolute/path/test.txt",
			expected: "/absolute/path/test.txt",
			wantErr:  false,
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
			wantErr:  false,
		},
		{
			name:     "tilde only",
			path:     "~",
			expected: "~",
			wantErr:  false,
		},
		{
			name:     "relative path",
			path:     "./test.txt",
			expected: "./test.txt",
			wantErr:  false,
		},
		{
			name:     "path with multiple tildes",
			path:     "~/path/~/test.txt",
			expected: filepath.Join(homeDir, "path/~/test.txt"),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveHomeDir(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveHomeDir: %v", err)
			}
			if result != tt.expected {
				t.Errorf("ResolveHomeDir() = %v, want %v", result, tt.expected)
			}
		})
	}
}
