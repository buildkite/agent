package archive

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathsToMappings(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	// "/opt/cache" is only absolute on POSIX; on Windows an absolute path needs
	// a volume, so build one from the temp dir's volume.
	absPath := "/opt/cache"
	if runtime.GOOS == "windows" {
		absPath = filepath.VolumeName(home) + `\opt\cache`
	}

	paths := []string{
		"~/.npm",
		absPath,
		"node_modules",
		filepath.Join(home, ".go-build"),
	}

	mappings, err := PathsToMappings(paths)
	if err != nil {
		t.Fatalf("PathsToMappings: %v", err)
	}
	if len(mappings) != 4 {
		t.Fatalf("len(mappings) = %d, want 4", len(mappings))
	}

	tests := []struct {
		namespace    string
		anchor       string
		resolvedPath string
	}{
		{"_0", AnchorHome, filepath.Join(home, ".npm")},
		{"_1", volumeRoot(filepath.Clean(absPath)), filepath.Clean(absPath)},
		{"_2", AnchorCWD, filepath.Join(cwd, "node_modules")},
		// An absolute path under $HOME is pinned (root-anchored), not portable:
		// only a leading "~" is portable per A-1584.
		{"_3", volumeRoot(filepath.Join(home, ".go-build")), filepath.Join(home, ".go-build")},
	}

	for i, want := range tests {
		got := mappings[i]
		if got.Namespace != want.namespace {
			t.Errorf("mappings[%d].Namespace = %q, want %q", i, got.Namespace, want.namespace)
		}
		if got.Anchor != want.anchor {
			t.Errorf("mappings[%d].Anchor = %q, want %q", i, got.Anchor, want.anchor)
		}
		if got.ResolvedPath != want.resolvedPath {
			t.Errorf("mappings[%d].ResolvedPath = %q, want %q", i, got.ResolvedPath, want.resolvedPath)
		}
	}
}

// TestResolveConfigPath pins the resolver used by both restore matching and
// cleanup. The bare-"~" case: it must resolve to $HOME
// (consistent with how save classifies it), not stay a literal "~".
func TestResolveConfigPath(t *testing.T) {
	home := t.TempDir()
	setHomeDir(t, home)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"bare tilde resolves to home", "~", home},
		{"tilde prefix", "~/documents/test.txt", filepath.Join(home, "documents/test.txt")},
		{"absolute path", filepath.Join(home, "x"), filepath.Join(home, "x")},
		{"relative path", filepath.Join("sub", "test.txt"), filepath.Join(cwd, "sub", "test.txt")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolveConfigPath(tt.path)
			if err != nil {
				t.Fatalf("ResolveConfigPath: %v", err)
			}
			if result != tt.expected {
				t.Errorf("ResolveConfigPath(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}
