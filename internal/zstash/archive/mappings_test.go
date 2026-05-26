package archive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPathsToMappings_AbsolutePathUnderHome(t *testing.T) {
	assert := require.New(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	paths := []string{
		filepath.Join(home, ".go-build"),
		filepath.Join(home, "go", "pkg", "mod"),
	}

	mappings, err := PathsToMappings(paths)
	assert.NoError(err)
	assert.Len(mappings, 2)

	assert.Equal(".go-build", mappings[0].RelativePath)
	assert.Equal(filepath.Join(home, ".go-build"), mappings[0].ResolvedPath)
	assert.False(mappings[0].Relative)

	assert.Equal(filepath.Join("go", "pkg", "mod"), mappings[1].RelativePath)
	assert.Equal(filepath.Join(home, "go", "pkg", "mod"), mappings[1].ResolvedPath)
	assert.False(mappings[1].Relative)
}

func TestResolveHomeDir(t *testing.T) {
	assert := require.New(t)

	homeDir, err := os.UserHomeDir()
	assert.NoError(err)

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
				assert.Error(err)
				return
			}
			assert.NoError(err)
			assert.Equal(tt.expected, result)
		})
	}
}
