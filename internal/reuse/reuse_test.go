package reuse

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "commit")
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"x"}`, 0o644)
	writeFile(t, filepath.Join(dir, "web", "app.ts"), "console.log(1)\n", 0o644)
	writeFile(t, filepath.Join(dir, "web", "README.md"), "# readme\n", 0o644)
	writeFile(t, filepath.Join(dir, "script.sh"), "#!/bin/sh\n", 0o644)
	commitAll(t, dir)
	return dir
}

func mustHash(t *testing.T, dir string, include, exclude []string, untracked bool) Result {
	t.Helper()
	res, err := HashFiles(t.Context(), dir, include, exclude, untracked)
	if err != nil {
		t.Fatalf("HashFiles(%v, %v) error = %v", include, exclude, err)
	}
	if !strings.HasPrefix(res.Digest, "b3:") || len(res.Digest) != 3+64 {
		t.Fatalf("HashFiles digest = %q, want b3:<64 hex>", res.Digest)
	}
	return res
}

func TestReuseHashFiles(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	all := []string{"**"}

	base := mustHash(t, dir, all, nil, false)
	if got, want := base.Count, 4; got != want {
		t.Errorf("Count = %d, want %d", got, want)
	}

	// Working tree changes that are not committed don't affect the digest.
	writeFile(t, filepath.Join(dir, "web", "app.ts"), "console.log(2)\n", 0o644)
	if got := mustHash(t, dir, all, nil, false).Digest; got != base.Digest {
		t.Errorf("digest after uncommitted change = %q, want unchanged %q", got, base.Digest)
	}

	// Committed content changes do.
	commitAll(t, dir)
	changed := mustHash(t, dir, all, nil, false)
	if changed.Digest == base.Digest {
		t.Errorf("digest after committed change = %q, want different", changed.Digest)
	}

	// So does the executable bit.
	writeFile(t, filepath.Join(dir, "script.sh"), "#!/bin/sh\n", 0o755)
	commitAll(t, dir)
	execBit := mustHash(t, dir, all, nil, false)
	if execBit.Digest == changed.Digest {
		t.Errorf("digest after chmod +x = %q, want different", execBit.Digest)
	}

	// Exclude removes files.
	excluded := mustHash(t, dir, []string{"web/**"}, []string{"web/**/*.md"}, false)
	if excluded.Count != 1 || excluded.Files[0].Path != "web/app.ts" {
		t.Errorf("excluded files = %+v, want only web/app.ts", excluded.Files)
	}

	// Zero matches is an error.
	_, err := HashFiles(t.Context(), dir, []string{"nope/**"}, nil, false)
	if !errors.Is(err, ErrNoFilesMatched) || err.Error() != `inputs.files pattern "nope/**" matched no files` {
		t.Errorf("HashFiles(no match) error = %v, want pattern \"nope/**\" matched no files", err)
	}

	// So is any one pattern matching nothing, even when others match.
	_, err = HashFiles(t.Context(), dir, []string{"web/**", "nope/**", "package.json"}, nil, false)
	if !errors.Is(err, ErrNoFilesMatched) || err.Error() != `inputs.files pattern "nope/**" matched no files` {
		t.Errorf("HashFiles(second pattern no match) error = %v, want pattern \"nope/**\" matched no files", err)
	}

	// A pattern whose files are all excluded matches nothing.
	_, err = HashFiles(t.Context(), dir, []string{"web/app.ts", "web/*.md"}, []string{"**/*.md"}, false)
	if !errors.Is(err, ErrNoFilesMatched) || err.Error() != `inputs.files pattern "web/*.md" matched no files` {
		t.Errorf("HashFiles(excluded pattern) error = %v, want pattern \"web/*.md\" matched no files", err)
	}

	// Overlapping patterns each count the files they match.
	if got := mustHash(t, dir, []string{"web/**", "web/app.ts"}, nil, false).Count; got != 2 {
		t.Errorf("Count(overlapping patterns) = %d, want 2", got)
	}
}

func TestReuseHashFilesUntracked(t *testing.T) {
	t.Parallel()
	dir := newRepo(t)
	writeFile(t, filepath.Join(dir, ".gitignore"), "gen/\n", 0o644)
	commitAll(t, dir)
	writeFile(t, filepath.Join(dir, "gen", "out.txt"), "generated\n", 0o644)

	if _, err := HashFiles(t.Context(), dir, []string{"gen/**"}, nil, false); !errors.Is(err, ErrNoFilesMatched) {
		t.Errorf("HashFiles(untracked=false) error = %v, want %v", err, ErrNoFilesMatched)
	}
	res := mustHash(t, dir, []string{"gen/**"}, nil, true)
	if res.Count != 1 {
		t.Errorf("Count = %d, want 1", res.Count)
	}
	writeFile(t, filepath.Join(dir, "gen", "out.txt"), "generated 2\n", 0o644)
	if got := mustHash(t, dir, []string{"gen/**"}, nil, true).Digest; got == res.Digest {
		t.Errorf("untracked digest unchanged after edit")
	}
}

func TestReuseTreeDigest(t *testing.T) {
	t.Parallel()
	mk := func(content string, mtime time.Time) string {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a", "b.txt"), content, 0o644)
		writeFile(t, filepath.Join(dir, "c.txt"), "c", 0o755)
		for _, p := range []string{"a/b.txt", "c.txt"} {
			if err := os.Chtimes(filepath.Join(dir, p), mtime, mtime); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	d1, n1, err := TreeDigest(mk("hello", time.Unix(1000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	d2, _, err := TreeDigest(mk("hello", time.Unix(2000000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	d3, _, err := TreeDigest(mk("hellp", time.Unix(1000, 0)))
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 2 {
		t.Errorf("files = %d, want 2", n1)
	}
	if d1 != d2 {
		t.Errorf("digests differ by mtime: %q != %q", d1, d2)
	}
	if d1 == d3 {
		t.Errorf("digest unchanged by content change: %q", d1)
	}
}

func TestReusePackUnpackRoundTrip(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "bin", "tool"), "#!/bin/sh\necho hi\n", 0o755)
	writeFile(t, filepath.Join(src, "lib", "x.js"), "x", 0o644)
	if err := os.Symlink("../bin/tool", filepath.Join(src, "lib", "tool")); err != nil {
		t.Fatal(err)
	}

	archive := filepath.Join(t.TempDir(), "out.tar.gz")
	if err := Pack(src, archive); err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	dest := filepath.Join(t.TempDir(), "dest")
	if err := Unpack(archive, dest); err != nil {
		t.Fatalf("Unpack() error = %v", err)
	}

	fi, err := os.Stat(filepath.Join(dest, "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("bin/tool mode = %v, want 0755", fi.Mode().Perm())
	}
	target, err := os.Readlink(filepath.Join(dest, "lib", "tool"))
	if err != nil {
		t.Fatalf("lib/tool is not a symlink: %v", err)
	}
	if target != "../bin/tool" {
		t.Errorf("lib/tool target = %q, want ../bin/tool", target)
	}

	want, _, err := TreeDigest(src)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := TreeDigest(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("TreeDigest after round trip = %q, want %q", got, want)
	}
}

func TestReuseSafeName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"/etc/passwd", "../x", "a/../../x", "a/.."} {
		if _, err := safeName(name); err == nil {
			t.Errorf("safeName(%q) error = nil, want error", name)
		}
	}
	for _, name := range []string{"a", "a/b/", "a..b"} {
		if _, err := safeName(name); err != nil {
			t.Errorf("safeName(%q) error = %v", name, err)
		}
	}
}
