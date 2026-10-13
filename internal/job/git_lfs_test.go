package job

import (
	"testing"

	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/google/go-cmp/cmp"
)

func TestGitLFSFetchCheckout(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{
		Shell: sh,
		Retry: true,
	}); err != nil {
		t.Fatalf("gitLFSFetchCheckout(ctx, ...) = %v", err)
	}

	wantLog := [][]string{
		{absoluteGit, "lfs", "fetch"},
		{absoluteGit, "lfs", "checkout"},
	}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitLFSFetchCheckoutWithInclude(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	if err := gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{
		Shell:        sh,
		Retry:        true,
		FetchInclude: []string{"src/", "docs/"},
	}); err != nil {
		t.Fatalf("gitLFSFetchCheckout(ctx, ...) = %v", err)
	}

	wantLog := [][]string{
		{absoluteGit, "lfs", "fetch", "--include=src/,docs/"},
		{absoluteGit, "lfs", "checkout", "src/", "docs/"},
	}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitLFSFetchCheckoutWithCheckoutPathsOverride(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	checkoutPaths := []string{"src/a.bin", "lib/b.bin"}
	if err := gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{
		Shell:         sh,
		Retry:         true,
		CheckoutPaths: &checkoutPaths,
	}); err != nil {
		t.Fatalf("gitLFSFetchCheckout(ctx, ...) = %v", err)
	}

	wantLog := [][]string{
		{absoluteGit, "lfs", "fetch"},
		{absoluteGit, "lfs", "checkout", "src/a.bin", "lib/b.bin"},
	}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}

func TestGitLFSFetchCheckoutWithEmptyCheckoutPathsSkipsCheckout(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	var gotLog [][]string
	sh := shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))

	absoluteGit, err := sh.AbsolutePath("git")
	if err != nil {
		t.Fatalf("sh.AbsolutePath(git) = %v", err)
	}

	checkoutPaths := []string{}
	if err := gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{
		Shell:         sh,
		Retry:         true,
		CheckoutPaths: &checkoutPaths,
	}); err != nil {
		t.Fatalf("gitLFSFetchCheckout(ctx, ...) = %v", err)
	}

	wantLog := [][]string{
		{absoluteGit, "lfs", "fetch"},
	}
	if diff := cmp.Diff(gotLog, wantLog); diff != "" {
		t.Errorf("executed commands diff (-got +want):\n%s", diff)
	}
}
