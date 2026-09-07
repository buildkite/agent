package configuration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v4/api"
	"github.com/google/go-cmp/cmp"
)

func TestCacheForPath(t *testing.T) {
	t.Parallel()

	got := CacheForPath("~/.npm")
	want := Cache{
		Name: "~/.npm",
		CacheKey: []KeyPart{
			{Source: SourceLiteral, Arg: ".auto"},
			{Source: SourceAgent, Arg: "os"},
			{Source: SourceAgent, Arg: "arch", FallbackLimit: true},
			{Source: SourceAgent, Arg: "branch"},
			{Source: SourceAgent, Arg: "commit"},
		},
		TargetPaths: []string{"~/.npm"},
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("CacheForPath() mismatch (-want +got):\n%s", diff)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("CacheForPath().Validate() error = %v, want nil", err)
	}
}

func TestCacheForPathResolvedKey(t *testing.T) {
	t.Parallel()

	cache := CacheForPath("~/.npm")
	got, err := ResolveCacheKey(cache.CacheKey, map[string]string{
		"BUILDKITE_BRANCH": "main",
		"BUILDKITE_COMMIT": "0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("ResolveCacheKey() error = %v, want nil", err)
	}

	want := []api.CacheKeyPart{
		{Value: ".auto", Mandatory: true},
		{Value: runtime.GOOS, Mandatory: true},
		{Value: runtime.GOARCH, Mandatory: true},
		{Value: "main", Mandatory: false},
		{Value: "0123456789abcdef", Mandatory: false},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ResolveCacheKey() mismatch (-want +got):\n%s", diff)
	}
}

func TestCacheForPathNormalizesShellExpandedHomePath(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir() error = %v", err)
	}

	got := CacheForPath(filepath.Join(home, ".npm"))
	want := "~/.npm"
	if got.Name != want {
		t.Errorf("CacheForPath().Name = %q, want %q", got.Name, want)
	}
	if diff := cmp.Diff([]string{want}, got.TargetPaths); diff != "" {
		t.Errorf("CacheForPath().TargetPaths mismatch (-want +got):\n%s", diff)
	}
	if got.CacheKey[0].Arg != ".auto" {
		t.Errorf("CacheForPath().CacheKey[0].Arg = %q, want %q", got.CacheKey[0].Arg, ".auto")
	}
}
