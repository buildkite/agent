package key

import (
	"crypto/sha256"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"drjosh.dev/zzglob"
)

var ignoreFiles = []string{
	".DS_Store",
	"Thumbs.db",
	".git",
	".hg",
	".svn",
	".bzr",
	".vscode",
	".idea",
	".keep",
}

func Template(id, key string) (string, error) {
	return TemplateWithEnv(id, key, nil)
}

func TemplateWithEnv(id, key string, env map[string]string) (string, error) {
	tpl := template.New("key").Option("missingkey=zero").Funcs(template.FuncMap{
		"id":       getID(id),
		"checksum": checksumPaths(),
		"env":      getEnvWithMap(env),
		"agent":    getAgent,
	})
	tpl, err := tpl.Parse(key)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	err = tpl.Execute(&sb, nil)
	if err != nil {
		return "", err
	}
	key = sb.String()

	// remove all leading and trailing whitespace
	key = strings.TrimSpace(key)

	return key, nil
}

func getID(id string) func() string {
	return func() string {
		slog.Debug("getID", "id", id)
		if id == "" {
			return ""
		}
		// remove all leading and trailing whitespace
		id = strings.TrimSpace(id)
		return id
	}
}

func getAgent() map[string]string {
	return map[string]string{
		"os":   runtime.GOOS,
		"arch": runtime.GOARCH,
	}
}

func getEnvWithMap(envMap map[string]string) func(string) string {
	return func(key string) string {
		slog.Info("getEnv", "key", key)

		var env string
		if envMap != nil {
			// Use provided environment map
			env = envMap[key]
		} else {
			// Fall back to OS environment
			env = os.Getenv(key)
		}

		if env == "" {
			return ""
		}

		// remove all leading and trailing whitespace
		env = strings.TrimSpace(env)

		return env
	}
}

func checksumPaths() func(files ...string) string {
	return func(patterns ...string) string {
		slog.Debug("checksumPaths", "files", patterns)

		if len(patterns) == 0 {
			return ""
		}

		// Resolve all patterns to actual file paths
		files, err := resolveFiles(patterns)
		if err != nil {
			slog.Error("error resolving files", "error", err)
			return ""
		}

		if len(files) == 0 {
			slog.Warn("no files found for patterns", "patterns", patterns)
			return ""
		}

		slog.Debug("resolved files for checksumming", "files", len(files))

		// Calculate individual checksums and combine (for backward compatibility)
		var sums []string
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				slog.Error("error reading file", "error", err, "file", file)
				return ""
			}
			sums = append(sums, checksum(data))
			slog.Debug("checksummed file", "file", file)
		}

		// Combine the sums into a single string and hash (matches original behavior)
		combinedSums := strings.Join(sums, "")
		return checksum([]byte(combinedSums))
	}
}

// resolveFiles returns all files that match any of the supplied glob patterns.
// Uses zzglob for full glob pattern support including **, *, ?, [], {a,b}.
// Maintains backward compatibility with existing patterns while adding standard glob capabilities.
func resolveFiles(patterns []string) ([]string, error) {
	seen := make(map[string]struct{})
	var result []string

	for _, patternStr := range patterns {
		slog.Debug("processing glob pattern", "pattern", patternStr)

		// Parse the pattern using zzglob
		pattern, err := zzglob.Parse(patternStr)
		if err != nil {
			slog.Error("glob pattern parse failed", "error", err, "pattern", patternStr)
			return nil, err
		}

		// Use zzglob to find all matches for this pattern
		err = pattern.Glob(func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Skip errors, continue walking
			}

			// Only include files, not directories
			if d.IsDir() {
				return nil
			}

			// Convert to platform-specific path separators
			match := filepath.FromSlash(path)

			// Apply ignore list
			ignored := false
			for _, ignore := range ignoreFiles {
				if strings.HasSuffix(match, ignore) {
					ignored = true
					slog.Debug("ignoring file", "path", match, "ignore", ignore)
					break
				}
			}

			if !ignored {
				// Deduplicate
				if _, exists := seen[match]; !exists {
					seen[match] = struct{}{}
					result = append(result, match)
					slog.Debug("file matched", "path", match, "pattern", patternStr)
				}
			}

			return nil
		})
		if err != nil {
			slog.Error("glob pattern failed", "error", err, "pattern", patternStr)
			return nil, err
		}
	}

	// Sort for deterministic output
	sort.Strings(result)
	slog.Debug("files resolved", "count", len(result))

	return result, nil
}

func checksum(data []byte) string {
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:])
}
