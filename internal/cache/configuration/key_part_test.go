package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"gopkg.in/yaml.v3"
)

func TestKeyPartUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string
		want    KeyPart
		wantErr string
	}{
		// Valid M1 subset.
		{name: "literal", yaml: `node`, want: KeyPart{Source: SourceLiteral, Arg: "node"}},
		{name: "literal quoted", yaml: `"v3"`, want: KeyPart{Source: SourceLiteral, Arg: "v3"}},
		{name: "agent os", yaml: `{ agent: os }`, want: KeyPart{Source: SourceAgent, Arg: "os"}},
		{name: "agent arch", yaml: `{ agent: arch }`, want: KeyPart{Source: SourceAgent, Arg: "arch"}},
		{name: "agent branch", yaml: `{ agent: branch }`, want: KeyPart{Source: SourceAgent, Arg: "branch"}},
		{name: "agent step", yaml: `{ agent: step }`, want: KeyPart{Source: SourceAgent, Arg: "step"}},
		{name: "agent pipeline", yaml: `{ agent: pipeline }`, want: KeyPart{Source: SourceAgent, Arg: "pipeline"}},
		{name: "checksum", yaml: `{ checksum: go.mod }`, want: KeyPart{Source: SourceChecksum, Arg: "go.mod"}},
		{name: "env", yaml: `{ env: GO_VERSION }`, want: KeyPart{Source: SourceEnv, Arg: "GO_VERSION"}},
		{name: "fallbackLimit on agent", yaml: `{ agent: arch, fallbackLimit: true }`, want: KeyPart{Source: SourceAgent, Arg: "arch", FallbackLimit: true}},
		{name: "fallbackLimit on checksum", yaml: `{ checksum: go.mod, fallbackLimit: true }`, want: KeyPart{Source: SourceChecksum, Arg: "go.mod", FallbackLimit: true}},
		{name: "fallbackLimit false is a no-op", yaml: `{ agent: arch, fallbackLimit: false }`, want: KeyPart{Source: SourceAgent, Arg: "arch", FallbackLimit: false}},

		// Out of M1 scope -> parse errors.
		{name: "empty literal", yaml: `""`, wantErr: "literal entry cannot be empty"},
		{name: "agent os_version deferred", yaml: `{ agent: os_version }`, wantErr: "unsupported agent argument"},
		{name: "checksum array deferred", yaml: `{ checksum: [go.mod, go.sum] }`, wantErr: "single file path"},
		{name: "cmd deferred", yaml: `{ cmd: ["go", "version"] }`, wantErr: "unknown source"},
		{name: "unknown source", yaml: `{ bogus: x }`, wantErr: "unknown source"},
		{name: "fallbackLimit non-bool", yaml: `{ agent: arch, fallbackLimit: 7 }`, wantErr: "fallbackLimit must be a boolean"},
		{name: "fallbackLimit without source", yaml: `{ fallbackLimit: true }`, wantErr: "exactly one source"},
		{name: "empty map", yaml: `{}`, wantErr: "exactly one source"},
		{name: "sequence", yaml: `[a, b]`, wantErr: "must be a string or a { source: arg } map"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got KeyPart
			err := yaml.Unmarshal([]byte(tt.yaml), &got)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("Unmarshal(%q) error = nil, want error containing %q", tt.yaml, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Unmarshal(%q) error = %q, want containing %q", tt.yaml, err.Error(), tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unmarshal(%q) unexpected error = %v", tt.yaml, err)
			}
			if got != tt.want {
				t.Fatalf("Unmarshal(%q) = %+v, want %+v", tt.yaml, got, tt.want)
			}
		})
	}
}

func TestKeyPartResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		part    KeyPart
		env     map[string]string
		want    string
		wantErr string
	}{
		{name: "literal", part: KeyPart{Source: SourceLiteral, Arg: "v3"}, want: "v3"},
		{name: "agent os", part: KeyPart{Source: SourceAgent, Arg: "os"}, want: runtime.GOOS},
		{name: "agent arch", part: KeyPart{Source: SourceAgent, Arg: "arch"}, want: runtime.GOARCH},
		{name: "env from map", part: KeyPart{Source: SourceEnv, Arg: "FOO"}, env: map[string]string{"FOO": "bar"}, want: "bar"},
		{name: "env missing from map", part: KeyPart{Source: SourceEnv, Arg: "MISSING"}, env: map[string]string{}, want: ""},
		{name: "agent branch", part: KeyPart{Source: SourceAgent, Arg: "branch"}, env: map[string]string{"BUILDKITE_BRANCH": "main"}, want: "main"},
		{name: "agent pipeline", part: KeyPart{Source: SourceAgent, Arg: "pipeline"}, env: map[string]string{"BUILDKITE_PIPELINE_SLUG": "my-pipeline"}, want: "my-pipeline"},
		{name: "agent step from key", part: KeyPart{Source: SourceAgent, Arg: "step"}, env: map[string]string{"BUILDKITE_STEP_KEY": "build", "BUILDKITE_STEP_ID": "01ABC"}, want: "build"},
		{name: "agent step falls back to id", part: KeyPart{Source: SourceAgent, Arg: "step"}, env: map[string]string{"BUILDKITE_STEP_ID": "01ABC"}, want: "01ABC"},
		{name: "agent branch missing is empty", part: KeyPart{Source: SourceAgent, Arg: "branch"}, env: map[string]string{}, want: ""},
		{name: "agent unsupported fact", part: KeyPart{Source: SourceAgent, Arg: "queue"}, wantErr: "unsupported agent fact"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.part.Resolve(tt.env)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Resolve() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() unexpected error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestKeyPartResolveEnvFromOS(t *testing.T) {
	t.Setenv("CACHE_KEY_TEST_VAR", "from-os-env")

	got, err := KeyPart{Source: SourceEnv, Arg: "CACHE_KEY_TEST_VAR"}.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if want := "from-os-env"; got != want {
		t.Fatalf("Resolve() = %q, want %q", got, want)
	}
}

func TestKeyPartResolveChecksum(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "lock")
	contents := []byte("dependency contents\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sum := sha256.Sum256(contents)
	want := hex.EncodeToString(sum[:])

	got, err := KeyPart{Source: SourceChecksum, Arg: path}.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if got != want {
		t.Fatalf("Resolve() = %q, want %q (sha256 of contents)", got, want)
	}
}

func TestKeyPartResolveChecksumMissingFile(t *testing.T) {
	t.Parallel()

	_, err := KeyPart{Source: SourceChecksum, Arg: filepath.Join(t.TempDir(), "does-not-exist")}.Resolve(nil)
	if err == nil {
		t.Fatal("Resolve() error = nil, want error for missing checksum file")
	}
}

func TestResolveCacheKey(t *testing.T) {
	t.Parallel()

	t.Run("empty is an error", func(t *testing.T) {
		t.Parallel()
		if _, err := ResolveCacheKey(nil, nil); err == nil {
			t.Fatal("ResolveCacheKey(nil) error = nil, want error")
		}
	})

	t.Run("resolves every part as mandatory", func(t *testing.T) {
		t.Parallel()
		parts := []KeyPart{
			{Source: SourceLiteral, Arg: "node"},
			{Source: SourceAgent, Arg: "os"},
			{Source: SourceEnv, Arg: "FOO"},
		}
		got, err := ResolveCacheKey(parts, map[string]string{"FOO": "bar"})
		if err != nil {
			t.Fatalf("ResolveCacheKey() unexpected error = %v", err)
		}

		want := []api.CacheKeyPart{
			{Value: "node", Mandatory: true},
			{Value: runtime.GOOS, Mandatory: true},
			{Value: "bar", Mandatory: true},
		}
		if len(got) != len(want) {
			t.Fatalf("ResolveCacheKey() len = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("ResolveCacheKey()[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("fallbackLimit splits mandatory from optional", func(t *testing.T) {
		t.Parallel()
		parts := []KeyPart{
			{Source: SourceLiteral, Arg: "node"},
			{Source: SourceAgent, Arg: "os", FallbackLimit: true},
			{Source: SourceEnv, Arg: "FOO"},
		}
		got, err := ResolveCacheKey(parts, map[string]string{"FOO": "bar"})
		if err != nil {
			t.Fatalf("ResolveCacheKey() unexpected error = %v", err)
		}

		// The part declaring fallbackLimit (os) is itself mandatory;
		// only parts after it are optional.
		want := []api.CacheKeyPart{
			{Value: "node", Mandatory: true},
			{Value: runtime.GOOS, Mandatory: true},
			{Value: "bar", Mandatory: false},
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("ResolveCacheKey()[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("fallbackLimit on first part makes only later parts optional", func(t *testing.T) {
		t.Parallel()
		parts := []KeyPart{
			{Source: SourceLiteral, Arg: "node", FallbackLimit: true},
			{Source: SourceAgent, Arg: "os"},
		}
		got, err := ResolveCacheKey(parts, nil)
		if err != nil {
			t.Fatalf("ResolveCacheKey() unexpected error = %v", err)
		}
		if !got[0].Mandatory {
			t.Fatalf("got[0].Mandatory = false, want true")
		}
		if got[1].Mandatory {
			t.Fatalf("got[1].Mandatory = true, want false")
		}
	})

	t.Run("propagates a resolution error", func(t *testing.T) {
		t.Parallel()
		parts := []KeyPart{{Source: SourceAgent, Arg: "queue"}}
		if _, err := ResolveCacheKey(parts, nil); err == nil {
			t.Fatal("ResolveCacheKey() error = nil, want error from unsupported agent fact")
		}
	})
}
