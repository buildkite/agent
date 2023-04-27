package hook_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/buildkite/agent/v3/hook"
)

type testCase struct {
	name             string
	hookPath         []string
	expectedHookType string
	errCheck         func(*testing.T, error) bool
}

func noErr(t *testing.T, err error) bool {
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return true
	}
	return false
}

func isNotExist(t *testing.T, err error) bool {
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
		return true
	}

	return false
}

func TestHookType(t *testing.T) {
	// The test working dir is at $REPO_ROOT/hook, but the fixtures are in $REPO_ROOT/test/fixtures/hook, so we need to
	// go up a directory to get to the root
	wd, _ := os.Getwd()
	root := filepath.Join(wd, "..")

	cases := []testCase{
		{
			name:             "non-shell script with shebang",
			hookPath:         hookFixture(root, "hook.rb"),
			expectedHookType: hook.TypeScript,
			errCheck:         noErr,
		},
		{
			name:             "shell script with shebang",
			hookPath:         hookFixture(root, "shebanged.sh"),
			expectedHookType: hook.TypeShell,
			errCheck:         noErr,
		},
		{
			name:             "shell script without shebang",
			hookPath:         hookFixture(root, "un-shebanged.sh"),
			expectedHookType: hook.TypeShell,
			errCheck:         noErr,
		},
		{
			name:             "non-existent hook",
			hookPath:         hookFixture(root, "some", "path", "that", "doesnt", "exist"),
			expectedHookType: hook.TypeUnknown,
			errCheck:         isNotExist,
		},
	}

	for _, os := range []string{"linux", "darwin", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			cases = append(cases, testCase{
				name:             fmt.Sprintf("binary for %s/%s", os, arch),
				hookPath:         hookFixture(root, "binaries", fmt.Sprintf("test-binary-%s-%s", os, arch)),
				expectedHookType: hook.TypeBinary,
				errCheck:         noErr,
			})
		}
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(c.hookPath...)
			hookType, err := hook.Type(path)
			if c.errCheck(t, err) {
				t.Fatalf("error check failed")
			}

			if hookType != c.expectedHookType {
				t.Fatalf("Expected hook type %q, got %q", c.expectedHookType, hookType)
			}
		})
	}
}

func hookFixture(wd string, fixturePath ...string) []string {
	return append([]string{wd, "test", "fixtures", "hook"}, fixturePath...)
}
