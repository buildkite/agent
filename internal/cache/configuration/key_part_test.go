package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
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
		{name: "checksum", yaml: `{ checksum: go.mod }`, want: KeyPart{Source: SourceChecksum, Patterns: []string{"go.mod"}}},
		{name: "checksum array", yaml: `{ checksum: [go.mod, go.sum] }`, want: KeyPart{Source: SourceChecksum, Patterns: []string{"go.mod", "go.sum"}}},
		{name: "checksum single-element array", yaml: `{ checksum: [go.mod] }`, want: KeyPart{Source: SourceChecksum, Patterns: []string{"go.mod"}}},
		{name: "checksum glob array", yaml: `{ checksum: ["**/*.proto", "buf.gen.yaml"] }`, want: KeyPart{Source: SourceChecksum, Patterns: []string{"**/*.proto", "buf.gen.yaml"}}},
		{name: "env", yaml: `{ env: GO_VERSION }`, want: KeyPart{Source: SourceEnv, Arg: "GO_VERSION"}},
		{name: "fallback_limit on agent", yaml: `{ agent: arch, fallback_limit: true }`, want: KeyPart{Source: SourceAgent, Arg: "arch", FallbackLimit: true}},
		{name: "fallback_limit on checksum", yaml: `{ checksum: go.mod, fallback_limit: true }`, want: KeyPart{Source: SourceChecksum, Patterns: []string{"go.mod"}, FallbackLimit: true}},
		{name: "fallback_limit on checksum array", yaml: `{ checksum: [go.mod, go.sum], fallback_limit: true }`, want: KeyPart{Source: SourceChecksum, Patterns: []string{"go.mod", "go.sum"}, FallbackLimit: true}},
		{name: "fallback_limit false is a no-op", yaml: `{ agent: arch, fallback_limit: false }`, want: KeyPart{Source: SourceAgent, Arg: "arch", FallbackLimit: false}},

		// Out of scope -> parse errors.
		{name: "empty literal", yaml: `""`, wantErr: "literal entry cannot be empty"},
		{name: "agent os_version deferred", yaml: `{ agent: os_version }`, wantErr: "unsupported agent argument"},
		{name: "checksum empty array", yaml: `{ checksum: [] }`, wantErr: "checksum array cannot be empty"},
		{name: "checksum array with empty entry", yaml: `{ checksum: [go.mod, ""] }`, wantErr: "checksum array entries cannot be empty"},
		{name: "checksum array with null entry", yaml: `{ checksum: [go.mod, null] }`, wantErr: "checksum array entries must be strings"},
		{name: "checksum array with tilde-null entry", yaml: `{ checksum: [go.mod, ~] }`, wantErr: "checksum array entries must be strings"},
		{name: "checksum array with nested sequence", yaml: `{ checksum: [go.mod, [go.sum]] }`, wantErr: "checksum array entries must be strings"},
		{name: "cmd deferred", yaml: `{ cmd: ["go", "version"] }`, wantErr: "unknown source"},
		{name: "unknown source", yaml: `{ bogus: x }`, wantErr: "unknown source"},
		{name: "fallback_limit non-bool", yaml: `{ agent: arch, fallback_limit: 7 }`, wantErr: "fallback_limit must be a boolean"},
		{name: "fallback_limit without source", yaml: `{ fallback_limit: true }`, wantErr: "exactly one source"},
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
			if !reflect.DeepEqual(got, tt.want) {
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

	got, err := KeyPart{Source: SourceChecksum, Patterns: []string{path}}.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if got != want {
		t.Fatalf("Resolve() = %q, want %q (sha256 of contents)", got, want)
	}
}

func TestKeyPartResolveChecksumMissingFile(t *testing.T) {
	t.Parallel()

	_, err := KeyPart{Source: SourceChecksum, Patterns: []string{filepath.Join(t.TempDir(), "does-not-exist")}}.Resolve(nil)
	if err == nil {
		t.Fatal("Resolve() error = nil, want error for missing checksum file")
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestKeyPartResolveChecksumArrayAndGlob(t *testing.T) {
	// Not parallel: relies on the process working directory via t.Chdir.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package-lock.json"), "lockfile\n")
	writeFile(t, filepath.Join(dir, "patches", "a.patch"), "patch a\n")
	writeFile(t, filepath.Join(dir, "patches", "b.patch"), "patch b\n")
	writeFile(t, filepath.Join(dir, "patches", "notes.txt"), "ignore me\n")
	t.Chdir(dir)

	// A literal path plus a glob that matches two of three files.
	part := KeyPart{Source: SourceChecksum, Patterns: []string{"package-lock.json", "patches/*.patch"}}
	got, err := part.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if got == "" {
		t.Fatal("Resolve() = empty digest")
	}

	// Reordering the patterns must not change the digest.
	reordered := KeyPart{Source: SourceChecksum, Patterns: []string{"patches/*.patch", "package-lock.json"}}
	got2, err := reordered.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if got != got2 {
		t.Fatalf("digest depends on pattern order: %q != %q", got, got2)
	}

	// An overlapping pattern set (same files, deduplicated) yields the same digest.
	overlap := KeyPart{Source: SourceChecksum, Patterns: []string{"package-lock.json", "patches/*.patch", "patches/a.patch"}}
	got3, err := overlap.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if got != got3 {
		t.Fatalf("dedup changed digest: %q != %q", got, got3)
	}

	// Uncleaned literal aliases ("./package-lock.json") must clean to the same
	// dedup key, so the file is hashed once and the digest is unchanged.
	aliased := KeyPart{Source: SourceChecksum, Patterns: []string{"package-lock.json", "./package-lock.json", "patches/*.patch"}}
	gotAlias, err := aliased.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if got != gotAlias {
		t.Fatalf("literal alias changed digest: %q != %q", got, gotAlias)
	}

	// Changing a matched file's contents changes the digest.
	writeFile(t, filepath.Join(dir, "patches", "a.patch"), "patch a modified\n")
	got4, err := part.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if got == got4 {
		t.Fatal("digest unchanged after a matched file's contents changed")
	}
}

func TestKeyPartResolveChecksumEmptyMatch(t *testing.T) {
	// Not parallel: relies on the process working directory via t.Chdir.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")
	t.Chdir(dir)

	// A non-matching glob alongside a matching one is fine (set is non-empty).
	part := KeyPart{Source: SourceChecksum, Patterns: []string{"go.mod", "patches/*.patch"}}
	if _, err := part.Resolve(nil); err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}

	// When the whole set matches nothing, it is an error.
	empty := KeyPart{Source: SourceChecksum, Patterns: []string{"nope/*.patch", "missing/**/*.lock"}}
	if _, err := empty.Resolve(nil); err == nil {
		t.Fatal("Resolve() error = nil, want error when no pattern matched any file")
	}
}

func TestChecksumDigestSingleGlobExcludesDirectories(t *testing.T) {
	// Not parallel: relies on the process working directory via t.Chdir.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "services", "a", "package-lock.json"), "a\n")
	writeFile(t, filepath.Join(dir, "services", "b", "package-lock.json"), "b\n")
	t.Chdir(dir)

	part := KeyPart{Source: SourceChecksum, Patterns: []string{"services/*/package-lock.json"}}
	if _, err := part.Resolve(nil); err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
}

func TestChecksumDigestGlobUnreadableRoot(t *testing.T) {
	// Not parallel: relies on the process working directory via t.Chdir.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")

	// A directory that the glob's fixed root walks into, made unreadable.
	secret := filepath.Join(dir, "secret")
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(secret, "a.lock"), "locked\n")
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o755) }) // so t.TempDir cleanup can remove it

	// Skip when the environment doesn't actually enforce the permission — e.g.
	// running as root, or a filesystem that ignores chmod (common in CI
	// containers). Without a real read failure there is nothing for this test to
	// observe.
	if _, err := os.ReadDir(secret); err == nil {
		t.Skip("filesystem does not enforce directory read permissions; cannot exercise an unreadable root")
	}
	t.Chdir(dir)

	// The unreadable root must surface as an error, not be silently dropped,
	// even though the other pattern (go.mod) matches. Otherwise the digest would
	// be computed from fewer inputs than declared.
	part := KeyPart{Source: SourceChecksum, Patterns: []string{"go.mod", "secret/**/*.lock"}}
	if _, err := part.Resolve(nil); err == nil {
		t.Fatal("Resolve() error = nil, want error when a glob's root is unreadable")
	}
}

func TestChecksumDigestGlobExcludesSymlinkedDirectory(t *testing.T) {
	// Not parallel: relies on the process working directory via t.Chdir.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real.lock"), "locked\n")
	if err := os.MkdirAll(filepath.Join(dir, "adir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A symlink to a directory: fs.DirEntry.IsDir() reports false for it, so it
	// must be excluded by stat'ing the resolved target rather than hashed.
	if err := os.Symlink(filepath.Join(dir, "adir"), filepath.Join(dir, "linkdir")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	t.Chdir(dir)

	// "*" matches real.lock, adir, and linkdir; only real.lock is hashable.
	part := KeyPart{Source: SourceChecksum, Patterns: []string{"*"}}
	if _, err := part.Resolve(nil); err != nil {
		t.Fatalf("Resolve() unexpected error = %v (symlink-to-dir should be excluded, not hashed)", err)
	}
}

func TestChecksumDigestGlobDoesNotTraverseSymlinkedDirs(t *testing.T) {
	// Not parallel: relies on the process working directory via t.Chdir.
	dir := t.TempDir()
	// A file reachable only by descending through a symlinked directory.
	writeFile(t, filepath.Join(dir, "outside", "x.lock"), "outside\n")
	writeFile(t, filepath.Join(dir, "inside", "y.lock"), "inside\n")
	if err := os.Symlink(filepath.Join(dir, "outside"), filepath.Join(dir, "inside", "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	t.Chdir(dir)

	// "inside/**/*.lock" must hash inside/y.lock but NOT descend through the
	// symlink inside/link into outside/, so outside/x.lock is excluded and the
	// digest stays within the real tree.
	got, err := KeyPart{Source: SourceChecksum, Patterns: []string{"inside/**/*.lock"}}.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}

	// Removing the out-of-tree file must not change the digest — proof it was
	// never followed.
	if err := os.Remove(filepath.Join(dir, "outside", "x.lock")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got2, err := KeyPart{Source: SourceChecksum, Patterns: []string{"inside/**/*.lock"}}.Resolve(nil)
	if err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
	if got != got2 {
		t.Fatalf("digest changed after removing a file reached only via a symlink: %q != %q", got, got2)
	}
}

func TestChecksumDigestGlobDoesNotExpandTilde(t *testing.T) {
	// Not parallel: relies on the process working directory and $HOME.
	home := t.TempDir() // an empty home: if "~" were expanded, nothing matches
	t.Setenv("HOME", home)

	dir := t.TempDir()
	// A directory literally named "~" under the working directory.
	writeFile(t, filepath.Join(dir, "~", "a.lock"), "locked\n")
	t.Chdir(dir)

	// "~/*.lock" must resolve relative to the working directory (matching the
	// literal "~" dir), not expand to $HOME (which is empty and would make the
	// whole set match nothing → error).
	if _, err := (KeyPart{Source: SourceChecksum, Patterns: []string{"~/*.lock"}}).Resolve(nil); err != nil {
		t.Fatalf("Resolve() unexpected error = %v (\"~\" should be literal, not expanded to $HOME)", err)
	}
}

func TestKeyPartResolveChecksumMissingLiteralInArray(t *testing.T) {
	// Not parallel: relies on the process working directory via t.Chdir.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "patches", "a.patch"), "patch a\n")
	// Deliberately no package-lock.json.
	t.Chdir(dir)

	// A named literal that doesn't exist is an error even though the glob matches.
	part := KeyPart{Source: SourceChecksum, Patterns: []string{"package-lock.json", "patches/*.patch"}}
	if _, err := part.Resolve(nil); err == nil {
		t.Fatal("Resolve() error = nil, want error for missing literal in array")
	}

	// Once the literal exists, it resolves.
	writeFile(t, filepath.Join(dir, "package-lock.json"), "lock\n")
	if _, err := part.Resolve(nil); err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}

	// A glob that matches nothing is fine as long as a literal exists.
	globOnly := KeyPart{Source: SourceChecksum, Patterns: []string{"package-lock.json", "does-not-exist/*.patch"}}
	if _, err := globOnly.Resolve(nil); err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}

	// Globs only, none matching, no literal to backstop them: the whole set is
	// empty, which is an error.
	noMatch := KeyPart{Source: SourceChecksum, Patterns: []string{"nope/*.patch", "missing/**/*.lock"}}
	if _, err := noMatch.Resolve(nil); err == nil {
		t.Fatal("Resolve() error = nil, want error when only globs are given and none match")
	}

	// Multiple globs, at least one matching, no literal: resolves over the union
	// of matches.
	writeFile(t, filepath.Join(dir, "protos", "svc.proto"), "syntax\n")
	multiGlob := KeyPart{Source: SourceChecksum, Patterns: []string{"patches/*.patch", "protos/**/*.proto"}}
	if _, err := multiGlob.Resolve(nil); err != nil {
		t.Fatalf("Resolve() unexpected error = %v", err)
	}
}

func TestKeyPartResolveChecksumLiteralDirectoryInArray(t *testing.T) {
	// Not parallel: relies on the process working directory via t.Chdir.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "node_modules", "x"), "x\n")
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")
	t.Chdir(dir)

	// A literal naming a directory has no hashable contents and is an error.
	part := KeyPart{Source: SourceChecksum, Patterns: []string{"go.mod", "node_modules"}}
	if _, err := part.Resolve(nil); err == nil {
		t.Fatal("Resolve() error = nil, want error for literal directory")
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

	t.Run("fallback_limit splits mandatory from optional", func(t *testing.T) {
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

		// The part declaring fallback_limit (os) is itself mandatory;
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

	t.Run("fallback_limit on first part makes only later parts optional", func(t *testing.T) {
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
