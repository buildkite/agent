package job

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/buildkite/agent/v4/internal/redact"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/jobapi"
)

var (
	errGitLFSPreflight             = errors.New("git lfs version failed")
	errInvalidSparseCheckoutMode   = errors.New("invalid sparse checkout mode")
	gitAuthenticationErrorPatterns = []string{
		"fatal: Authentication failed",
		"fatal: authentication failed",
		"Permission denied (publickey",
		"Permission denied (password",
	}
	gitNetworkErrorPatterns = []string{
		"Could not resolve host:",
		"Could not resolve proxy:",
		"ssh: Could not resolve hostname",
		"Failed to connect to",
		"ssh: connect to host",
	}
)

type gitErrorReportsKey struct{}

// maxGitErrorOutput bounds the Git output kept while a command runs. The
// report needs only its last lines, which usually say why Git failed.
const maxGitErrorOutput = 16 << 10

// maxGitErrorMessage is the most characters of summary and Git output to
// report. It leaves room within the Job API's 1000-character message limit for
// redaction to lengthen short secrets, so the end of the output is not cut.
const maxGitErrorMessage = 900

type gitErrorOutput struct {
	mu   sync.Mutex
	data []byte
	cut  bool
}

func (o *gitErrorOutput) tee(sh *shell.Shell) shell.RunCommandOpt {
	if sh.Env.GetString("BUILDKITE_CAPTURE_GIT_ERRORS", "") != "true" ||
		sh.Env.GetString("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "") != "true" {
		return shell.TeeOutput(nil)
	}
	return shell.TeeOutput(o)
}

func (o *gitErrorOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.data = append(o.data, p...)
	if over := len(o.data) - maxGitErrorOutput; over > 0 {
		o.data = append(o.data[:0], o.data[over:]...)
		o.cut = true
	}
	return len(p), nil
}

// String returns the whole lines of output kept, dropping a first line that
// was cut short.
func (o *gitErrorOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	data := o.data
	if o.cut {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return ""
		}
		data = data[i+1:]
	}
	return string(data)
}

func (o *gitErrorOutput) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.data = nil
	o.cut = false
}

// gitErrorReports collects observations from the sequential Git operations in
// one checkout attempt. Delivery waits until the attempt's result is settled.
type gitErrorReports []jobapi.CapturedError

func (reports gitErrorReports) deliver(ctx context.Context, sh *shell.Shell) {
	for _, report := range reports {
		deliverError(ctx, sh, report)
	}
}

// addGitRemoteErrorPatterns adds reporting-only patterns to a command's search.
func addGitRemoteErrorPatterns(smelt map[string]bool) {
	for _, pattern := range gitAuthenticationErrorPatterns {
		smelt[pattern] = false
	}
	for _, pattern := range gitNetworkErrorPatterns {
		smelt[pattern] = false
	}
}

func captureGitError(ctx context.Context, sh *shell.Shell, err error, output string) {
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
		captureError(ctx, sh, code, withGitOutput(message, redactGitOutput(ctx, output)))
	}
}

type gitErrorNeedlesKey struct{}

// withGitErrorNeedles gives Git error capture the job's registered secrets,
// such as the executor's redactors' needles, read when output is captured.
func withGitErrorNeedles(ctx context.Context, needles func() []string) context.Context {
	return context.WithValue(ctx, gitErrorNeedlesKey{}, needles)
}

// redactGitOutput redacts registered secrets and Buildkite tokens in Git's
// output before withGitOutput keeps only its last lines, so the cut cannot
// leave part of a multi-line secret, such as a private key, that the Job API
// could no longer match. Needles are read now, so they include secrets
// registered while Git ran. A secret longer than the output kept can still
// lose its start to the buffer's cut, but not reach the lines reported.
func redactGitOutput(ctx context.Context, output string) string {
	var needles []string
	if f, ok := ctx.Value(gitErrorNeedlesKey{}).(func() []string); ok {
		needles = f()
	}
	var b strings.Builder
	r := replacer.New(&b, needles, redact.Redacted)
	r.AddPrefixes(redact.TokenPrefixes()...)
	// Errors writing to a strings.Builder are bugs.
	if _, err := r.Write([]byte(output)); err != nil {
		panic(err)
	}
	if err := r.Flush(); err != nil {
		panic(err)
	}
	return b.String()
}

// withGitOutput adds as many of the last whole lines of Git's output to
// summary as fit within maxGitErrorMessage characters. The Job API redacts the
// report, and cutting only between lines keeps whole the URLs and tokens that
// Git prints, so it can still recognize them.
func withGitOutput(summary, output string) string {
	if strings.TrimSpace(output) == "" || !utf8.ValidString(output) || strings.ContainsRune(output, 0) {
		return summary
	}
	// Mask URLs first, so a line that is long only because of a presigned
	// signature still fits. The Job API masks them again after matching
	// registered secrets.
	output = redact.URLQueriesInText(redact.URLCredentialsInText(output))
	const heading = "\n\nLast lines of Git output:\n"
	budget := maxGitErrorMessage - utf8.RuneCountInString(summary) - utf8.RuneCountInString(heading)
	lines := strings.SplitAfter(output, "\n")
	start := len(lines)
	for start > 0 {
		n := utf8.RuneCountInString(lines[start-1])
		if n > budget {
			break
		}
		budget -= n
		start--
	}
	recent := strings.Join(lines[start:], "")
	if strings.TrimSpace(recent) == "" {
		return summary
	}
	return summary + heading + recent
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
		var lockErr ErrTimedOutAcquiringLock
		switch {
		case errors.Is(err, errInvalidRef):
			return "git_invalid_ref", "Git checkout received an invalid reference."
		case errors.Is(err, ErrCommitVerificationFailed):
			return "git_commit_verification_failed", "Git commit verification failed."
		case errors.Is(err, errGitLFSPreflight):
			return "git_lfs_preflight_failed", "Git LFS version check failed."
		case errors.Is(err, errInvalidSparseCheckoutMode):
			return "git_invalid_sparse_checkout_mode", "Git checkout received an invalid sparse checkout mode."
		case errors.As(err, &lockErr):
			return "git_checkout_lock_timeout", "Git checkout timed out waiting for a lock."
		default:
			return "git_checkout_phase_failed", "The default checkout phase failed."
		}
	}
	for _, pattern := range gitAuthenticationErrorPatterns {
		if gitErr.outputMatches[pattern] {
			return "git_authentication_failed", "Git reported an authentication failure."
		}
	}
	for _, pattern := range gitNetworkErrorPatterns {
		if gitErr.outputMatches[pattern] {
			return "git_network_failed", "Git reported a network connection failure."
		}
	}
	var code, message string
	switch gitErr.Type {
	case gitErrorCheckout:
		code, message = "git_checkout_failed", "Git checkout failed."
	case gitErrorCheckoutReferenceIsNotATree:
		code, message = "git_reference_not_a_tree", "Git checkout could not resolve the reference to a tree."
	case gitErrorCheckoutRetryClean:
		code, message = "git_checkout_unclassified", "Git checkout failed with an unclassified error."
	case gitErrorClone:
		code, message = "git_clone_failed", "Git clone failed."
	case gitErrorCloneTimeout:
		code, message = "git_clone_timeout", "Git clone failed because the transfer was too slow."
	case gitErrorFetch:
		code, message = "git_fetch_failed", "Git fetch failed."
	case gitErrorFetchRetryClean:
		code, message = "git_fetch_unclassified", "Git fetch failed with an unclassified error."
	case gitErrorFetchBadObject:
		code, message = "git_bad_object", "Git fetch encountered a bad object."
	case gitErrorFetchBadReference:
		code, message = "git_ref_not_found", "Git fetch could not find the remote reference."
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
			if gitErr.captured {
				return
			}
			// Git commands capture their own failures. Other checkout operations
			// reuse these error types for recovery, not to identify a failed command.
			gitErr.captured = true
			captureError(ctx, sh, "git_checkout_phase_failed", "The default checkout phase failed.")
			return
		}
	}
	code, message := classifyGitError(err)
	captureError(ctx, sh, code, message)
}

func captureError(ctx context.Context, sh *shell.Shell, code, message string) {
	// Automatic Git reporting is opt-in independently of the general capture API.
	if sh.Env.GetString("BUILDKITE_CAPTURE_GIT_ERRORS", "") != "true" ||
		sh.Env.GetString("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", "") != "true" {
		return
	}
	// The Local Job API redacts registered secrets before forwarding the report.
	now := time.Now()
	report := jobapi.CapturedError{Code: code, Message: message, Timestamp: &now}
	if reports, ok := ctx.Value(gitErrorReportsKey{}).(*gitErrorReports); ok {
		*reports = append(*reports, report)
		return
	}
	deliverError(ctx, sh, report)
}

func deliverError(ctx context.Context, sh *shell.Shell, report jobapi.CapturedError) {
	if ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client, err := jobapi.NewClient(ctx, sh.Env.GetString("BUILDKITE_AGENT_JOB_API_SOCKET", ""), sh.Env.GetString("BUILDKITE_AGENT_JOB_API_TOKEN", ""))
	if err == nil {
		_, err = client.CaptureError(ctx, &report)
	}
	if err != nil {
		// Transport errors can contain upstream response bodies or socket paths.
		sh.Warningf("Could not capture Git error %q", report.Code)
	}
}
