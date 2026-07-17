package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/buildkite/agent/v3/api"
	"gopkg.in/yaml.v3"
)

type KeyPart struct {
	Arg    string
	Source Source
	// FallbackLimit marks the fallback boundary: this part and every part before
	// it are mandatory; parts after it are optional. At most one part may set it.
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
			if val.Kind != yaml.ScalarNode {
				return fmt.Errorf("cache_key: checksum takes a single file path (no arrays)")
			}
			if val.Value == "" {
				return fmt.Errorf("cache_key: checksum entry cannot be empty")
			}
			k.Source, k.Arg = SourceChecksum, val.Value
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
		// For now, support is for single regular file only. Multi-file checksums, globs, and
		// directories are not supported. We have NOT yet considered symlinks, special files, or very large
		// files — k.Arg is treated as one path.
		v, err = sha256File(k.Arg)
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

// sha256File returns the hex-encoded SHA-256 of the file's contents
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
