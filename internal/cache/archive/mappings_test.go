package archive

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestPathsToMappings_AbsolutePathOutsideHomeRejected(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	outside := filepath.Join(t.TempDir(), "opt", "cache")

	_, err := PathsToMappings([]string{outside})
	if err == nil {
		t.Fatal("PathsToMappings: expected error for absolute path outside home, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error %q should contain %q", err.Error(), "not supported")
	}
}

func TestPathsToMappings_ParentRelativeRejected(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	for _, path := range []string{"..", filepath.Join("..", "cache")} {
		_, err := PathsToMappings([]string{path})
		if err == nil {
			t.Errorf("PathsToMappings(%q): expected error for parent-relative path, got nil", path)
			continue
		}
		if !strings.Contains(err.Error(), "escapes the working directory") {
			t.Errorf("error %q should contain %q", err.Error(), "escapes the working directory")
		}
	}
}

func TestPathsToMappings_HomeEscapeRejected(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	_, err := PathsToMappings([]string{"~/../shared/cache"})
	if err == nil {
		t.Fatal("PathsToMappings: expected error for path escaping home, got nil")
	}
	if !strings.Contains(err.Error(), "escapes the home directory") {
		t.Errorf("error %q should contain %q", err.Error(), "escapes the home directory")
	}
}

func TestPathsToMappings_RootedPathsOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only relevant on Windows")
	}

	home := t.TempDir()
	setHomeDir(t, home)

	for _, path := range []string{`\etc`, "/etc", "C:cache"} {
		_, err := PathsToMappings([]string{path})
		if err == nil {
			t.Errorf("PathsToMappings(%q): expected error for rooted path on Windows, got nil", path)
		}
	}
}

func TestPathsToMappings_SiblingOfHomeIsNotHomeRelative(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "user")
	setHomeDir(t, home)

	// A sibling directory sharing the home path as a string prefix must not
	// be treated as home-relative: it is outside home and must be rejected.
	sibling := filepath.Join(base, "user2", "cache")

	_, err := PathsToMappings([]string{sibling})
	if err == nil {
		t.Fatal("PathsToMappings: expected error for sibling of home, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error %q should contain %q", err.Error(), "not supported")
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
