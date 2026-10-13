package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func writeRepositoryHook(t *testing.T, tester *ExecutorTester, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(tester.HooksDir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func repositoryAllowlistEnv(patterns ...string) string {
	return "BUILDKITE_ALLOWED_REPOSITORIES=" + strings.Join(patterns, ",")
}

func TestCheckoutRepositoryAllowlistAfterHooks(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("test hooks use POSIX shell syntax")
	}

	for _, mode := range []string{"strict", "from-job", "none"} {
		for _, source := range []string{"environment", "pre-checkout", "plugin-pre-checkout", "custom-checkout", "clear-policy", "empty-repository"} {
			t.Run(mode+"/"+source, func(t *testing.T) {
				t.Parallel()
				tester, err := NewExecutorTester(mainCtx)
				if err != nil {
					t.Fatal(err)
				}
				defer tester.Close()

				environ := []string{
					"BUILDKITE_CHECKOUT_OVERRIDE_MODE=" + mode,
					repositoryAllowlistEnv("^" + regexp.QuoteMeta(tester.Repo.Path) + "$"),
				}
				body := "export BUILDKITE_REPO=https://disallowed.example/repo.git"
				hookName := "pre-checkout"
				if source == "environment" {
					hookName = "environment"
				}
				if source == "empty-repository" {
					body = "export BUILDKITE_REPO="
				}
				if source == "clear-policy" {
					body += "\nexport BUILDKITE_ALLOWED_REPOSITORIES="
				}
				if source == "plugin-pre-checkout" {
					plugin := createTestPlugin(t, map[string][]string{
						"pre-checkout": {"#!/bin/sh", body},
					})
					defer plugin.Close()
					pluginJSON, err := plugin.ToJSON()
					if err != nil {
						t.Fatal(err)
					}
					environ = append(environ, "BUILDKITE_PLUGINS="+pluginJSON)
				} else {
					writeRepositoryHook(t, tester, hookName, body)
					// Reject before any clone, fetch, keyscan, or checkout retry.
					tester.MustMock(t, "git").Expect().NotCalled()
				}
				if source == "custom-checkout" {
					tester.ExpectGlobalHook("checkout").NotCalled()
				}
				// Rejection must precede destructive clean-checkout handling.
				checkoutDir := t.TempDir()
				marker := filepath.Join(checkoutDir, "keep")
				if err := os.WriteFile(marker, []byte("existing checkout"), 0o600); err != nil {
					t.Fatal(err)
				}
				environ = append(environ, "BUILDKITE_CLEAN_CHECKOUT=true", "BUILDKITE_BUILD_CHECKOUT_PATH="+checkoutDir)
				if err := tester.Run(t, environ...); err == nil {
					t.Fatal("checkout accepted a disallowed repository")
				}
				if !strings.Contains(tester.Output, "is not permitted by --allowed-repositories") {
					t.Fatalf("missing rejection: %s", tester.Output)
				}
				if source == "clear-policy" && !strings.Contains(tester.Output, "Changes to some env vars were blocked; the affected vars are: [BUILDKITE_ALLOWED_REPOSITORIES]") {
					t.Errorf("hook could change the policy environment: %s", tester.Output)
				}
				if _, err := os.Stat(marker); err != nil {
					t.Errorf("rejection removed existing checkout: %v", err)
				}
				tester.CheckMocks(t)
			})
		}
	}
}

func TestCheckoutRepositoryAllowlistPermitsHookRewrite(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("test hooks use POSIX shell syntax")
	}
	for _, checkout := range []string{"fresh", "existing", "local-mirror", "no-allowlist"} {
		t.Run(checkout, func(t *testing.T) {
			t.Parallel()
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			replacement, err := createTestGitRespository()
			if err != nil {
				t.Fatal(err)
			}
			defer replacement.Close()
			writeRepositoryHook(t, tester, "pre-checkout", fmt.Sprintf("export BUILDKITE_REPO='%s'", replacement.Path))
			environ := []string{"BUILDKITE_COMMIT_RESOLVED=true"}
			if checkout != "no-allowlist" {
				environ = append(environ, repositoryAllowlistEnv(`^https://example\.com/repo[0-9]+\.git$`, "^"+regexp.QuoteMeta(tester.Repo.Path)+"$", "^"+regexp.QuoteMeta(replacement.Path)+"$"))
			}
			if checkout == "local-mirror" {
				if err := tester.EnableGitMirrors(); err != nil {
					t.Fatal(err)
				}
			}
			if checkout == "existing" {
				// Seed a checkout with the original URL, then rerun with a hook rewrite.
				if err := os.Remove(filepath.Join(tester.HooksDir, "pre-checkout")); err != nil {
					t.Fatal(err)
				}
				tester.RunAndCheck(t, environ...)
				writeRepositoryHook(t, tester, "pre-checkout", fmt.Sprintf("export BUILDKITE_REPO='%s'", replacement.Path))
			}
			tester.RunAndCheck(t, environ...)
			if !strings.Contains(tester.Output, replacement.Path) {
				t.Fatalf("checkout did not use the replacement repository: %s", tester.Output)
			}
		})
	}
}

func TestCheckoutRepositoryAllowlistInvalidPolicy(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{`[`} {
		t.Run(policy, func(t *testing.T) {
			t.Parallel()
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			tester.MustMock(t, "git").Expect().NotCalled()
			if err := tester.Run(t, "BUILDKITE_ALLOWED_REPOSITORIES="+policy); err == nil {
				t.Fatal("bootstrap accepted an invalid repository policy")
			}
			if !strings.Contains(tester.Output, "allowed repositor") {
				t.Fatalf("missing invalid-policy error: %s", tester.Output)
			}
			tester.CheckMocks(t)
		})
	}
}

func TestCheckoutRepositoryAllowlistRemoteMirror(t *testing.T) {
	t.Parallel()
	for _, permitted := range []bool{false, true} {
		t.Run(fmt.Sprint(permitted), func(t *testing.T) {
			t.Parallel()
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			patterns := []string{"^" + regexp.QuoteMeta(tester.Repo.Path) + "$"}
			if permitted {
				patterns = append(patterns, `^https://mirror\.example/`)
			}
			tester.RunAndCheck(t,
				repositoryAllowlistEnv(patterns...),
				"BUILDKITE_GIT_REMOTE_MIRROR_URL=https://mirror.example/repo.git",
			)
			if dropped := strings.Contains(tester.Output, "Remote Git mirror is not permitted by --allowed-repositories"); dropped == permitted {
				t.Errorf("mirror dropped = %t, permitted = %t\n%s", dropped, permitted, tester.Output)
			}
		})
	}
}
