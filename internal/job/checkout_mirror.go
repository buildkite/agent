package job

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/internal/osutil"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
	"github.com/buildkite/shellwords"
)

func remoteMirrorOnHostRefspec(commit, branch string) string {
	branchComponent := badCharsRE.ReplaceAllString(branch, "-")
	return "+" + commit + ":refs/buildkite-agent/remote-mirror/" + branchComponent
}

func isOnHostRemoteMirrorAttempt(attempt *remoteMirrorAttempt) bool {
	return attempt != nil &&
		attempt.site == remoteMirrorSiteOnHostMirror &&
		attempt.outcome == remoteMirrorOutcomeNotReached
}

func remoteMirrorStagingDir(mirrorDir string) string {
	sum := sha256.Sum256([]byte(mirrorDir))
	return filepath.Join(filepath.Dir(mirrorDir), fmt.Sprintf(".remote-mirror-%x", sum[:8]))
}

func (e *Executor) getOrUpdateMirrorDir(ctx context.Context, repository string, attempt *remoteMirrorAttempt) (string, error) {
	var mirrorDir string
	// Skip updating the Git mirror before using it?
	if e.GitMirrorsSkipUpdate {
		mirrorDir = filepath.Join(e.GitMirrorsPath, dirForRepository(repository))
		e.shell.Commentf("Skipping update and using existing mirror for repository %s at %s.", repository, mirrorDir)

		// Check if specified mirrorDir exists, otherwise the clone will fail.
		if !osutil.FileExists(mirrorDir) {
			// Fall back to a clean clone, rather than failing the clone and therefore the build
			e.shell.Commentf("No existing mirror found for repository %s at %s.", repository, mirrorDir)
			mirrorDir = ""
		}

		// If git mirror updates are skipped, we assume there's no change
		// to the mirror objects, so no need for snapshotting.
		return mirrorDir, nil
	}

	mirrorDir, err := e.updateGitMirror(ctx, repository, attempt)
	var lockErr ErrTimedOutAcquiringLock
	if isOnHostRemoteMirrorAttempt(attempt) && errors.As(err, &lockErr) {
		// The remote mirror is optional. A speculative clone/update holding the
		// shared lock must not make another job fail when canonical checkout can
		// proceed without an on-host reference.
		attempt.outcome = remoteMirrorOutcomeTimeout
		e.shell.Commentf("Timed out waiting for on-host Git mirror; using canonical repository without it")
		return "", nil
	}
	return mirrorDir, err
}

// updateGitMirror clones a new git mirror (git clone --mirror ...), or updates
// an existing git mirror to ensure relevant refs are available. It returns a
// directory path that a checkout can use for the --reference flag. If clean
// checkouts are enabled, dir will be a path to a snapshot of the mirror,
// otherwise it will be the mirror.
//
// For efficiency reasons, updating an existing mirror is done by fetching
// specific refspecs rather than using `git remote update` to fetch everything
// (see https://github.com/buildkite/agent/pull/1112).
func (e *Executor) updateGitMirror(ctx context.Context, repository string, attempt *remoteMirrorAttempt) (dir string, finalErr error) {
	// Create a unique directory for the repository mirror
	mirrorDir := filepath.Join(e.GitMirrorsPath, dirForRepository(repository))
	isMainRepository := repository == e.Repository

	// Create the mirrors path if it doesn't exist
	if baseDir := filepath.Dir(mirrorDir); !osutil.FileExists(baseDir) {
		e.shell.Commentf("Creating \"%s\"", baseDir)
		// Actual file permissions will be reduced by umask, and won't be 0o777 unless the user has manually changed the umask to 000
		if err := os.MkdirAll(baseDir, 0o777); err != nil {
			return "", err
		}
	}

	if err := e.shell.Chdir(e.GitMirrorsPath); err != nil {
		return "", fmt.Errorf("failed to change directory to %q: %w", e.GitMirrorsPath, err)
	}

	lockTimeout := time.Second * time.Duration(e.GitMirrorsLockTimeout)

	if e.Debug {
		e.shell.Commentf("Acquiring mirror repository clone lock")
	}

	// Lock the mirror dir to prevent concurrent clones
	cloneCtx, canc := context.WithTimeout(ctx, lockTimeout)
	defer canc()

	cloneLockSpan, _ := e.traceOpSpan(ctx, "git.mirror.lock_wait.clone")

	mirrorCloneLock, err := e.shell.LockFile(cloneCtx, mirrorDir+".clonelock")

	tracetools.AddAttributes(cloneLockSpan, map[string]string{"git.timed_out": strconv.FormatBool(errors.Is(err, context.DeadlineExceeded))})
	tracetools.FinishWithError(cloneLockSpan, err)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", ErrTimedOutAcquiringLock{Name: "clone", Err: err}
		}
		return "", fmt.Errorf("unable to acquire clone lock: %w", err)
	}
	defer mirrorCloneLock.Unlock() //nolint:errcheck // Best-effort cleanup - primary unlock checked below.

	stagingDir := remoteMirrorStagingDir(mirrorDir)
	var stagingCleanupErr error
	if osutil.FileExists(stagingDir) {
		e.shell.Commentf("Removing interrupted remote mirror staging dir %q", stagingDir)
		if err := os.RemoveAll(stagingDir); err != nil {
			stagingCleanupErr = fmt.Errorf("removing interrupted remote mirror staging dir %q: %w", stagingDir, err)
			e.shell.Warningf("%v", stagingCleanupErr)
		}
	}

	// If we don't have a mirror, we need to clone it
	if !osutil.FileExists(mirrorDir) {
		e.shell.Commentf("Cloning a mirror of the repository to %q", mirrorDir)
		flags := []string{"--mirror"} // note --mirror implies --bare
		mirrorFlags, err := shellwords.Split(e.GitCloneMirrorFlags)
		if err != nil {
			e.shell.Errorf("Invalid --git-clone-mirror-flags %q (%s)", e.GitCloneMirrorFlags, err)
			return "", err
		}
		flags = append(flags, mirrorFlags...)

		cloneMirror := func(
			ctx context.Context,
			gitFlags []string,
			source, destination string,
			runOpts ...shell.RunCommandOpt,
		) error {
			return e.traceOp(ctx, "git.mirror.clone", func(ctx context.Context) error {
				return gitClone(ctx, e.shell, gitFlags, flags, source, destination, runOpts...)
			})
		}
		removeFailedMirror := func(path string) error {
			if !osutil.FileExists(path) {
				return nil
			}
			e.shell.Commentf("Removing mirror dir %q due to failed clone", path)
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("removing failed mirror %q: %w", path, err)
			}
			return nil
		}

		if isOnHostRemoteMirrorAttempt(attempt) {
			started := time.Now()
			tempMirrorDir := stagingDir
			// A prior process may have been killed while cloning. The clone
			// lock makes this deterministic staging directory exclusive.
			err := stagingCleanupErr
			if err == nil {
				// Match direct git-clone directory creation: apply the process
				// umask and inherit parent setgid/default ACLs.
				err = os.Mkdir(tempMirrorDir, 0o777)
			}
			if err == nil {
				err = cloneMirror(
					ctx,
					remoteMirrorBulkGitFlags(ctx),
					attempt.url,
					tempMirrorDir,
					shell.AlwaysHidePrompt(),
				)
			}
			if err == nil {
				// C16: agents predating remote mirrors may share this mirror.
				// Canonicalize the temporary repository before atomically
				// publishing it to the shared mixed-version cache.
				err = e.shell.Command(
					"git", "--git-dir", tempMirrorDir,
					"remote", "set-url", "origin", repository,
				).Run(ctx)
			}
			hasCommit := false
			if err == nil {
				hasCommit = hasGitCommit(ctx, e.shell, tempMirrorDir, e.Commit)
				if ctxErr := ctx.Err(); ctxErr != nil {
					if cleanupErr := removeFailedMirror(tempMirrorDir); cleanupErr != nil {
						return "", errors.Join(ctxErr, cleanupErr)
					}
					return "", ctxErr
				}
				err = os.Rename(tempMirrorDir, mirrorDir)
			}
			attempt.duration = time.Since(started)
			if err == nil {
				if hasCommit {
					attempt.outcome = remoteMirrorOutcomeHit
				} else {
					// A lagging clone is still useful: the checkout's canonical
					// --reference clone transfers only the missing delta.
					attempt.outcome = remoteMirrorOutcomeMiss
				}
				return e.snapshotMirror(ctx, repository, mirrorDir)
			}

			if stagingCleanupErr == nil && tempMirrorDir != "" {
				if cleanupErr := removeFailedMirror(tempMirrorDir); cleanupErr != nil {
					// Staging is independent of the canonical destination.
					// Preserve the cleanup failure in the log, but still let
					// the optional mirror optimization fail open.
					e.shell.Warningf("Failed to clean remote mirror staging before canonical fallback: %v", cleanupErr)
				}
			}
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			var cloneErr *gitError
			if errors.As(err, &cloneErr) && cloneErr.Type == gitErrorCloneTimeout {
				attempt.outcome = remoteMirrorOutcomeTimeout
			} else {
				attempt.outcome = remoteMirrorOutcomeError
			}
			e.shell.Commentf("Remote Git mirror clone failed; falling back to canonical repository")
		}

		if err := cloneMirror(ctx, nil, repository, mirrorDir); err != nil {
			if cleanupErr := removeFailedMirror(mirrorDir); cleanupErr != nil {
				return "", errors.Join(err, cleanupErr)
			}
			return "", err
		}
		return e.snapshotMirror(ctx, repository, mirrorDir)
	}

	// If it exists, immediately release the clone lock.
	if err := mirrorCloneLock.Unlock(); err != nil {
		return "", fmt.Errorf("unable to release clone lock: %w", err)
	}

	if e.Debug {
		e.shell.Commentf("Acquiring mirror repository update lock")
	}

	// Lock the mirror dir to prevent concurrent updates
	updateCtx, canc := context.WithTimeout(ctx, lockTimeout)
	defer canc()

	updateLockSpan, _ := e.traceOpSpan(ctx, "git.mirror.lock_wait.update")

	mirrorUpdateLock, err := e.shell.LockFile(updateCtx, mirrorDir+".updatelock")

	tracetools.AddAttributes(updateLockSpan, map[string]string{"git.timed_out": strconv.FormatBool(errors.Is(err, context.DeadlineExceeded))})
	tracetools.FinishWithError(updateLockSpan, err)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", ErrTimedOutAcquiringLock{Name: "update", Err: err}
		}
		return "", fmt.Errorf("unable to acquire update lock: %w", err)
	}
	defer func() {
		if err := mirrorUpdateLock.Unlock(); err != nil {
			finalErr = errors.Join(finalErr, fmt.Errorf("unable to release update lock: %w", err))
		}
	}()

	if isMainRepository &&
		attempt != nil &&
		attempt.outcome == remoteMirrorOutcomeSkipped &&
		attempt.skipReason == remoteMirrorSkipNoURL &&
		hasGitCommit(ctx, e.shell, mirrorDir, e.Commit) {
		// Avoid the global reachability scan when remote mirroring is not configured.
		e.shell.Commentf("Commit %q exists in mirror", e.Commit)
		return e.snapshotMirror(ctx, repository, mirrorDir)
	}

	commitAlreadyPresent := false
	if isMainRepository {
		// Check again after we get a lock, in case the other process has already updated
		if hasGitCommit(ctx, e.shell, mirrorDir, e.Commit) &&
			hasGitCommitReachableFromRef(ctx, e.shell, mirrorDir, e.Commit) {
			e.shell.Commentf("Commit %q exists in mirror", e.Commit)
			commitAlreadyPresent = true
		}
	}

	e.shell.Commentf("Updating existing repository mirror to find commit %s", e.Commit)

	// Update the origin of the repository so we can gracefully handle
	// repository renames. This must happen before a remote-mirror hit can skip
	// the canonical fetch: dirForRepository is lossy, so distinct canonical
	// URLs can share one durable mirror directory.
	urlChanged, err := e.updateRemoteURL(ctx, mirrorDir, repository)
	if err != nil {
		return "", fmt.Errorf("setting remote URL: %w", err)
	}

	remoteMirrorHit := false
	if isMainRepository && !commitAlreadyPresent {
		if isOnHostRemoteMirrorAttempt(attempt) {
			hit, err := e.fetchCommitFromRemoteMirror(
				ctx,
				attempt,
				mirrorDir,
				"--no-tags",
				remoteMirrorOnHostRefspec(e.Commit, e.Branch),
			)
			if err != nil {
				return "", err
			}
			if hit {
				// C2: the helper requires both a successful ref write and
				// local object presence. Keep going through rename
				// maintenance instead of returning early.
				remoteMirrorHit = true
			}
		}
	}

	if isMainRepository && !commitAlreadyPresent && !remoteMirrorHit {
		var refspecs []string
		var retry bool

		switch {
		case e.RefSpec != "":
			// If a custom refspec is provided, use it instead of the branch
			e.shell.Commentf("Fetching and mirroring custom refspec %s", e.RefSpec)
			refspecs = []string{e.RefSpec}
		case e.PullRequest != "false" && strings.Contains(e.PipelineProvider, "github"):
			e.shell.Commentf("Fetching and mirroring pull request head from GitHub. This will be retried if it fails, as the pull request head might not be available yet — GitHub creates them asynchronously")
			var refspec string
			if e.PullRequestUsingMergeRefspec {
				refspec = fmt.Sprintf("refs/pull/%s/merge", e.PullRequest)
			} else {
				refspec = fmt.Sprintf("refs/pull/%s/head", e.PullRequest)
			}
			refspecs = []string{refspec}
			retry = true
		default:
			// Fetch the build branch from the upstream repository into the mirror.
			refspecs = []string{e.Branch}
		}

		// Fetch the refspecs from the upstream repository into the mirror.
		if err := e.traceOp(ctx, "git.mirror.fetch", func(ctx context.Context) error {
			return gitFetch(ctx, gitFetchArgs{
				Shell:      e.shell,
				GitFlags:   []string{"--git-dir", mirrorDir},
				Repository: "origin",
				RefSpecs:   refspecs,
				Retry:      retry,
			})
		}); err != nil {
			return "", err
		}
	}
	if !isMainRepository {
		// This is a mirror of a submodule.
		// Update without specifying particular ref, since we don't know which
		// ref is needed for the main build.
		// (If it doesn't contain the needed ref, then the build would fail on
		// a clean host or with a clean checkout.)
		// TODO: Investigate getting the ref from the main repo and passing
		// that in here.
		cmd := e.shell.Command("git", "--git-dir", mirrorDir, "fetch", "origin")
		if err := e.traceOp(ctx, "git.mirror.fetch", func(ctx context.Context) error {
			return cmd.Run(ctx)
		}); err != nil {
			return "", err
		}
	}

	if urlChanged {
		// Let's opportunistically fsck and gc.
		// 1. In case of remote URL confusion (bug introduced in #1959), and
		// 2. There's possibly some object churn when remotes are renamed.
		if err := e.shell.Command("git", "--git-dir", mirrorDir, "fsck").Run(ctx); err != nil {
			e.shell.Warningf("Couldn't run git fsck: %v", err)
		}
		if err := e.shell.Command("git", "--git-dir", mirrorDir, "gc").Run(ctx); err != nil {
			e.shell.Warningf("Couldn't run git gc: %v", err)
		}
	}

	// Unpinned objects are unsafe through --reference; fail open rather than manage recovery refs.
	if isMainRepository &&
		hasGitCommit(ctx, e.shell, mirrorDir, e.Commit) &&
		!hasGitCommitReachableFromRef(ctx, e.shell, mirrorDir, e.Commit) {
		e.shell.Warningf("Commit %q exists in mirror without a durable ref; using canonical repository without it", e.Commit)
		return "", nil
	}

	return e.snapshotMirror(ctx, repository, mirrorDir)
}

// snapshotMirror creates a snapshot of the mirror. It returns the directory for
// the rest of the checkout to use as --reference, which will be the path to a
// snapshot, unless clean checkout is disabled, in which case it will simply
// return mirrorDir.
//
// This "snapshot" is a clone of the *mirror* in a nearby directory, but on a
// filesystem that supports hardlinks (most modern filesystems), the git objects
// in this clone will be hardlinks into the mirror - quick to create and taking
// up negligible extra space. Doing this ensures that any object changes in the
// mirror (due to, say, git gc) won't corrupt the downstream reference clone
// (the checkout). When the mirror updates, it may write new object files and
// unlink old object files, but the snapshot continues to have access to the old
// files via its hardlinks. (At that point, the snapshot takes up more space.)
//
// Like the (clean) checkout, the snapshot only needs to exist as long as the
// current job, but we specifically remove the snapshot at the end of the job,
// since the filesystem containing mirrors (typically) persists across jobs -
// leaving them will let them accumulate and drift from the mirror, taking up
// space. (In contrast, the checkout may cease to exist if the agent is
// ephemeral, so cleaning up the checkout at the end of the job might or might
// not be wasted effort.) Ephemeral agents can flag that they don't keep
// their checkouts by just enabling clean checkout.
//
// Why no snapshots if clean checkout is disabled:
// Disabling clean checkout is how checkouts are reused (for efficiency), so
// the checkout can exist with its dependency on the snapshot forever, so
// we can't delete the snapshot (otherwise we will corrupt the checkout), so
// the mirror volume will gradually fill up with snapshots.
// And while the mirror is updated with new objects in each job, the snapshots
// are not updated, so the non-clean checkout will probably redundantly fetch
// its update from the remote, so we would need extra logic to disable the
// mirror update unless the checkout turns out to be a fresh clone.
// It's not impossible (perhaps we need a process to age-out existing checkouts)
// but something to think about, and when we have a good implementation enable
// it for more cases.
//
// Why no snapshots if the command phase isn't included:
// The cleanup mechanism happens when this instance of the executor tears down,
// not when the last executor among many tears down. In a split-phase setup
// (such as in agent-stack-k8s) where one container runs the checkout phase and
// another runs the command phase, the snapshot would be deleted after the
// checkout phase, which could break many git operations in the command phase.
// Presently we have no way to pass cleanup instructions between containers,
// which would enable this case.
func (e *Executor) snapshotMirror(ctx context.Context, repository, mirrorDir string) (string, error) {
	if !e.CleanCheckout || !e.includePhase("command") {
		return mirrorDir, nil
	}

	snapshotBaseDir := filepath.Join(e.GitMirrorsPath, "snapshots")

	// Create the snapshots base dir if it doesn't exist
	if !osutil.FileExists(snapshotBaseDir) {
		e.shell.Commentf("Creating %q", snapshotBaseDir)
		// See comment above about umask
		if err := os.MkdirAll(snapshotBaseDir, 0o777); err != nil {
			return "", fmt.Errorf("creating base directory for snapshots: %w", err)
		}
	}

	// Create a unique directory for this snapshot.
	// MkdirTemp ensures the new dir won't collide with other agents.
	snapshotDir, err := os.MkdirTemp(snapshotBaseDir, dirForRepository(repository))
	if err != nil {
		return "", fmt.Errorf("creating snapshot directory: %w", err)
	}
	if err := os.Chmod(snapshotDir, 0o777&^osutil.Umask); err != nil {
		return "", fmt.Errorf("changing permissions on snapshot directory: %w", err)
	}

	// Automatically remove it during teardown
	e.cleanupDirs = append(e.cleanupDirs, snapshotDir)

	// Finally, clone the snapshot. Yes, it's a --mirror of a --mirror.
	e.shell.Commentf("Creating mirror snapshot in %q", snapshotDir)
	if err := e.traceOp(ctx, "git.mirror.snapshot", func(ctx context.Context) error {
		return gitClone(ctx, e.shell, nil, []string{"--mirror"}, mirrorDir, snapshotDir)
	}); err != nil {
		return "", err
	}

	return snapshotDir, nil
}

// updateRemoteURL updates the URL for 'origin' and reports whether the
// URL changed from something else. If gitDir == "", it assumes the
// local repo is in the current directory, otherwise it includes --git-dir.
// If the remote has changed, it logs some extra information.
func (e *Executor) updateRemoteURL(ctx context.Context, gitDir, repository string) (bool, error) {
	// Update the origin of the repository so we can gracefully handle
	// repository renames.

	// First check what the existing remote is, for both logging and debugging
	// purposes.

	// Check if there are multiple URLs configured (e.g., via git remote set-url --add).
	args := []string{"config", "--get-all", "remote.origin.url"}
	if gitDir != "" {
		args = append([]string{"--git-dir", gitDir}, args...)
	}
	allURLs, err := e.shell.Command("git", args...).RunAndCaptureStdout(ctx)
	if err != nil {
		return false, err
	}

	var gotURL string
	urls := strings.Split(strings.TrimSpace(allURLs), "\n")
	if len(urls) > 1 {
		// Multiple URLs configured - fall back to git remote get-url which
		// handles this correctly (returns primary fetch URL).
		args = []string{"remote", "get-url", "origin"}
		if gitDir != "" {
			args = append([]string{"--git-dir", gitDir}, args...)
		}
		gotURL, err = e.shell.Command("git", args...).RunAndCaptureStdout(ctx)
		if err != nil {
			return false, err
		}
	} else {
		// Single URL - use config output directly to avoid insteadOf transformation.
		gotURL = urls[0]
	}

	if gotURL == repository {
		// No need to update anything
		return false, nil
	}

	gd := gitDir
	if gd == "" {
		gd = e.shell.Getwd()
	}

	e.shell.Commentf("Remote URL for git directory %s has changed (%s -> %s)!", gd, gotURL, repository)
	e.shell.Commentf("This is usually because the repository has been renamed.")
	e.shell.Commentf("If this is unexpected, you may see failures.")

	args = []string{"remote", "set-url", "origin", repository}
	if gitDir != "" {
		args = append([]string{"--git-dir", gitDir}, args...)
	}
	return true, e.shell.Command("git", args...).Run(ctx)
}

// This is the same thing that git does at the end of clone when it is
// passed --dissociate:
// https://github.com/git/git/blob/6e8d538aab8fe4dd07ba9fb87b5c7edcfa5706ad/builtin/clone.c#L843-L859
// This kind of git surgery is acceptable, because it's how one would dissociate
// a reference clone prior to Git 2.3.
func (e *Executor) dissociateIfNeeded(ctx context.Context, gitDir string) error {
	alternates := filepath.Join(gitDir, "objects", "info", "alternates")
	if !osutil.FileExists(alternates) {
		return nil
	}
	e.shell.Commentf("Dissociating existing reference clone because git mirror checkout mode is %q", e.GitMirrorCheckoutMode)
	if err := gitRepack(ctx, e.shell, "-a", "-d"); err != nil {
		return fmt.Errorf("cleaning up reference clone: %w", err)
	}
	if err := os.Remove(alternates); err != nil {
		return fmt.Errorf("removing alternates file: %w", err)
	}
	return nil
}

// reassociateIfNeeded writes a new alternates file into gitDir/objects/info,
// referring to mirrorDir/objects. This allows the repo in gitDir to reuse
// objects from mirrorDir, but at the risk of those objects becoming unavailable
// later on.
func (e *Executor) reassociateIfNeeded(ctx context.Context, gitDir, mirrorDir string) error {
	alternates := filepath.Join(gitDir, "objects", "info", "alternates")
	if osutil.FileExists(alternates) {
		return nil
	}
	e.shell.Commentf("Re-associating existing clone because git mirror checkout mode is %q", e.GitMirrorCheckoutMode)
	objects := filepath.Join(mirrorDir, "objects")
	if !osutil.FileExists(objects) {
		return fmt.Errorf("objects directory missing from mirror directory %s", mirrorDir)
	}
	objects += "\n"
	if err := os.WriteFile(alternates, []byte(objects), 0o644); err != nil {
		return fmt.Errorf("writing alternates file: %w", err)
	}
	return nil
}

type ErrTimedOutAcquiringLock struct {
	Name string
	Err  error
}

func (e ErrTimedOutAcquiringLock) Error() string {
	return fmt.Sprintf("timed out acquiring %s lock: %v", e.Name, e.Err)
}

func (e ErrTimedOutAcquiringLock) Unwrap() error { return e.Err }
