package job

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/internal/osutil"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/tracetools"
)

const (
	remoteMirrorBranchComponentMax = 240
)

var remoteMirrorProbeTimeout = 30 * time.Second

var fullSHA1ObjectID = regexp.MustCompile(`^[0-9a-f]{40}$`)

type remoteMirrorSite int

const (
	remoteMirrorSiteNone remoteMirrorSite = iota
	remoteMirrorSiteOnHostMirror
	remoteMirrorSiteExistingCheckout
	remoteMirrorSiteFreshClone
)

func (s remoteMirrorSite) String() string {
	switch s {
	case remoteMirrorSiteOnHostMirror:
		return "on-host-mirror"
	case remoteMirrorSiteExistingCheckout:
		return "existing-checkout"
	case remoteMirrorSiteFreshClone:
		return "fresh-clone"
	default:
		return "none"
	}
}

type remoteMirrorOutcome int

const (
	// notReached must remain the zero value so early returns cannot report a
	// mirror hit before the selected site runs.
	remoteMirrorOutcomeNotReached remoteMirrorOutcome = iota
	remoteMirrorOutcomeHit
	remoteMirrorOutcomeMiss
	remoteMirrorOutcomeTimeout
	remoteMirrorOutcomeError
	remoteMirrorOutcomeSkipped
)

func (o remoteMirrorOutcome) String() string {
	switch o {
	case remoteMirrorOutcomeHit:
		return "hit"
	case remoteMirrorOutcomeMiss:
		return "miss"
	case remoteMirrorOutcomeTimeout:
		return "timeout"
	case remoteMirrorOutcomeError:
		return "error"
	case remoteMirrorOutcomeSkipped:
		return "skipped"
	default:
		return "notReached"
	}
}

type remoteMirrorSkipReason string

const (
	remoteMirrorSkipNone             remoteMirrorSkipReason = ""
	remoteMirrorSkipNoURL            remoteMirrorSkipReason = "no-url"
	remoteMirrorSkipNotHTTPS         remoteMirrorSkipReason = "not-https"
	remoteMirrorSkipCanonicalChanged remoteMirrorSkipReason = "canonical-changed"
	remoteMirrorSkipNotFullObjectID  remoteMirrorSkipReason = "not-full-object-id"
	remoteMirrorSkipEmptyBranch      remoteMirrorSkipReason = "empty-branch"
	remoteMirrorSkipBranchTooLong    remoteMirrorSkipReason = "branch-too-long"
	remoteMirrorSkipCustomRefspec    remoteMirrorSkipReason = "custom-refspec"
	remoteMirrorSkipTagBuild         remoteMirrorSkipReason = "tag-build"
	remoteMirrorSkipPullRequest      remoteMirrorSkipReason = "pull-request"
	remoteMirrorSkipNotFirstAttempt  remoteMirrorSkipReason = "not-first-attempt"
)

type remoteMirrorAttempt struct {
	site       remoteMirrorSite
	url        string
	skipReason remoteMirrorSkipReason

	outcome  remoteMirrorOutcome
	duration time.Duration
}

func (a remoteMirrorAttempt) hitOutsideOnHostMirror() bool {
	return a.outcome == remoteMirrorOutcomeHit && a.site != remoteMirrorSiteOnHostMirror
}

func (e *Executor) resolveRemoteMirrorAttempt(previousAttempts int) remoteMirrorAttempt {
	skip := func(reason remoteMirrorSkipReason) remoteMirrorAttempt {
		return remoteMirrorAttempt{
			outcome:    remoteMirrorOutcomeSkipped,
			skipReason: reason,
		}
	}

	if e.GitRemoteMirrorURL == "" {
		return skip(remoteMirrorSkipNoURL)
	}
	mirrorURL, err := url.Parse(e.GitRemoteMirrorURL)
	if err != nil || mirrorURL.Scheme != "https" || mirrorURL.Host == "" {
		return skip(remoteMirrorSkipNotHTTPS)
	}
	if e.Repository != e.canonicalRepository {
		return skip(remoteMirrorSkipCanonicalChanged)
	}
	if !fullSHA1ObjectID.MatchString(e.Commit) {
		return skip(remoteMirrorSkipNotFullObjectID)
	}
	if e.Branch == "" {
		return skip(remoteMirrorSkipEmptyBranch)
	}
	branchComponent := badCharsRE.ReplaceAllString(e.Branch, "-")
	if len(branchComponent) > remoteMirrorBranchComponentMax {
		return skip(remoteMirrorSkipBranchTooLong)
	}
	if e.RefSpec != "" {
		return skip(remoteMirrorSkipCustomRefspec)
	}
	if e.Tag != "" {
		return skip(remoteMirrorSkipTagBuild)
	}
	if e.PullRequest != "false" {
		return skip(remoteMirrorSkipPullRequest)
	}
	if previousAttempts != 0 {
		return skip(remoteMirrorSkipNotFirstAttempt)
	}

	attempt := remoteMirrorAttempt{url: e.GitRemoteMirrorURL}
	switch {
	case e.GitMirrorsPath != "" && e.Repository != "" && !e.GitMirrorsSkipUpdate:
		attempt.site = remoteMirrorSiteOnHostMirror
	case e.checkoutAlreadyExists():
		attempt.site = remoteMirrorSiteExistingCheckout
	default:
		attempt.site = remoteMirrorSiteFreshClone
	}
	return attempt
}

func (e *Executor) checkoutAlreadyExists() bool {
	checkoutPath, ok := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")
	return ok && osutil.FileExists(filepath.Join(checkoutPath, ".git"))
}

func remoteMirrorGitFlags(ctx context.Context) []string {
	return []string{
		"-c", "credential.useHttpPath=true",
		"-c", "credential.helper=",
		"-c", "credential.helper=" + gitCredentialHelperCommand(ctx),
		"-c", "http.extraHeader=",
		"-c", "protocol.version=2",
	}
}

// fetchCommitFromRemoteMirror performs one bounded, non-retrying exact-object
// fetch and owns both halves of hit confirmation: the command must succeed and
// the commit must be present locally. Expected mirror failures are represented
// in attempt.outcome so callers can fail open to canonical. A cancelled parent
// context is returned and must not trigger canonical fallback.
func (e *Executor) fetchCommitFromRemoteMirror(
	ctx context.Context,
	attempt *remoteMirrorAttempt,
	gitDir string,
	gitFetchFlags string,
	refspec string,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	started := time.Now()
	mirrorCtx, cancel := context.WithTimeout(ctx, remoteMirrorProbeTimeout)
	defer cancel()

	gitFlags := make([]string, 0, len(remoteMirrorGitFlags(ctx))+2)
	if gitDir != "" {
		gitFlags = append(gitFlags, "--git-dir", gitDir)
	}
	gitFlags = append(gitFlags, remoteMirrorGitFlags(ctx)...)

	err := gitFetch(mirrorCtx, gitFetchArgs{
		Shell:         e.shell,
		GitFlags:      gitFlags,
		GitFetchFlags: gitFetchFlags,
		Repository:    attempt.url,
		RefSpecs:      []string{refspec},
	})
	attempt.duration = time.Since(started)

	if parentErr := ctx.Err(); parentErr != nil {
		return false, parentErr
	}
	if errors.Is(mirrorCtx.Err(), context.DeadlineExceeded) {
		attempt.outcome = remoteMirrorOutcomeTimeout
		return false, nil
	}
	if err != nil {
		var gitErr *gitError
		if errors.As(err, &gitErr) && gitErr.Type == gitErrorFetchRefNotOnRemote {
			attempt.outcome = remoteMirrorOutcomeMiss
		} else {
			attempt.outcome = remoteMirrorOutcomeError
		}
		return false, nil
	}
	if !hasGitCommit(ctx, e.shell, gitDir, e.Commit) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		attempt.outcome = remoteMirrorOutcomeMiss
		return false, nil
	}

	attempt.outcome = remoteMirrorOutcomeHit
	return true, nil
}

func (e *Executor) emitRemoteMirrorTelemetry(span tracetools.Span, attempt remoteMirrorAttempt) {
	attributes := map[string]string{
		"git.remote_mirror.outcome": attempt.outcome.String(),
		"git.remote_mirror.site":    attempt.site.String(),
	}
	if attempt.skipReason != remoteMirrorSkipNone {
		attributes["git.remote_mirror.skip_reason"] = string(attempt.skipReason)
	}
	if attempt.duration > 0 {
		attributes["git.remote_mirror.duration_ms"] = strconv.FormatInt(attempt.duration.Milliseconds(), 10)
	}
	span.AddAttributes(attributes)

	message := "Remote Git mirror: outcome=" + attempt.outcome.String() + " site=" + attempt.site.String()
	if attempt.skipReason != remoteMirrorSkipNone {
		message += " skip_reason=" + string(attempt.skipReason)
	}
	if attempt.duration > 0 {
		message += " duration=" + attempt.duration.Round(time.Millisecond).String()
	}
	if attempt.url != "" {
		message += " url=" + redact.URLCredentials(attempt.url)
	}
	e.shell.Commentf("%s", strings.TrimSpace(message))
}
