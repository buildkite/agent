package job

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// fetchAndCheckoutLFS fills the persistent mirror's LFS cache before
// materialising the checkout. Skip-update mirrors may be read-only, so leave
// their storage untouched and use the normal checkout-local fetch instead.
func (e *Executor) fetchAndCheckoutLFS(ctx context.Context, mirrorDir string, args gitLFSFetchCheckoutArgs) (retErr error) {
	if mirrorDir == "" || e.GitMirrorsSkipUpdate {
		return gitLFSFetchCheckout(ctx, args)
	}

	// mirrorDir may be a disposable clean-checkout snapshot. Cache downloads
	// in the original mirror so subsequent jobs can reuse them.
	sharedMirrorDir := filepath.Join(e.GitMirrorsPath, dirForRepository(e.Repository))
	// Explicit storage can live outside mirror/lfs, where LFS alternates
	// cannot discover it. Preserve existing behavior for either repository.
	for _, gitArgs := range [][]string{
		{"config", "--get", "--default", "", "lfs.storage"},
		{"--git-dir=" + sharedMirrorDir, "config", "--get", "--default", "", "lfs.storage"},
	} {
		storage, err := args.Shell.Command("git", gitArgs...).RunAndCaptureStdout(ctx)
		if err != nil {
			return fmt.Errorf("reading LFS storage configuration: %w", err)
		}
		if strings.TrimSpace(storage) != "" {
			return gitLFSFetchCheckout(ctx, args)
		}
	}

	args.MirrorDir = sharedMirrorDir
	lockCtx, cancel := context.WithTimeout(ctx, time.Second*time.Duration(e.GitMirrorsLockTimeout))
	defer cancel()
	lock, err := e.shell.LockFile(lockCtx, args.MirrorDir+".updatelock")
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return ErrTimedOutAcquiringLock{Name: "LFS update", Err: err}
		}
		return fmt.Errorf("acquiring mirror LFS update lock: %w", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("releasing mirror LFS update lock: %w", err))
		}
	}()

	e.shell.Commentf("Fetching Git LFS objects into mirror %q", args.MirrorDir)
	return gitLFSFetchCheckout(ctx, args)
}
