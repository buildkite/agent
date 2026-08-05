package configuration

import (
	"reflect"
	"testing"
)

func TestResolveSaveScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scopes  map[string]bool
		want    map[string]bool
		wantErr string
	}{
		{
			name:   "nil scopes returns non-nil empty map",
			scopes: nil,
			want:   map[string]bool{},
		},
		{
			name:   "empty scopes returns non-nil empty map",
			scopes: map[string]bool{},
			want:   map[string]bool{},
		},
		{
			name:   "all disabled returns non-nil empty map",
			scopes: map[string]bool{ScopeBranch: false, ScopeBuildID: false, ScopePipeline: false},
			want:   map[string]bool{},
		},
		{
			name:   "branch only",
			scopes: map[string]bool{ScopeBranch: true},
			want:   map[string]bool{ScopeBranch: true},
		},
		{
			name:   "build only",
			scopes: map[string]bool{ScopeBuildID: true},
			want:   map[string]bool{ScopeBuildID: true},
		},
		{
			name:   "pipeline only",
			scopes: map[string]bool{ScopePipeline: true},
			want:   map[string]bool{ScopePipeline: true},
		},
		{
			name:   "all enabled",
			scopes: map[string]bool{ScopeBranch: true, ScopeBuildID: true, ScopePipeline: true},
			want: map[string]bool{
				ScopeBranch:   true,
				ScopeBuildID:  true,
				ScopePipeline: true,
			},
		},
		{
			name:   "mixed enabled and disabled skips disabled",
			scopes: map[string]bool{ScopeBranch: true, ScopeBuildID: true, ScopePipeline: false},
			want: map[string]bool{
				ScopeBranch:  true,
				ScopeBuildID: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ResolveSaveScopes(tt.scopes)

			// Contract: always non-nil so the wire `scopes` object marshals to `{}`, never null.
			if got == nil {
				t.Fatal("ResolveSaveScopes() = nil, want non-nil map")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ResolveSaveScopes() = %v, want %v", got, tt.want)
			}
		})
	}
}
