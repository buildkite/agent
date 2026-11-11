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

func TestCheckoutRepositoryAllowlistSubmodules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	for _, tc := range []struct {
		name, url                                string
		allow, mirrors, recursiveClone, override bool
	}{
		{name: "disallowed"},
		{name: "disallowed with mirrors", mirrors: true},
		{name: "recursive clone flags", recursiveClone: true},
		{name: "allowed", allow: true},
		{name: "allowed with mirrors", allow: true, mirrors: true},
		{name: "relative allowed", url: "relative", allow: true},
		{name: "rewritten to disallowed", url: "rewrite"},
		{name: "per-command URL override", allow: true, override: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			submodule, err := createTestGitRespository()
			if err != nil {
				t.Fatal(err)
			}
			defer submodule.Close()
			if out, err := tester.Repo.Execute("-c", "protocol.file.allow=always", "submodule", "add", submodule.Path, "outside"); err != nil {
				t.Fatalf("adding submodule: %v: %s", err, out)
			}
			if tc.url != "" {
				url := "../" + filepath.Base(submodule.Path)
				if tc.url == "rewrite" {
					url = "https://allowed.example/submodule"
					config := fmt.Sprintf("[url %q]\n\tinsteadOf = %s\n", submodule.Path, url)
					if err := os.WriteFile(filepath.Join(tester.HomeDir, ".gitconfig"), []byte(config), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if out, err := tester.Repo.Execute("config", "--file", ".gitmodules", "submodule.outside.url", url); err != nil {
					t.Fatalf("setting submodule URL: %v: %s", err, out)
				}
			}
			if tc.name == "allowed" {
				// A stale .gitmodules entry without a gitlink is not fetched by Git.
				for _, entry := range []string{"submodule.unused.path", "submodule.unused.url"} {
					if out, err := tester.Repo.Execute("config", "--file", ".gitmodules", entry, "unused"); err != nil {
						t.Fatalf("setting unused submodule entry: %v: %s", err, out)
					}
				}
			}
			if out, err := tester.Repo.Execute("commit", "-am", "Add test submodule"); err != nil {
				t.Fatalf("committing submodule: %v: %s", err, out)
			}
			if tc.mirrors {
				if err := tester.EnableGitMirrors(); err != nil {
					t.Fatal(err)
				}
			}
			patterns := []string{"^" + regexp.QuoteMeta(tester.Repo.Path) + "$"}
			if tc.allow {
				patterns = append(patterns, "^"+regexp.QuoteMeta(submodule.Path)+"$")
			}
			cloneConfig := "protocol.file.allow=always"
			if tc.override {
				other, err := createTestGitRespository()
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				cloneConfig += ",submodule.outside.url=" + other.Path
			}
			env := []string{repositoryAllowlistEnv(patterns...), "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=" + cloneConfig}
			if tc.recursiveClone {
				env = append(env, "BUILDKITE_GIT_CLONE_FLAGS=--recurse-submodules", "GIT_ALLOW_PROTOCOL=file")
			}
			err = tester.Run(t, env...)
			file := filepath.Join(tester.CheckoutDir(), "outside", "test.txt")
			if tc.allow && !tc.override {
				if err != nil {
					t.Fatalf("allowed submodule rejected: %v\n%s", err, tester.Output)
				}
				if _, err := os.Stat(file); err != nil {
					t.Fatalf("allowed submodule not fetched: %v\n%s", err, tester.Output)
				}
			} else {
				if err == nil || !strings.Contains(tester.Output, "is not permitted by --allowed-repositories") {
					t.Fatalf("checkout did not reject disallowed submodule: %v\n%s", err, tester.Output)
				}
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatalf("disallowed submodule was fetched: %v", err)
				}
				if tc.mirrors && strings.Contains(tester.Output, "git clone --mirror -- "+submodule.Path) {
					t.Fatal("disallowed submodule was mirrored")
				}
			}
		})
	}
}

func TestCheckoutRepositoryAllowlistNestedSubmodule(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	for _, tc := range []struct {
		name           string
		allow, mirrors bool
	}{
		{name: "nested disallowed"},
		{name: "nested disallowed with mirrors", mirrors: true},
		{name: "nested allowed", allow: true},
		{name: "nested allowed with mirrors", allow: true, mirrors: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			parent, err := createTestGitRespository()
			if err != nil {
				t.Fatal(err)
			}
			defer parent.Close()
			leaf, err := createTestGitRespository()
			if err != nil {
				t.Fatal(err)
			}
			defer leaf.Close()
			if out, err := parent.Execute("-c", "protocol.file.allow=always", "submodule", "add", leaf.Path, "inside"); err != nil {
				t.Fatalf("adding nested submodule: %v: %s", err, out)
			}
			if out, err := parent.Execute("config", "--file", ".gitmodules", "submodule.inside.url", "../"+filepath.Base(leaf.Path)); err != nil {
				t.Fatalf("setting relative nested URL: %v: %s", err, out)
			}
			if out, err := parent.Execute("commit", "-am", "Add nested submodule"); err != nil {
				t.Fatalf("committing nested submodule: %v: %s", err, out)
			}
			if out, err := tester.Repo.Execute("-c", "protocol.file.allow=always", "submodule", "add", parent.Path, "outside"); err != nil {
				t.Fatalf("adding parent submodule: %v: %s", err, out)
			}
			if out, err := tester.Repo.Execute("commit", "-am", "Add parent submodule"); err != nil {
				t.Fatalf("committing parent submodule: %v: %s", err, out)
			}
			if tc.mirrors {
				if err := tester.EnableGitMirrors(); err != nil {
					t.Fatal(err)
				}
			}
			patterns := []string{"^" + regexp.QuoteMeta(tester.Repo.Path) + "$", "^" + regexp.QuoteMeta(parent.Path) + "$"}
			if tc.allow {
				patterns = append(patterns, "^"+regexp.QuoteMeta(leaf.Path)+"$")
			}
			env := []string{repositoryAllowlistEnv(patterns...), "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always", "BUILDKITE_LOCAL_HOOKS_ENABLED=false"}
			if !tc.allow {
				env = append(env, "BUILDKITE_COMMAND_EVAL=false")
			}
			err = tester.Run(t, env...)
			parentFile := filepath.Join(tester.CheckoutDir(), "outside", "test.txt")
			leafFile := filepath.Join(tester.CheckoutDir(), "outside", "inside", "test.txt")
			if _, statErr := os.Stat(parentFile); statErr != nil {
				t.Fatalf("allowed parent was not fetched: %v\n%s", statErr, tester.Output)
			}
			if tc.allow {
				if err != nil {
					t.Fatalf("allowed nested submodule rejected: %v\n%s", err, tester.Output)
				}
				if _, statErr := os.Stat(leafFile); statErr != nil {
					t.Fatalf("allowed nested submodule was not fetched: %v\n%s", statErr, tester.Output)
				}
			} else {
				if err == nil || !strings.Contains(tester.Output, "is not permitted by --allowed-repositories") {
					t.Fatalf("nested submodule was not rejected: %v\n%s", err, tester.Output)
				}
				if _, statErr := os.Stat(leafFile); !os.IsNotExist(statErr) {
					t.Fatalf("disallowed nested submodule was fetched: %v", statErr)
				}
				if strings.Contains(tester.Output, "Attempt 2/6") {
					t.Fatal("policy rejection was retried")
				}
				if tc.mirrors && strings.Contains(tester.Output, "git clone --mirror -- "+leaf.Path) {
					t.Fatal("disallowed nested submodule was mirrored")
				}
			}
		})
	}
}

func TestCheckoutRepositoryAllowlistExistingSubmoduleFetch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()
	submodule, err := createTestGitRespository()
	if err != nil {
		t.Fatal(err)
	}
	defer submodule.Close()
	if out, err := tester.Repo.Execute("-c", "protocol.file.allow=always", "submodule", "add", submodule.Path, "outside"); err != nil {
		t.Fatalf("adding submodule: %v: %s", err, out)
	}
	if out, err := tester.Repo.Execute("commit", "-am", "Add submodule"); err != nil {
		t.Fatalf("committing submodule: %v: %s", err, out)
	}
	tester.RunAndCheck(t, repositoryAllowlistEnv("^"+regexp.QuoteMeta(tester.Repo.Path)+"$", "^"+regexp.QuoteMeta(submodule.Path)+"$"), "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always")
	if err := os.WriteFile(filepath.Join(submodule.Path, "test.txt"), []byte("second commit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := submodule.Add("test.txt"); err != nil {
		t.Fatal(err)
	}
	if err := submodule.Commit("Second commit"); err != nil {
		t.Fatal(err)
	}
	commit, err := submodule.Execute("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	commit = strings.TrimSpace(commit)
	if out, err := tester.Repo.Execute("-C", "outside", "fetch", "--", submodule.Path, "main"); err != nil {
		t.Fatalf("updating fixture submodule: %v: %s", err, out)
	}
	if out, err := tester.Repo.Execute("-C", "outside", "checkout", "-f", commit); err != nil {
		t.Fatalf("checking out fixture submodule: %v: %s", err, out)
	}
	if out, err := tester.Repo.Execute("commit", "-am", "Update submodule pointer"); err != nil {
		t.Fatalf("committing fixture pointer: %v: %s", err, out)
	}
	if err := tester.Run(t,
		repositoryAllowlistEnv("^"+regexp.QuoteMeta(tester.Repo.Path)+"$"),
		"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always",
		"BUILDKITE_GIT_FETCH_FLAGS=--recurse-submodules=on-demand",
		"GIT_ALLOW_PROTOCOL=file",
	); err == nil || !strings.Contains(tester.Output, "is not permitted by --allowed-repositories") {
		t.Fatalf("existing checkout did not reject submodule: %v\n%s", err, tester.Output)
	}
	if _, err := tester.Repo.Execute("-C", filepath.Join(tester.CheckoutDir(), "outside"), "cat-file", "-e", commit+"^{commit}"); err == nil {
		t.Fatal("primary repository fetch obtained a disallowed submodule commit")
	}
}
