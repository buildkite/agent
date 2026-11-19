package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/internal/job"
	"github.com/buildkite/agent/v4/internal/job/githttptest"
	"github.com/buildkite/bintest/v3"
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

func TestCheckoutRepositoryAllowlistRejectsHTTPPathResolution(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	s := githttptest.NewServer()
	defer s.Close()
	for _, name := range []string{"allowed", "outside"} {
		if err := s.CreateRepository(name); err != nil {
			t.Fatal(err)
		}
		if out, err := s.InitRepository(name); err != nil {
			t.Fatalf("initializing %s: %v: %s", name, err, out)
		}
		if err := s.SetDefaultBranch(name, "main"); err != nil {
			t.Fatal(err)
		}
	}
	policy := repositoryAllowlistEnv("^" + regexp.QuoteMeta(s.RepoURL("allowed")) + `(?:/|$)`)

	allowed, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer allowed.Close()
	allowed.RunAndCheck(t, policy, "BUILDKITE_REPO="+s.RepoURL("allowed"))
	if _, err := os.Stat(filepath.Join(allowed.CheckoutDir(), "README.md")); err != nil {
		t.Fatalf("allowed HTTP repository was not checked out: %v", err)
	}

	blocked, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	blocked.MustMock(t, "git").Expect().NotCalled()
	if err := blocked.Run(t, policy, "BUILDKITE_REPO="+s.RepoURL("allowed")+"/../outside.git"); err == nil {
		t.Fatal("checkout accepted a URL that resolves to the outside repository")
	}
	if !strings.Contains(blocked.Output, "is not permitted by --allowed-repositories") {
		t.Fatalf("missing allowlist rejection: %s", blocked.Output)
	}
	blocked.CheckMocks(t)
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
		name, url                                                                                           string
		allow, mirrors, recursiveClone, override, active, mirrorRewrite, skipMirror, parentRewrite, shallow bool
	}{
		{name: "disallowed"},
		{name: "disallowed with mirrors", mirrors: true},
		{name: "recursive clone flags", recursiveClone: true},
		{name: "recursive partial clone flags", recursiveClone: true, allow: true},
		{name: "per-command active submodule", active: true},
		{name: "per-command mirror rewrite", mirrors: true, mirrorRewrite: true},
		{name: "allowed", allow: true},
		{name: "allowed with mirrors", allow: true, mirrors: true},
		{name: "allowed with skipped missing mirror", allow: true, mirrors: true, skipMirror: true},
		{name: "shallow pinned commit", allow: true, shallow: true},
		{name: "shallow pinned commit with mirrors", allow: true, shallow: true, mirrors: true},
		{name: "shallow pinned commit with missing mirror", allow: true, shallow: true, mirrors: true, skipMirror: true},
		{name: "relative allowed", url: "relative", allow: true},
		{name: "rewritten to disallowed", url: "rewrite"},
		{name: "per-command URL override", allow: true, override: true},
		{name: "parent-only rewrite", parentRewrite: true},
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
			if tc.shallow {
				// file:// makes Git honor shallow cloning, unlike a local path.
				for key, value := range map[string]string{"url": "file://" + submodule.Path, "shallow": "true"} {
					if out, err := tester.Repo.Execute("config", "--file", ".gitmodules", "submodule.outside."+key, value); err != nil {
						t.Fatalf("configuring shallow submodule: %v: %s", err, out)
					}
				}
				if err := os.WriteFile(filepath.Join(submodule.Path, "test.txt"), []byte("new tip, not the pinned commit"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := submodule.Add("test.txt"); err != nil {
					t.Fatal(err)
				}
				if err := submodule.Commit("Advance past pinned commit"); err != nil {
					t.Fatal(err)
				}
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
				if tc.skipMirror {
					if err := os.Remove(tester.GitMirrorsDir); err != nil {
						t.Fatal(err)
					}
				}
			}
			patterns := []string{"^" + regexp.QuoteMeta(tester.Repo.Path) + "$"}
			if tc.allow {
				patterns = append(patterns, "^(?:file://)?"+regexp.QuoteMeta(submodule.Path)+"$")
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
			if tc.active {
				if err := os.WriteFile(filepath.Join(tester.HomeDir, ".gitconfig"), []byte("[submodule]\n\tactive = selected\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				cloneConfig += ",submodule.active=outside"
			}
			if tc.mirrorRewrite {
				other, err := createTestGitRespository()
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				patterns = append(patterns, "^"+regexp.QuoteMeta(other.Path)+"$")
				cloneConfig += ",url." + other.Path + ".insteadOf=" + submodule.Path
			}
			if tc.parentRewrite {
				other, err := createTestGitRespository()
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				patterns = append(patterns, "^"+regexp.QuoteMeta(other.Path)+"$")
				tester.RunAndCheck(t, repositoryAllowlistEnv(patterns...), "BUILDKITE_GIT_SUBMODULES=false")
				// A clone does not inherit this parent-local rewrite. Checking
				// it in the parent would approve a source the child won't use.
				if out, err := tester.Repo.Execute("-C", tester.CheckoutDir(), "config", "url."+other.Path+".insteadOf", submodule.Path); err != nil {
					t.Fatalf("configuring parent rewrite: %v: %s", err, out)
				}
			}
			env := []string{repositoryAllowlistEnv(patterns...), "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=" + cloneConfig}
			if tc.shallow {
				env = append(env, "BUILDKITE_CHECKOUT_ATTEMPTS=1")
			}
			if tc.skipMirror {
				env = append(env, "BUILDKITE_GIT_MIRRORS_SKIP_UPDATE=true")
			}
			if tc.recursiveClone {
				flags := "--recurse-submodules"
				if tc.name == "recursive partial clone flags" {
					flags += " --filter=blob:none --also-filter-submodules"
				}
				env = append(env, "BUILDKITE_GIT_CLONE_FLAGS="+flags, "GIT_ALLOW_PROTOCOL=file")
			}
			err = tester.Run(t, env...)
			file := filepath.Join(tester.CheckoutDir(), "outside", "test.txt")
			if (tc.allow && !tc.override) || tc.mirrorRewrite {
				if err != nil {
					t.Fatalf("allowed submodule rejected: %v\n%s", err, tester.Output)
				}
				if _, err := os.Stat(file); err != nil {
					t.Fatalf("allowed submodule not fetched: %v\n%s", err, tester.Output)
				}
				if tc.shallow {
					want, err := os.ReadFile(filepath.Join(tester.Repo.Path, "outside", "test.txt"))
					if err != nil {
						t.Fatal(err)
					}
					if got, err := os.ReadFile(file); err != nil || string(got) != string(want) {
						t.Fatalf("pinned submodule contents = %q, %v; want %q", got, err, want)
					}
				}
				if tc.mirrorRewrite && strings.Contains(tester.Output, "git clone --mirror -- "+submodule.Path) {
					t.Fatal("mirror fetched the disallowed submodule source")
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

func TestCheckoutRepositoryAllowlistSubmoduleConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	for _, source := range []string{"conditional clone rewrite", "remote-URL clone rewrite", "config-only URL"} {
		for _, allow := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/allowed=%t", source, allow), func(t *testing.T) {
				tester, err := NewExecutorTester(mainCtx)
				if err != nil {
					t.Fatal(err)
				}
				defer tester.Close()
				approved, err := createTestGitRespository()
				if err != nil {
					t.Fatal(err)
				}
				defer approved.Close()
				blocked, err := createTestGitRespository()
				if err != nil {
					t.Fatal(err)
				}
				defer blocked.Close()
				cloneConfig := "protocol.file.allow=always"
				for name, url := range map[string]string{"inside": approved.Path, "outside": blocked.Path} {
					if source != "config-only URL" {
						url = approved.Path
					}
					if out, err := tester.Repo.Execute("-c", "protocol.file.allow=always", "submodule", "add", url, name); err != nil {
						t.Fatalf("adding submodule: %v: %s", err, out)
					}
				}
				if source == "config-only URL" {
					if out, err := tester.Repo.Execute("config", "--file", ".gitmodules", "--unset", "submodule.outside.url"); err != nil {
						t.Fatalf("removing submodule URL: %v: %s", err, out)
					}
					cloneConfig += ",submodule.outside.url=" + blocked.Path
				} else {
					rewrite := filepath.Join(t.TempDir(), "rewrite.config")
					if err := os.WriteFile(rewrite, []byte(fmt.Sprintf("[url %q]\n\tinsteadOf = %s\n", blocked.Path, approved.Path)), 0o600); err != nil {
						t.Fatal(err)
					}
					condition := "gitdir:**/modules/**"
					if source == "remote-URL clone rewrite" {
						condition = "hasconfig:remote.*.url:" + approved.Path
					}
					if err := os.WriteFile(filepath.Join(tester.HomeDir, ".gitconfig"), []byte(fmt.Sprintf("[includeIf %q]\n\tpath = %s\n", condition, rewrite)), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if out, err := tester.Repo.Execute("commit", "-am", "Configure submodules"); err != nil {
					t.Fatalf("committing submodules: %v: %s", err, out)
				}
				patterns := []string{"^" + regexp.QuoteMeta(tester.Repo.Path) + "$", "^" + regexp.QuoteMeta(approved.Path) + "$"}
				if allow {
					patterns = append(patterns, "^"+regexp.QuoteMeta(blocked.Path)+"$")
				}
				traceFile := filepath.Join(t.TempDir(), "git-trace")
				err = tester.Run(t, repositoryAllowlistEnv(patterns...), "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG="+cloneConfig, "GIT_TRACE="+traceFile, "BUILDKITE_CHECKOUT_ATTEMPTS=1")
				if allow {
					if err != nil {
						t.Fatalf("allowed configured source rejected: %v\n%s", err, tester.Output)
					}
					if _, err := os.Stat(filepath.Join(tester.CheckoutDir(), "outside", "test.txt")); err != nil {
						t.Fatalf("configured submodule not checked out: %v", err)
					}
					return
				}
				if err == nil || !strings.Contains(tester.Output, "is not permitted by --allowed-repositories") {
					t.Errorf("disallowed configured source was not rejected: %v\n%s", err, tester.Output)
				}
				trace, err := os.ReadFile(traceFile)
				if err != nil {
					t.Fatal(err)
				}
				for line := range strings.SplitSeq(string(trace), "\n") {
					if strings.Contains(line, "git-upload-pack") && strings.Contains(line, blocked.Path) {
						t.Fatalf("submodule contacted a blocked source:\n%s", trace)
					}
				}
			})
		}
	}
}

func TestCheckoutRepositoryAllowlistSubmoduleActivity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	for _, tc := range []struct {
		name, config    string
		reused, mirrors bool
		active          bool
	}{
		{name: "explicit false overrides matching pathspec", config: "submodule.active=modules/**,submodule.modules/java-prettify.active=off"},
		{name: "pathspec excludes populated child", config: "submodule.active=modules/**,submodule.active=:(exclude)modules/java-prettify", reused: true},
		{name: "inactive populated child with mirrors", config: "submodule.modules/java-prettify.active=false", reused: true, mirrors: true},
		{name: "explicit true overrides excluding pathspec", config: "submodule.active=not-selected,submodule.modules/java-prettify.active=true", mirrors: true, active: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			if tc.mirrors {
				if err := tester.EnableGitMirrors(); err != nil {
					t.Fatal(err)
				}
			}
			parent, child := setupRelativeSubmoduleFixture(t, tester)
			patterns := []string{"^" + regexp.QuoteMeta(parent.Path) + "$"}
			if tc.reused {
				tester.MockAgent(t).Expect("meta-data", "exists", job.CommitMetadataKey).Min(0).Max(bintest.InfiniteTimes).AndExitWith(0)
				tester.RunAndCheck(t,
					repositoryAllowlistEnv(append(patterns, "^"+regexp.QuoteMeta(child.Path)+"$")...),
					"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always",
				)
				// init records active=true locally; remove it to allow a later
				// pathspec selection to take effect while retaining the URL.
				if out, err := tester.Repo.Execute("-C", tester.CheckoutDir(), "config", "--unset", "submodule.modules/java-prettify.active"); err != nil {
					t.Fatalf("removing previous activity selection: %v: %s", err, out)
				}
			}
			if tc.active {
				patterns = append(patterns, "^"+regexp.QuoteMeta(child.Path)+"$")
			}
			tester.RunAndCheck(t,
				repositoryAllowlistEnv(patterns...),
				"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always,submodule.modules/java-prettify.url="+child.Path+","+tc.config,
				"BUILDKITE_CHECKOUT_ATTEMPTS=1",
			)
			requireCheckoutPath(t, tester.CheckoutDir(), "superproject.txt", true)
			requireCheckoutPath(t, tester.CheckoutDir(), "modules/java-prettify/submodule.txt", tc.reused || tc.active)
		})
	}
}

func TestCheckoutRepositoryAllowlistReusedSubmoduleMirrorRewrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	for _, source := range []string{"local config", "conditional include", "fresh conditional include"} {
		t.Run(source, func(t *testing.T) {
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			approved, err := createTestGitRespository()
			if err != nil {
				t.Fatal(err)
			}
			defer approved.Close()
			blocked, err := createTestGitRespository()
			if err != nil {
				t.Fatal(err)
			}
			defer blocked.Close()
			if out, err := tester.Repo.Execute("-c", "protocol.file.allow=always", "submodule", "add", approved.Path, "outside"); err != nil {
				t.Fatalf("adding submodule: %v: %s", err, out)
			}
			if out, err := tester.Repo.Execute("commit", "-am", "Add submodule"); err != nil {
				t.Fatalf("committing submodule: %v: %s", err, out)
			}
			if err := tester.EnableGitMirrors(); err != nil {
				t.Fatal(err)
			}
			tester.MockAgent(t).Expect("meta-data", "exists", job.CommitMetadataKey).Min(0).Max(bintest.InfiniteTimes).AndExitWith(0)
			env := []string{
				repositoryAllowlistEnv("^"+regexp.QuoteMeta(tester.Repo.Path)+"$", "^"+regexp.QuoteMeta(approved.Path)+"$"),
				"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always",
				"BUILDKITE_CHECKOUT_ATTEMPTS=1",
			}
			tester.RunAndCheck(t, env...)
			configs, err := filepath.Glob(filepath.Join(tester.GitMirrorsDir, "*", "config"))
			if err != nil {
				t.Fatal(err)
			}
			var mirror string
			for _, config := range configs {
				dir := filepath.Dir(config)
				url, err := tester.Repo.Execute("--git-dir", dir, "config", "--get", "remote.origin.url")
				if err != nil {
					t.Fatalf("reading mirror origin: %v: %s", err, url)
				}
				if strings.TrimSpace(url) == approved.Path {
					mirror = dir
				}
			}
			if mirror == "" {
				t.Fatal("submodule mirror was not created")
			}
			if source == "local config" {
				if out, err := tester.Repo.Execute("--git-dir", mirror, "config", "url."+blocked.Path+".insteadOf", approved.Path); err != nil {
					t.Fatalf("setting mirror rewrite: %v: %s", err, out)
				}
			} else {
				rewrite := filepath.Join(t.TempDir(), "rewrite.config")
				if err := os.WriteFile(rewrite, []byte(fmt.Sprintf("[url %q]\n\tinsteadOf = %s\n", blocked.Path, approved.Path)), 0o600); err != nil {
					t.Fatal(err)
				}
				config := fmt.Sprintf("[includeIf %q]\n\tpath = %s\n", "gitdir:"+mirror, rewrite)
				if err := os.WriteFile(filepath.Join(tester.HomeDir, ".gitconfig"), []byte(config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Force a fresh worktree while retaining the configured mirror.
			if err := os.RemoveAll(tester.CheckoutDir()); err != nil {
				t.Fatal(err)
			}
			if source == "fresh conditional include" {
				if err := os.RemoveAll(mirror); err != nil {
					t.Fatal(err)
				}
			}
			traceFile := filepath.Join(t.TempDir(), "git-trace")
			tester.RunAndCheck(t, append(env, "GIT_TRACE="+traceFile)...)
			if _, err := os.Stat(filepath.Join(tester.CheckoutDir(), "outside", "test.txt")); err != nil {
				t.Fatalf("direct submodule checkout failed: %v\n%s", err, tester.Output)
			}
			trace, err := os.ReadFile(traceFile)
			if err != nil {
				t.Fatal(err)
			}
			for line := range strings.SplitSeq(string(trace), "\n") {
				if strings.Contains(line, "git-upload-pack") && strings.Contains(line, blocked.Path) {
					t.Fatalf("reused submodule mirror contacted a blocked source:\n%s", trace)
				}
			}
		})
	}
}

func TestCheckoutRepositoryAllowlistRelativeSubmoduleMirrorFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	tester, err := NewExecutorTester(mainCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer tester.Close()
	if err := tester.EnableGitMirrors(); err != nil {
		t.Fatal(err)
	}
	parent, child := setupRelativeSubmoduleFixture(t, tester)
	git := tester.MustMock(t, "git").PassthroughToLocalCommand().Before(func(i bintest.Invocation) error {
		if len(i.Args) >= 4 && i.Args[0] == "clone" && i.Args[1] == "--mirror" && i.Args[len(i.Args)-2] == child.Path {
			return fmt.Errorf("submodule mirror temporarily unavailable")
		}
		return nil
	})
	git.Expect().AtLeastOnce().WithAnyArguments()
	tester.RunAndCheck(t,
		repositoryAllowlistEnv("^"+regexp.QuoteMeta(parent.Path)+"$", "^"+regexp.QuoteMeta(child.Path)+"$"),
		"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always",
		"BUILDKITE_CHECKOUT_ATTEMPTS=1",
	)
	contents, err := os.ReadFile(filepath.Join(tester.CheckoutDir(), "modules", "java-prettify", "submodule.txt"))
	if err != nil || string(contents) != "submodule" {
		t.Fatalf("direct submodule checkout contents = %q, %v; want submodule", contents, err)
	}
}

func TestCheckoutRepositoryAllowlistManagedSubmoduleRewrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	for _, state := range []string{"fresh", "populated", "mirror", "different repository"} {
		t.Run(state, func(t *testing.T) {
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
			if state == "populated" {
				tester.RunAndCheck(t, "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always")
			}
			if state == "mirror" {
				if err := tester.EnableGitMirrors(); err != nil {
					t.Fatal(err)
				}
			}
			// Resolve the managed URL for real, but prohibit the HTTPS transport
			// itself. This tests policy acceptance without contacting GitHub or
			// requesting real credentials, even outside the isolated test runner.
			config := "[protocol \"https\"]\n\tallow = never\n"
			if state == "different repository" {
				config += "[url \"https://github.com/other/repo.git\"]\n\tinsteadOf = git@github.com:org/repo.git\n"
			}
			if err := os.WriteFile(filepath.Join(tester.HomeDir, ".gitconfig"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			err = tester.Run(t,
				repositoryAllowlistEnv("^"+regexp.QuoteMeta(tester.Repo.Path)+"$", `^git@github\.com:org/`),
				"BUILDKITE_USE_GITHUB_APP_GIT_CREDENTIALS=true",
				"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=submodule.outside.url=git@github.com:org/repo.git",
				"BUILDKITE_CHECKOUT_ATTEMPTS=1",
			)
			if err == nil {
				t.Fatal("checkout unexpectedly used prohibited HTTPS transport")
			}
			if state == "different repository" {
				if !strings.Contains(tester.Output, "is not permitted by --allowed-repositories") || strings.Contains(tester.Output, "transport 'https' not allowed") {
					t.Fatalf("different repository rewrite was not rejected before transport: %s", tester.Output)
				}
			} else if strings.Contains(tester.Output, "is not permitted by --allowed-repositories") || !strings.Contains(tester.Output, "transport 'https' not allowed") {
				t.Fatalf("managed rewrite did not pass policy validation: %s", tester.Output)
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
				tester.MockAgent(t).Expect("meta-data", "exists", job.CommitMetadataKey).Min(0).Max(bintest.InfiniteTimes).AndExitWith(0)
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
				// On reuse, a global recurse setting must not make the parent's
				// checkout reach descendants before their own checked fetches.
				if err := os.WriteFile(filepath.Join(tester.HomeDir, ".gitconfig"), []byte("[submodule]\n\trecurse = true\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(leaf.Path, "test.txt"), []byte("updated nested commit"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := leaf.Add("test.txt"); err != nil {
					t.Fatal(err)
				}
				if err := leaf.Commit("Advance nested commit"); err != nil {
					t.Fatal(err)
				}
				for _, repo := range []*gitRepository{parent, tester.Repo} {
					if out, err := repo.Execute("-c", "protocol.file.allow=always", "submodule", "update", "--remote"); err != nil {
						t.Fatalf("updating fixture pointer: %v: %s", err, out)
					}
					if out, err := repo.Execute("add", "-u"); err != nil {
						t.Fatalf("staging fixture pointer: %v: %s", err, out)
					}
					if err := repo.Commit("Advance submodule pointer"); err != nil {
						t.Fatal(err)
					}
				}
				tester.RunAndCheck(t, append(env, "BUILDKITE_CHECKOUT_ATTEMPTS=1")...)
				if got, err := os.ReadFile(leafFile); err != nil || string(got) != "updated nested commit" {
					t.Fatalf("updated nested contents = %q, %v", got, err)
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
	parentCommit, err := tester.Repo.Execute("rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := tester.Run(t,
		repositoryAllowlistEnv("^"+regexp.QuoteMeta(tester.Repo.Path)+"$"),
		"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always",
		"BUILDKITE_GIT_FETCH_FLAGS=--recurse-submodules=on-demand",
		"BUILDKITE_COMMIT="+strings.TrimSpace(parentCommit),
		"GIT_ALLOW_PROTOCOL=file",
	); err == nil || !strings.Contains(tester.Output, "is not permitted by --allowed-repositories") {
		t.Fatalf("verified checkout did not reject submodule: %v\n%s", err, tester.Output)
	}
	if _, err := tester.Repo.Execute("-C", filepath.Join(tester.CheckoutDir(), "outside"), "cat-file", "-e", commit+"^{commit}"); err == nil {
		t.Fatal("commit verification fetched a disallowed submodule commit")
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

func TestCheckoutRepositoryAllowlistExistingSubmoduleURLChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Git submodule fixture")
	}
	for _, state := range []string{"origin", "upstream", "tag collision", "parent default retry", "mirrors", "available", "unrestricted"} {
		t.Run(state, func(t *testing.T) {
			tester, err := NewExecutorTester(mainCtx)
			if err != nil {
				t.Fatal(err)
			}
			defer tester.Close()
			tester.MockAgent(t).Expect("meta-data", "exists", job.CommitMetadataKey).Min(0).Max(bintest.InfiniteTimes).AndExitWith(0)
			allowed, err := createTestGitRespository()
			if err != nil {
				t.Fatal(err)
			}
			defer allowed.Close()
			blocked, err := createTestGitRespository()
			if err != nil {
				t.Fatal(err)
			}
			defer blocked.Close()
			if out, err := tester.Repo.Execute("-c", "protocol.file.allow=always", "submodule", "add", allowed.Path, "outside"); err != nil {
				t.Fatalf("adding submodule: %v: %s", err, out)
			}
			if out, err := tester.Repo.Execute("commit", "-am", "Add allowed submodule"); err != nil {
				t.Fatalf("committing submodule: %v: %s", err, out)
			}
			policy := repositoryAllowlistEnv("^"+regexp.QuoteMeta(tester.Repo.Path)+"$", "^"+regexp.QuoteMeta(allowed.Path)+"$")
			tester.RunAndCheck(t, policy, "BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always")
			if err := os.WriteFile(filepath.Join(blocked.Path, "test.txt"), []byte("new blocked commit"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := blocked.Add("test.txt"); err != nil {
				t.Fatal(err)
			}
			if err := blocked.Commit("New blocked commit"); err != nil {
				t.Fatal(err)
			}
			commit, err := blocked.Execute("rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			commit = strings.TrimSpace(commit)
			if out, err := tester.Repo.Execute("config", "--file", ".gitmodules", "submodule.outside.url", blocked.Path); err != nil {
				t.Fatalf("changing submodule URL: %v: %s", err, out)
			}
			if out, err := tester.Repo.Execute("-C", "outside", "fetch", "--", blocked.Path, "main"); err != nil {
				t.Fatalf("fetching fixture commit: %v: %s", err, out)
			}
			if out, err := tester.Repo.Execute("-C", "outside", "checkout", "-f", commit); err != nil {
				t.Fatalf("checking out fixture commit: %v: %s", err, out)
			}
			if out, err := tester.Repo.Execute("commit", "-am", "Change submodule source and pointer"); err != nil {
				t.Fatalf("committing fixture pointer: %v: %s", err, out)
			}
			child := filepath.Join(tester.CheckoutDir(), "outside")
			git := func(args ...string) {
				t.Helper()
				if out, err := tester.Repo.Execute(append([]string{"-C", child}, args...)...); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			if state != "origin" {
				git("checkout", "-B", "main")
				git("remote", "add", "other", blocked.Path)
				git("config", "branch.main.remote", "other")
				git("config", "branch.main.merge", "refs/heads/main")
			}
			if state != "origin" && state != "upstream" {
				git("tag", "main")
			}
			if state == "parent default retry" {
				// The first fetch's upstream is allowed but lacks the commit;
				// Git's direct-SHA retry would use the blocked origin instead.
				git("remote", "set-url", "other", allowed.Path)
			}
			if state == "mirrors" {
				if err := tester.EnableGitMirrors(); err != nil {
					t.Fatal(err)
				}
				git("remote", "set-url", "other", blocked.Path)
			}
			if state == "available" {
				if out, err := allowed.Execute("fetch", "--", blocked.Path, commit); err != nil {
					t.Fatalf("making commit available at approved URL: %v: %s", err, out)
				}
			}
			if state == "unrestricted" {
				policy = repositoryAllowlistEnv()
			}
			traceFile := filepath.Join(t.TempDir(), "git-trace")
			err = tester.Run(t, policy,
				"BUILDKITE_GIT_SUBMODULE_CLONE_CONFIG=protocol.file.allow=always,submodule.outside.url="+allowed.Path,
				"BUILDKITE_CHECKOUT_ATTEMPTS=1",
				"GIT_TRACE="+traceFile,
				"GIT_ALLOW_PROTOCOL=file",
			)
			if state == "available" || state == "unrestricted" {
				if err != nil {
					t.Fatalf("checkout rejected available commit: %v\n%s", err, tester.Output)
				}
				if contents, err := os.ReadFile(filepath.Join(child, "test.txt")); err != nil || string(contents) != "new blocked commit" {
					t.Fatalf("commit contents = %q, %v", contents, err)
				}
			} else if err == nil || !strings.Contains(tester.Output, "fetching submodule") {
				t.Fatalf("checkout did not fail fetching from the approved source: %v\n%s", err, tester.Output)
			}
			trace, err := os.ReadFile(traceFile)
			if err != nil {
				t.Fatal(err)
			}
			contactedBlocked := false
			for line := range strings.SplitSeq(string(trace), "\n") {
				if strings.Contains(line, "git-upload-pack") && strings.Contains(line, blocked.Path) {
					contactedBlocked = true
				}
			}
			if contactedBlocked != (state == "unrestricted") {
				t.Fatalf("contacted blocked repository = %t, state = %s\n%s", contactedBlocked, state, trace)
			}
			tester.CheckMocks(t)
		})
	}
}
