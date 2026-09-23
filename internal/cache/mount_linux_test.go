package cache

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/cache/configuration"
	"golang.org/x/sys/unix"
)

// Run in a subprocess with private user/mount namespaces: no mounts escape the
// test, and the Go test process never changes its own thread's mount namespace.
func TestCacheBindMounts(t *testing.T) {
	if os.Getenv("BUILDKITE_TEST_CACHE_MOUNT_NAMESPACE") != "1" {
		if out, err := exec.Command("unshare", "-Urnm", "true").CombinedOutput(); err != nil {
			t.Skipf("requires unshare and user/mount namespaces: %v: %s", err, out)
		}
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("unshare", "-Urnm", exe, "-test.run=^TestCacheBindMounts$", "-test.v")
		cmd.Env = append(os.Environ(), "BUILDKITE_TEST_CACHE_MOUNT_NAMESPACE=1")
		out, err := cmd.CombinedOutput()
		t.Logf("%s", out)
		if err != nil {
			t.Fatalf("bind-mount subprocess: %v", err)
		}
		return
	}

	base := t.TempDir()
	source := filepath.Join(base, "source")
	target := filepath.Join(base, "mount with spaces")
	storage := filepath.Join(base, "storage")
	for _, dir := range []string{source, target, storage} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mount(source, target, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Unmount(target, 0); err != nil {
			t.Error(err)
		}
	})

	// Exercise mount detection through a symlinked parent, not just its literal
	// mountinfo spelling. Source and target also share a filesystem device ID.
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(base, alias); err != nil {
		t.Fatal(err)
	}
	configuredTarget := filepath.Join(alias, filepath.Base(target))
	c := &client{
		api: newMockAPIClient("local_file"), registry: "~", format: "zip",
		bucketURL: (&url.URL{Scheme: "file", Path: storage}).String(),
		caches: []configuration.Cache{{
			Name: "mounted", TargetPaths: []string{configuredTarget},
			CacheKey: []configuration.KeyPart{{Source: configuration.SourceLiteral, Arg: "mount-test"}},
		}},
	}
	want := "saved dependency contents"
	if err := os.WriteFile(filepath.Join(source, "dependency"), []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Save(t.Context(), "mounted"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "dependency"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "stale"), []byte("remove me"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.WriteFile(outside, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(source, "stale-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o555); err != nil {
		t.Fatal(err)
	}
	result, err := c.Restore(t.Context(), "mounted")
	if err != nil || !result.CacheRestored {
		t.Fatalf("Restore: restored=%v, err=%v", result.CacheRestored, err)
	}
	if got, err := os.ReadFile(filepath.Join(source, "dependency")); err != nil || string(got) != want {
		t.Fatalf("restored contents through mount source = %q, %v; want %q", got, err, want)
	}
	if _, err := os.Stat(filepath.Join(source, "stale")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale file remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(source, "stale-link")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale symlink remains: %v", err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "keep me" {
		t.Fatalf("cleanup followed symlink outside target: %q, %v", got, err)
	}
	if mounted, err := cleanupMount(configuredTarget); err != nil || !mounted {
		t.Fatalf("mount was not preserved: mounted=%v err=%v", mounted, err)
	}

	t.Run("protected mount remains protected", func(t *testing.T) {
		t.Chdir(target)
		if err := cleanPath(t.Context(), target); err == nil || !strings.Contains(err.Error(), "protected directory") {
			t.Fatalf("cleanPath should protect mounted cwd: %v", err)
		}
		if got, err := os.ReadFile(filepath.Join(source, "dependency")); err != nil || string(got) != want {
			t.Fatalf("protected contents changed: %q, %v", got, err)
		}
	})

	t.Run("nested mount rejected before any target is changed", func(t *testing.T) {
		first := filepath.Join(base, "first")
		nested := filepath.Join(target, "nested")
		for _, dir := range []string{first, nested} {
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		marker := filepath.Join(first, "keep")
		if err := os.WriteFile(marker, []byte("untouched"), 0o600); err != nil {
			t.Fatal(err)
		}
		c.caches[0].TargetPaths = []string{first, configuredTarget}
		c.caches[0].CacheKey[0].Arg = "nested-test"
		if _, err := c.Save(t.Context(), "mounted"); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mount(storage, nested, "", unix.MS_BIND, ""); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := unix.Unmount(nested, 0); err != nil {
				t.Error(err)
			}
		}()
		if err := os.Chmod(source, 0o555); err != nil {
			t.Fatal(err)
		}
		_, err := c.Restore(t.Context(), "mounted")
		if err == nil || !strings.Contains(err.Error(), "nested mount") {
			t.Fatalf("Restore should reject nested mount: %v", err)
		}
		if got, err := os.ReadFile(marker); err != nil || string(got) != "untouched" {
			t.Fatalf("earlier target changed: %q, %v", got, err)
		}
		if info, err := os.Stat(source); err != nil || info.Mode().Perm() != 0o555 {
			t.Fatalf("mounted target permissions changed: %v, %v", info, err)
		}
	})
}
