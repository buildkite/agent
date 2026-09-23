package job

import (
	"context"
	"errors"
	"time"

	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/jobapi"
)

func captureGitError(ctx context.Context, sh *shell.Shell, err error) {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) || shell.ExitCode(err) == -1 {
		return
	}
	var gitErr *gitError
	if !errors.As(err, &gitErr) || gitErr.captured {
		return
	}
	// An outer checkout failure must not resubmit a failed delivery either.
	gitErr.captured = true
	code, message := classifyGitError(err)
	if code != "" {
		captureError(ctx, sh, code, message)
	}
}

// classifyGitError maps checkout errors to codes and messages independently of
// delivery. These messages must not replace err.Error() or the longstanding
// checkout log formats, which include operation and retry details.
func classifyGitError(err error) (string, string) {
	if errors.Is(err, errCheckoutAttemptTimedOut) {
		return "git_checkout_timeout", "The Git checkout attempt timed out."
	}
	var gitErr *gitError
	if !errors.As(err, &gitErr) {
		switch {
		case errors.Is(err, errInvalidRef):
			return "git_invalid_ref", "Git checkout received an invalid reference."
		case errors.Is(err, ErrCommitVerificationFailed):
			return "git_commit_verification_failed", "Git commit verification failed."
		default:
			return "git_checkout_failed", "Git checkout preparation failed."
		}
	}
	var code, message string
	switch gitErr.Type {
	case gitErrorCheckout:
		code, message = "git_checkout_failed", "Git checkout failed."
	case gitErrorCheckoutReferenceIsNotATree:
		code, message = "git_reference_not_a_tree", "Git checkout could not resolve the reference to a tree."
	case gitErrorCheckoutRetryClean:
		code, message = "git_checkout_retry_clean", "Git checkout failed and requires a clean checkout before retrying."
	case gitErrorClone:
		code, message = "git_clone_failed", "Git clone failed."
	case gitErrorCloneTimeout:
		code, message = "git_clone_timeout", "Git clone failed because the transfer was too slow."
	case gitErrorFetch:
		code, message = "git_fetch_failed", "Git fetch failed."
	case gitErrorFetchRetryClean:
		code, message = "git_fetch_retry_clean", "Git fetch failed and requires a clean checkout before retrying."
	case gitErrorFetchBadObject:
		code, message = "git_bad_object", "Git fetch encountered a bad object."
	case gitErrorFetchBadReference:
		code, message = "ref_not_found", "Git fetch could not find the remote reference."
	case gitErrorFetchRefNotOnRemote:
		code, message = "git_ref_not_on_remote", "Git fetch requested an object the remote does not have or advertise."
	case gitErrorClean:
		code, message = "git_clean_failed", "Git clean failed."
	case gitErrorCleanSubmodules:
		code, message = "git_submodule_clean_failed", "Git submodule clean failed."
	case gitErrorRepack:
		code, message = "git_repack_failed", "Git repack failed."
	case gitErrorLFS:
		code, message = "git_lfs_failed", "Git LFS fetch or checkout failed."
	default:
		return "", ""
	}
	return code, message
}

func captureCheckoutError(ctx context.Context, sh *shell.Shell, err error) {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return
	}
	// The attempt deadline kills Git with a signal, but is not job cancellation.
	if !errors.Is(err, errCheckoutAttemptTimedOut) {
		if shell.ExitCode(err) == -1 {
			return
		}
		var gitErr *gitError
		if errors.As(err, &gitErr) {
			captureGitError(ctx, sh, err)
			return
		}
	}
	code, message := classifyGitError(err)
	captureError(ctx, sh, code, message)
}

func captureError(ctx context.Context, sh *shell.Shell, code, message string) {
	if !experiments.IsEnabled(ctx, experiments.CaptureError) || sh.Env.GetString("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "") != "true" {
		return
	}
	// Use fixed messages until the capture API supports payload redaction. Never
	// include Git output, wrapped errors, repository URLs, paths, or refs here.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client, err := jobapi.NewClient(ctx, sh.Env.GetString("BUILDKITE_AGENT_JOB_API_SOCKET", ""), sh.Env.GetString("BUILDKITE_AGENT_JOB_API_TOKEN", ""))
	if err == nil {
		err = client.CaptureError(ctx, &jobapi.CapturedError{Code: code, Message: message})
	}
	if err != nil {
		// Transport errors can contain upstream response bodies or socket paths.
		sh.Warningf("Could not capture Git error %q", code)
	}
}
