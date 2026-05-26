package cache

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheValidate(t *testing.T) {
	tests := []struct {
		name    string
		cache   Cache
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid cache",
			cache: Cache{
				ID:           "node_modules",
				Key:          "v1-{{checksum 'package.json'}}",
				FallbackKeys: []string{"v1-fallback", "v1-node-backup"},
				Paths:        []string{"node_modules"},
			},
			wantErr: false,
		},
		{
			name: "valid cache with underscores and numbers",
			cache: Cache{
				ID:    "test_123",
				Key:   "build-key",
				Paths: []string{"./dist", "../cache"},
			},
			wantErr: false,
		},
		{
			name: "invalid ID with hyphen",
			cache: Cache{
				ID:    "node-modules",
				Key:   "valid-key",
				Paths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "can only contain letters, numbers, and underscores",
		},
		{
			name: "invalid ID with space",
			cache: Cache{
				ID:    "node modules",
				Key:   "valid-key",
				Paths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "can only contain letters, numbers, and underscores",
		},
		{
			name: "empty ID",
			cache: Cache{
				ID:    "",
				Key:   "valid-key",
				Paths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "id cannot be empty",
		},
		{
			name: "whitespace only ID",
			cache: Cache{
				ID:    "   ",
				Key:   "valid-key",
				Paths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "id cannot be empty",
		},
		{
			name: "empty key",
			cache: Cache{
				ID:    "valid_id",
				Key:   "",
				Paths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "key cannot be empty",
		},
		{
			name: "whitespace only key",
			cache: Cache{
				ID:    "valid_id",
				Key:   "   ",
				Paths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "key cannot be empty",
		},
		{
			name: "fallback key with space",
			cache: Cache{
				ID:           "valid_id",
				Key:          "valid-key",
				FallbackKeys: []string{"invalid key"},
				Paths:        []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "cannot contain spaces",
		},
		{
			name: "empty fallback key",
			cache: Cache{
				ID:           "valid_id",
				Key:          "valid-key",
				FallbackKeys: []string{""},
				Paths:        []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "fallback key at index 0 cannot be empty",
		},
		{
			name: "no paths",
			cache: Cache{
				ID:    "valid_id",
				Key:   "valid-key",
				Paths: []string{},
			},
			wantErr: true,
			errMsg:  "at least one path must be specified",
		},
		{
			name: "empty path",
			cache: Cache{
				ID:    "valid_id",
				Key:   "valid-key",
				Paths: []string{""},
			},
			wantErr: true,
			errMsg:  "path at index 0 cannot be empty",
		},
		{
			name: "path with null byte",
			cache: Cache{
				ID:    "valid_id",
				Key:   "valid-key",
				Paths: []string{"invalid\x00path"},
			},
			wantErr: true,
			errMsg:  "path at index 0 is not valid",
		},
		{
			name: "valid fallback keys with hyphens",
			cache: Cache{
				ID:           "valid_id",
				Key:          "valid-key",
				FallbackKeys: []string{"fallback-key", "another-fallback_key"},
				Paths:        []string{"node_modules"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := require.New(t)
			err := tt.cache.Validate()
			if tt.wantErr {
				assert.Error(err, "Cache.Validate() expected error but got none")
				assert.Contains(err.Error(), tt.errMsg, "error message should contain expected text")
			} else {
				assert.NoError(err, "Cache.Validate() should not return error")
			}
		})
	}
}

func TestIsValidPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "valid relative path",
			path: "node_modules",
			want: true,
		},
		{
			name: "valid path with dots",
			path: "./dist",
			want: true,
		},
		{
			name: "valid path with parent directory",
			path: "../cache",
			want: true,
		},
		{
			name: "empty path",
			path: "",
			want: false,
		},
		{
			name: "whitespace only path",
			path: "   ",
			want: false,
		},
		{
			name: "path with null byte",
			path: "invalid\x00path",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := require.New(t)
			got := isValidPath(tt.path)
			assert.Equal(tt.want, got, "isValidPath() should return expected result")
		})
	}
}
