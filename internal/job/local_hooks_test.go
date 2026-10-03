package job

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/buildkite/agent/v4/internal/shell"
)

func TestRepositoryHookLocation(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		root       string
		workdir    string
		noCheckout bool
		link       string
		target     string
		want       string
	}{
		{
			name:    "checkout root",
			workdir: ".",
			want:    ".buildkite/hooks/command",
		},
		{
			name:    "service directory",
			workdir: "services/api",
			want:    "services/api/.buildkite/hooks/command",
		},
		{
			name:    "symlinked checkout with physical working directory",
			root:    "../checkout-link",
			workdir: ".",
			link:    "../checkout-link",
			target:  ".",
			want:    ".buildkite/hooks/command",
		},
		{
			name:    "symlinked checkout with physical service directory",
			root:    "../checkout-link",
			workdir: "services/api",
			link:    "../checkout-link",
			target:  ".",
			want:    "services/api/.buildkite/hooks/command",
		},
		{
			name:    "physical checkout with symlinked service directory",
			workdir: "../checkout-link/services/api",
			link:    "../checkout-link",
			target:  ".",
			want:    "services/api/.buildkite/hooks/command",
		},
		{
			name:    "missing service hook does not fall back to root",
			workdir: "services/worker",
		},
		{
			name:    "sibling directory with same path prefix",
			workdir: "../checkout-other",
		},
		{
			name:       "before checkout",
			workdir:    ".",
			noCheckout: true,
		},
		{
			name:    "hook symlink within checkout",
			workdir: "services/api",
			link:    "services/api/.buildkite/hooks/command",
			target:  ".buildkite/hooks/command",
			want:    "services/api/.buildkite/hooks/command",
		},
		{
			name:    "working directory symlink escapes checkout",
			workdir: "linked-service",
			link:    "linked-service",
			target:  "../checkout-other",
		},
		{
			name:    "buildkite directory symlink escapes checkout",
			workdir: "services/api",
			link:    "services/api/.buildkite",
			target:  "../checkout-other/.buildkite",
		},
		{
			name:    "hooks directory symlink escapes checkout",
			workdir: "services/api",
			link:    "services/api/.buildkite/hooks",
			target:  "../checkout-other/.buildkite/hooks",
		},
		{
			name:    "hook symlink escapes checkout",
			workdir: "services/api",
			link:    "services/api/.buildkite/hooks/command",
			target:  "../checkout-other/.buildkite/hooks/command",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			checkout := filepath.Join(t.TempDir(), "checkout")
			for _, dir := range []string{".", "services/api", "../checkout-other"} {
				hooks := filepath.Join(checkout, dir, ".buildkite", "hooks")
				if err := os.MkdirAll(hooks, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(hooks, "command"), []byte("echo hook\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			workdir := filepath.Join(checkout, test.workdir)
			if err := os.MkdirAll(workdir, 0o755); err != nil {
				t.Fatal(err)
			}
			if test.link != "" {
				link := filepath.Join(checkout, test.link)
				if err := os.RemoveAll(link); err != nil {
					t.Fatal(err)
				}
				target, err := filepath.Rel(filepath.Dir(link), filepath.Join(checkout, test.target))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, link); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("cannot create symlink: %v", err)
					}
					t.Fatal(err)
				}
			}

			sh := shell.NewTestShell(t)
			if err := sh.Chdir(workdir); err != nil {
				t.Fatal(err)
			}
			// Updating the checkout-path variable must not move the boundary.
			sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", workdir)
			e := &Executor{shell: sh}
			if !test.noCheckout {
				root, err := os.OpenRoot(filepath.Join(checkout, test.root))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := root.Close(); err != nil {
						t.Error(err)
					}
				})
				e.checkoutRoot = root
			}

			got, err := e.localHookPath("command")
			if test.want == "" {
				if got != "" || !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("localHookPath(command) = (%q, %v), want no hook", got, err)
				}
				return
			}
			if want := filepath.Join(checkout, test.root, test.want); err != nil || got != want {
				t.Fatalf("localHookPath(command) = (%q, %v), want (%q, nil)", got, err, want)
			}
		})
	}
}
