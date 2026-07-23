package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"drjosh.dev/zzglob"
	"github.com/buildkite/agent/v3/api"
	"gopkg.in/yaml.v3"
)

type KeyPart struct {
	Arg           string
	Patterns      []string
	Source        Source
	FallbackLimit bool
}
type Source string

const (
	SourceLiteral  Source = "literal"
	SourceAgent    Source = "agent"
	SourceEnv      Source = "env"
	SourceChecksum Source = "checksum"
)

var supportedAgentArgs = map[string]struct{}{
	"os":       {},
	"arch":     {},
	"branch":   {},
	"step":     {},
	"pipeline": {},
}

func (k *KeyPart) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			return fmt.Errorf("cache_key: literal entry cannot be empty")
		}
		k.Source, k.Arg = SourceLiteral, node.Value
		return nil

	case yaml.MappingNode:
		return k.unmarshalMapping(node)

	default:
		return fmt.Errorf("cache_key: entry must be a string or a { source: arg } map")
	}
}

func (k *KeyPart) unmarshalMapping(node *yaml.Node) error {
	fields := make(map[string]yaml.Node)
	if err := node.Decode(&fields); err != nil {
		return fmt.Errorf("cache_key: invalid entry: %w", err)
	}

	// fallback_limit is an optional modifier alongside the single source.
	hasFallbackLimit := false
	if raw, ok := fields["fallback_limit"]; ok {
		if err := raw.Decode(&k.FallbackLimit); err != nil {
			return fmt.Errorf("cache_key: fallback_limit must be a boolean")
		}
		hasFallbackLimit = true
	}

	// Exactly one source key; fallback_limit is a modifier, not a source.
	sourceCount := len(fields)
	if hasFallbackLimit {
		sourceCount--
	}
	if sourceCount != 1 {
		return fmt.Errorf("cache_key: entry must name exactly one source (plus optional fallback_limit)")
	}

	for src, val := range fields {
		if src == "fallback_limit" {
			continue
		}
		switch Source(src) {
		case SourceAgent:
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("cache_key: agent source must be a string")
			}
			if _, ok := supportedAgentArgs[val.Value]; !ok {
				return fmt.Errorf("cache_key: unsupported agent argument: %s", val.Value)
			}
			k.Source, k.Arg = SourceAgent, val.Value
		case SourceChecksum:
			patterns, err := decodeChecksumPatterns(&val)
			if err != nil {
				return err
			}
			k.Source, k.Patterns = SourceChecksum, patterns
		case SourceEnv:
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("cache_key: env source must be a string")
			}
			if val.Value == "" {
				return fmt.Errorf("cache_key: env entry cannot be empty")
			}
			k.Source, k.Arg = SourceEnv, val.Value
		default:
			return fmt.Errorf("cache_key: unknown source %q (supported sources: agent, checksum, env)", src)
		}
	}
	return nil
}

// decodeChecksumPatterns accepts the checksum argument as either a single file
// path (scalar) or an array of file paths / glob patterns (sequence), and
// returns the normalised, non-empty list of patterns.
func decodeChecksumPatterns(val *yaml.Node) ([]string, error) {
	switch val.Kind {
	case yaml.ScalarNode:
		if val.Value == "" {
			return nil, fmt.Errorf("cache_key: checksum entry cannot be empty")
		}
		return []string{val.Value}, nil

	case yaml.SequenceNode:
		var patterns []string
		if err := val.Decode(&patterns); err != nil {
			return nil, fmt.Errorf("cache_key: checksum array must be a list of strings: %w", err)
		}
		if len(patterns) == 0 {
			return nil, fmt.Errorf("cache_key: checksum array cannot be empty")
		}
		for _, p := range patterns {
			if p == "" {
				return nil, fmt.Errorf("cache_key: checksum array entries cannot be empty")
			}
		}
		return patterns, nil

	default:
		return nil, fmt.Errorf("cache_key: checksum must be a file path or an array of file paths")
	}
}

// ResolveCacheKey resolves each KeyPart to its concrete value and returns the
// flat wire shape the backend expects. A part is mandatory iff it is at or
// before the part declaring fallback_limit; parts after it are optional. When no
// part declares fallback_limit, every part is mandatory. At most one part may
// declare it (enforced by Validate).
func ResolveCacheKey(keyParts []KeyPart, env map[string]string) ([]api.CacheKeyPart, error) {
	if len(keyParts) == 0 {
		return nil, fmt.Errorf("cache_key cannot be empty")
	}

	// Default boundary = last part → every part mandatory.
	limit := len(keyParts) - 1
	for i, keyPart := range keyParts {
		if keyPart.FallbackLimit {
			limit = i
			break
		}
	}

	out := make([]api.CacheKeyPart, len(keyParts))
	for i, keyPart := range keyParts {
		resolvedKeyPart, err := keyPart.Resolve(env)
		if err != nil {
			return nil, fmt.Errorf("cache_key[%d]: %w", i, err)
		}
		out[i] = api.CacheKeyPart{Value: resolvedKeyPart, Mandatory: i <= limit}
	}
	return out, nil
}

func (k KeyPart) Resolve(env map[string]string) (string, error) {
	var v string
	var err error
	switch k.Source {
	case SourceLiteral:
		v = k.Arg
	case SourceAgent:
		switch k.Arg {
		case "os":
			v = runtime.GOOS
		case "arch":
			v = runtime.GOARCH
		case "branch":
			v = lookupEnv(env, "BUILDKITE_BRANCH")
		case "pipeline":
			v = lookupEnv(env, "BUILDKITE_PIPELINE_SLUG")
		case "step":
			v = lookupEnv(env, "BUILDKITE_STEP_KEY")
			if v == "" {
				v = lookupEnv(env, "BUILDKITE_STEP_ID")
			}
		default:
			return "", fmt.Errorf("cache_key: unsupported agent fact %q", k.Arg)
		}
	case SourceEnv:
		v = lookupEnv(env, k.Arg)
	case SourceChecksum:
		v, err = checksumDigest(k.Patterns)
	default:
		return "", fmt.Errorf("cache_key: unknown source %q", k.Source)
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// lookupEnv reads name from the supplied env map, or from the process
// environment when env is nil. Missing value resolves to "".
func lookupEnv(env map[string]string, name string) string {
	if env != nil {
		return env[name]
	}
	return os.Getenv(name)
}

// checksumDigest folds every file matched by patterns into a single
// deterministic SHA-256 digest.
func checksumDigest(patterns []string) (string, error) {
	if len(patterns) == 0 {
		return "", fmt.Errorf("cache_key: checksum has no patterns")
	}
	if len(patterns) == 1 && !hasGlobMeta(patterns[0]) {
		return sha256File(patterns[0])
	}

	seen := make(map[string]struct{})
	for _, pattern := range patterns {
		if !hasGlobMeta(pattern) {
			// Literal path: must exist and must be a regular file since a directory has no hashable contents.
			info, err := os.Stat(pattern)
			if err != nil {
				return "", fmt.Errorf("cache_key: checksum %q: %w", pattern, err)
			}
			if info.IsDir() {
				return "", fmt.Errorf("cache_key: checksum %q is a directory", pattern)
			}
			// Clean before use as a dedup key so aliases like "go.mod" and "./go.mod" collapse to one entry.
			seen[filepath.ToSlash(filepath.Clean(pattern))] = struct{}{}
			continue
		}

		parsed, err := zzglob.Parse(pattern)
		if err != nil {
			return "", fmt.Errorf("cache_key: checksum pattern %q: %w", pattern, err)
		}
		// zzglob walks the filesystem sequentially (fs.WalkDir), so recording
		// matches into seen needs no synchronisation. A glob matching nothing
		// simply never invokes the callback; a read error on a matched path is
		// surfaced rather than silently dropped.
		walk := func(match string, d fs.DirEntry, err error) error {
			if err != nil {
				// A non-existent path (e.g. the glob's base directory is absent)
				// just means the glob matched nothing here — legitimate and not an
				// error.
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return fmt.Errorf("cache_key: checksum pattern %q: %w", pattern, err)
			}
			if d == nil {
				return nil
			}
			// d.IsDir() reports false for a symlink to a directory (it reflects
			// the link, not its target), so stat the resolved path to exclude
			// directories reached via a symlink too.
			info, err := os.Stat(match)
			if err != nil {
				return fmt.Errorf("cache_key: checksum pattern %q: %w", pattern, err)
			}
			if info.IsDir() {
				return nil // directory (or symlink to one) has no hashable contents
			}
			seen[filepath.ToSlash(match)] = struct{}{}
			return nil
		}
		// WalkIntermediateDirs makes zzglob invoke the callback for the pattern's
		// fixed walk root.
		if err := parsed.Glob(walk, zzglob.WalkIntermediateDirs(true)); err != nil {
			return "", err
		}
	}
	if len(seen) == 0 {
		return "", fmt.Errorf("cache_key: checksum patterns %v matched no files", patterns)
	}

	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	h := sha256.New()
	for _, p := range paths {
		sum, err := sha256File(filepath.FromSlash(p))
		if err != nil {
			return "", err
		}
		// path\0contents-hash\n.
		_, _ = fmt.Fprintf(h, "%s\x00%s\n", p, sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hasGlobMeta reports whether p contains any glob metacharacter. A path with
// none is a plain file path treated as a literal.
func hasGlobMeta(p string) bool {
	return strings.ContainsAny(p, "*?[{")
}

// sha256File returns the hex-encoded SHA-256 of the file's contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cache_key: checksum %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("cache_key: checksum %q: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
