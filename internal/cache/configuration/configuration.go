package configuration

import (
	"os"
	"path/filepath"
	"strings"
)

// CacheForPath returns the default cache definition for a path supplied on the
// command line.
func CacheForPath(path string) Cache {
	path = portableHomePath(path)

	return Cache{
		Name: path,
		CacheKey: []KeyPart{
			{Source: SourceLiteral, Arg: path},
			{Source: SourceAgent, Arg: "os"},
			{Source: SourceAgent, Arg: "arch", FallbackLimit: true},
			{Source: SourceAgent, Arg: "branch"},
		},
		TargetPaths: []string{path},
	}
}

// portableHomePath converts a shell-expanded path beneath the current user's
// home directory back to its portable ~ form. This keeps an unquoted
// `--path ~/.npm` consistent across agents with different home directories.
func portableHomePath(path string) string {
	if !filepath.IsAbs(path) {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(home, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	if rel == "." {
		return "~"
	}
	return filepath.Join("~", rel)
}

// ExpandCacheConfiguration returns the cache definitions unchanged. Cache keys
// are resolved at send time (see ResolveCacheKey), so there is nothing to
// expand at load time.
func ExpandCacheConfiguration(caches []Cache) ([]Cache, error) {
	return caches, nil
}
