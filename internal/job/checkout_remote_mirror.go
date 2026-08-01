package job

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/buildkite/agent/v3/tracetools"
)

var fullCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (e *Executor) shouldAttemptRemoteMirror(previousAttempts int) bool {
	return previousAttempts == 0 &&
		isSupportedRemoteMirrorURL(e.GitRemoteMirrorURL) &&
		fullCommitSHA.MatchString(e.Commit) &&
		e.Tag == "" &&
		e.fetchRefspecKind() == refspecCommit &&
		e.Repository == e.GitRemoteMirrorRepository
}

func isSupportedRemoteMirrorURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
}

type remoteMirrorResult string

const (
	remoteMirrorHit     remoteMirrorResult = "hit"
	remoteMirrorMiss    remoteMirrorResult = "miss"
	remoteMirrorTimeout remoteMirrorResult = "timeout"
	remoteMirrorError   remoteMirrorResult = "error"
)

// remoteMirrorAttemptTrace centralises attributes shared by the on-host
// mirror, reused-checkout, and fresh-clone implementations.
type remoteMirrorAttemptTrace struct {
	span      tracetools.Span
	startedAt time.Time
}

func (e *Executor) startRemoteMirrorAttemptTrace(ctx context.Context) (remoteMirrorAttemptTrace, context.Context) {
	span, spanCtx := e.traceOpSpan(ctx, "git.remote_mirror.attempt")
	return remoteMirrorAttemptTrace{span: span, startedAt: time.Now()}, spanCtx
}

func (t remoteMirrorAttemptTrace) finish(result remoteMirrorResult, fallback bool, err error) {
	t.span.AddAttributes(map[string]string{
		"git.remote_mirror.result":   string(result),
		"git.remote_mirror.fallback": strconv.FormatBool(fallback),
		"git.remote_mirror.duration": time.Since(t.startedAt).String(),
	})
	t.span.FinishWithError(err)
}
