package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/roko"
)

// gitLFSCheckoutPathBatchSize bounds pathspecs passed to a single
// `git lfs checkout` invocation so large sparse working trees don't hit ARG_MAX.
const gitLFSCheckoutPathBatchSize = 1000

type gitLFSFetchCheckoutArgs struct {
	Shell *shell.Shell
	Retry bool // Whether to retry the fetch+checkout on failure
	// ReferenceDir supplies cached LFS objects, including for dissociated clones
	// and clones referencing disposable Git mirror snapshots.
	ReferenceDir string
	// FetchInclude is passed as --include=<csv> to `git lfs fetch`. Empty means
	// fetch all LFS objects.
	FetchInclude []string
	// CheckoutPaths, when non-nil, are pathspecs for `git lfs checkout`
	// (overriding FetchInclude). A non-nil empty slice means there is nothing
	// to materialise. When nil, checkout uses FetchInclude if set, otherwise
	// all LFS objects.
	CheckoutPaths *[]string
}

// gitLFSFetchCheckout fetches LFS objects for the current HEAD then materialises
// them. Fetch and checkout failures are wrapped with distinct messages so that a
// caller can tell which step failed from the error string alone.
//
// When args.Retry is true, the fetch+checkout pair is retried with exponential
// backoff to ride out transient network errors talking to the LFS server.
// Unlike gitFetch, we don't smelt for specific error strings: git-lfs uses
// different exit codes and error vocabulary than git itself, so we retry
// indiscriminately on any failure and rely on the retry budget to bound the
// damage from a genuinely permanent error.
//
// On exhaustion, the error is wrapped as a *gitError with WasRetried=true so
// that the outer checkout retrier in defaultCheckoutPhase's caller does not
// loop on top of this one — without that signal, a permanent LFS failure
// could be attempted 6 × 5 = 30 times instead of 5.
func gitLFSFetchCheckout(ctx context.Context, args gitLFSFetchCheckoutArgs) error {
	retrier := roko.NewRetrier(
		roko.WithStrategy(roko.Constant(0)),
		roko.WithMaxAttempts(1),
	)

	if args.Retry {
		retrier = roko.NewRetrier(
			roko.WithStrategy(roko.ExponentialSubsecond(1*time.Second)),
			roko.WithMaxAttempts(5), // 5 attempts will take ~16s
			roko.WithJitter(),
		)
	}

	var runOpts []shell.RunCommandOpt
	if args.ReferenceDir != "" {
		objects := filepath.Join(args.ReferenceDir, "objects")
		if existing, ok := args.Shell.Env.Get("GIT_ALTERNATE_OBJECT_DIRECTORIES"); ok && existing != "" {
			objects += string(os.PathListSeparator) + existing
		}
		runOpts = append(runOpts, shell.WithExtraEnv(env.FromSlice([]string{"GIT_ALTERNATE_OBJECT_DIRECTORIES=" + objects})))
	}

	fetchCmd := []string{"lfs", "fetch"}
	if len(args.FetchInclude) > 0 {
		fetchCmd = append(fetchCmd, "--include="+strings.Join(args.FetchInclude, ","))
	}

	checkoutPathspecs, checkoutScoped := args.checkoutPathspecs()

	err := retrier.DoWithContext(ctx, func(retrier *roko.Retrier) error {
		if err := args.Shell.Command("git", fetchCmd...).Run(ctx, runOpts...); err != nil {
			if args.Retry {
				args.Shell.Commentf("%s", retrier)
			}
			return fmt.Errorf("git lfs fetch: %w", err)
		}
		if checkoutScoped && len(checkoutPathspecs) == 0 {
			return nil
		}
		if !checkoutScoped {
			if err := args.Shell.Command("git", "lfs", "checkout").Run(ctx, runOpts...); err != nil {
				if args.Retry {
					args.Shell.Commentf("%s", retrier)
				}
				return fmt.Errorf("git lfs checkout: %w", err)
			}
			return nil
		}
		for batch := range slices.Chunk(checkoutPathspecs, gitLFSCheckoutPathBatchSize) {
			checkoutCmd := append([]string{"lfs", "checkout"}, batch...)
			if err := args.Shell.Command("git", checkoutCmd...).Run(ctx, runOpts...); err != nil {
				if args.Retry {
					args.Shell.Commentf("%s", retrier)
				}
				return fmt.Errorf("git lfs checkout: %w", err)
			}
		}
		return nil
	})

	if err != nil && args.Retry {
		return &gitError{error: err, Type: gitErrorLFS, WasRetried: args.Retry}
	}
	return err
}

// checkoutPathspecs returns the pathspecs for `git lfs checkout` and whether
// checkout is scoped. When scoped is false, checkout should materialise every
// LFS object. When scoped is true, only the returned pathspecs are materialised
// (and an empty list means nothing to materialise).
func (args gitLFSFetchCheckoutArgs) checkoutPathspecs() (paths []string, scoped bool) {
	if args.CheckoutPaths != nil {
		return *args.CheckoutPaths, true
	}
	if len(args.FetchInclude) > 0 {
		return args.FetchInclude, true
	}
	return nil, false
}
