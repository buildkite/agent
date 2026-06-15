package configuration

import (
	"strings"
	"testing"
)

func TestCacheValidate(t *testing.T) {
	literalKey := []KeyPart{{Source: SourceLiteral, Arg: "v1"}}

	tests := []struct {
		name    string
		cache   Cache
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid cache",
			cache: Cache{
				Name:        "node_modules",
				CacheKey:    literalKey,
				TargetPaths: []string{"node_modules"},
			},
			wantErr: false,
		},
		{
			name: "valid cache with underscores and numbers, multiple paths",
			cache: Cache{
				Name:        "test_123",
				CacheKey:    literalKey,
				TargetPaths: []string{"./dist", "../cache"},
			},
			wantErr: false,
		},
		{
			name: "invalid name with hyphen",
			cache: Cache{
				Name:        "node-modules",
				CacheKey:    literalKey,
				TargetPaths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "can only contain letters, numbers, and underscores",
		},
		{
			name: "invalid name with space",
			cache: Cache{
				Name:        "node modules",
				CacheKey:    literalKey,
				TargetPaths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "can only contain letters, numbers, and underscores",
		},
		{
			name: "empty Name",
			cache: Cache{
				Name:        "",
				CacheKey:    literalKey,
				TargetPaths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "name cannot be empty",
		},
		{
			name: "whitespace only ID",
			cache: Cache{
				Name:        "   ",
				CacheKey:    literalKey,
				TargetPaths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "name cannot be empty",
		},
		{
			name: "empty cache_key",
			cache: Cache{
				Name:        "valid_id",
				CacheKey:    nil,
				TargetPaths: []string{"node_modules"},
			},
			wantErr: true,
			errMsg:  "cache_key cannot be empty",
		},
		{
			name: "no target paths",
			cache: Cache{
				Name:        "valid_id",
				CacheKey:    literalKey,
				TargetPaths: []string{},
			},
			wantErr: true,
			errMsg:  "at least one target_paths entry must be specified",
		},
		{
			name: "empty target path",
			cache: Cache{
				Name:        "valid_id",
				CacheKey:    literalKey,
				TargetPaths: []string{""},
			},
			wantErr: true,
			errMsg:  "target_paths[0] cannot be empty",
		},
		{
			name: "target path with null byte",
			cache: Cache{
				Name:        "valid_id",
				CacheKey:    literalKey,
				TargetPaths: []string{"invalid\x00path"},
			},
			wantErr: true,
			errMsg:  "target_paths[0] is not valid",
		},
		{
			name: "duplicate target paths",
			cache: Cache{
				Name:        "valid_id",
				CacheKey:    literalKey,
				TargetPaths: []string{"node_modules", "node_modules"},
			},
			wantErr: true,
			errMsg:  "is duplicated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cache.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Cache.Validate() expected error but got none")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error message should contain %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("Cache.Validate() should not return error: %v", err)
				}
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
			got := isValidPath(tt.path)
			if got != tt.want {
				t.Errorf("isValidPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
