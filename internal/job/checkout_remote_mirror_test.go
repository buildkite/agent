package job

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/shell"
)

func TestResolveRemoteMirrorAttempt(t *testing.T) {
	t.Parallel()

	commit := strings.Repeat("a", 40)
	base := ExecutorConfig{
		Repository:         "https://canonical.example/acme/widgets.git",
		GitRemoteMirrorURL: "https://mirror.example/acme/widgets.git",
		Commit:             commit,
		Branch:             "main",
		PullRequest:        "false",
	}

	tests := []struct {
		name             string
		mutate           func(*Executor)
		previousAttempts int
		wantSite         remoteMirrorSite
		wantOutcome      remoteMirrorOutcome
		wantSkipReason   remoteMirrorSkipReason
	}{
		{
			name:           "fresh checkout",
			wantSite:       remoteMirrorSiteFreshClone,
			wantOutcome:    remoteMirrorOutcomeNotReached,
			wantSkipReason: remoteMirrorSkipNone,
		},
		{
			name: "on-host mirror",
			mutate: func(e *Executor) {
				e.GitMirrorsPath = t.TempDir()
			},
			wantSite:       remoteMirrorSiteOnHostMirror,
			wantOutcome:    remoteMirrorOutcomeNotReached,
			wantSkipReason: remoteMirrorSkipNone,
		},
		{
			name: "existing checkout",
			mutate: func(e *Executor) {
				checkout := checkoutPathForRemoteMirrorTest(t, e)
				if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantSite:       remoteMirrorSiteExistingCheckout,
			wantOutcome:    remoteMirrorOutcomeNotReached,
			wantSkipReason: remoteMirrorSkipNone,
		},
		{
			name: "skip on-host update falls through to fresh checkout",
			mutate: func(e *Executor) {
				e.GitMirrorsPath = t.TempDir()
				e.GitMirrorsSkipUpdate = true
			},
			wantSite:       remoteMirrorSiteFreshClone,
			wantOutcome:    remoteMirrorOutcomeNotReached,
			wantSkipReason: remoteMirrorSkipNone,
		},
		{
			name: "no URL",
			mutate: func(e *Executor) {
				e.GitRemoteMirrorURL = ""
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipNoURL,
		},
		{
			name: "non-HTTPS URL",
			mutate: func(e *Executor) {
				e.GitRemoteMirrorURL = "http://mirror.example/acme/widgets.git"
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipNotHTTPS,
		},
		{
			name: "canonical changed after construction",
			mutate: func(e *Executor) {
				e.Repository = "https://other.example/acme/widgets.git"
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipCanonicalChanged,
		},
		{
			name: "not a full object ID",
			mutate: func(e *Executor) {
				e.Commit = commit[:12]
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipNotFullObjectID,
		},
		{
			name: "uppercase object ID",
			mutate: func(e *Executor) {
				e.Commit = strings.ToUpper(commit)
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipNotFullObjectID,
		},
		{
			name: "empty branch",
			mutate: func(e *Executor) {
				e.Branch = ""
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipEmptyBranch,
		},
		{
			name: "branch path component too long",
			mutate: func(e *Executor) {
				e.Branch = strings.Repeat("a", remoteMirrorBranchComponentMax+1)
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipBranchTooLong,
		},
		{
			name: "custom refspec",
			mutate: func(e *Executor) {
				e.RefSpec = "refs/heads/main"
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipCustomRefspec,
		},
		{
			name: "tag build",
			mutate: func(e *Executor) {
				e.Tag = "v1.0.0"
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipTagBuild,
		},
		{
			name: "pull request",
			mutate: func(e *Executor) {
				e.PullRequest = "42"
			},
			wantOutcome:    remoteMirrorOutcomeSkipped,
			wantSkipReason: remoteMirrorSkipPullRequest,
		},
		{
			name:             "later checkout attempt",
			previousAttempts: 1,
			wantOutcome:      remoteMirrorOutcomeSkipped,
			wantSkipReason:   remoteMirrorSkipNotFirstAttempt,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := New(base)
			e.shell = shell.NewTestShell(t)
			checkoutPathForRemoteMirrorTest(t, e)
			if tc.mutate != nil {
				tc.mutate(e)
			}

			got := e.resolveRemoteMirrorAttempt(tc.previousAttempts)
			if got.site != tc.wantSite {
				t.Errorf("site = %q, want %q", got.site, tc.wantSite)
			}
			if got.outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", got.outcome, tc.wantOutcome)
			}
			if got.skipReason != tc.wantSkipReason {
				t.Errorf("skipReason = %q, want %q", got.skipReason, tc.wantSkipReason)
			}
		})
	}
}

func TestRemoteMirrorOutcomeNotReachedIsZeroValue(t *testing.T) {
	t.Parallel()

	var got remoteMirrorOutcome
	if got != remoteMirrorOutcomeNotReached {
		t.Fatalf("zero remoteMirrorOutcome = %q, want not reached", got)
	}
}

func TestRemoteMirrorTelemetryContainsNoURL(t *testing.T) {
	t.Parallel()

	span := &recordingRemoteMirrorSpan{}
	e := New(ExecutorConfig{})
	e.shell = shell.NewTestShell(t)
	attempt := remoteMirrorAttempt{
		site:    remoteMirrorSiteFreshClone,
		url:     "https://token:secret@mirror.example/acme/widgets.git",
		outcome: remoteMirrorOutcomeNotReached,
	}

	e.emitRemoteMirrorTelemetry(span, attempt)

	for key, value := range span.attributes {
		if strings.Contains(key, "url") || strings.Contains(value, "mirror.example") || strings.Contains(value, "secret") {
			t.Errorf("metric-shaped telemetry leaks URL data: %q=%q", key, value)
		}
	}
}

func TestFetchCommitFromRemoteMirrorPassesURLAfterOptionTerminator(t *testing.T) {
	t.Parallel()

	var gotLog [][]string
	e := New(ExecutorConfig{Commit: strings.Repeat("a", 40)})
	e.shell = shell.NewTestShell(t, shell.WithDryRun(true), shell.WithCommandLog(&gotLog))
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteExistingCheckout,
		url:  "https://mirror.example/--upload-pack=evil.git",
	}

	_, err := e.fetchCommitFromRemoteMirror(t.Context(), &attempt, ".git", "", e.Commit)
	if err != nil {
		t.Fatalf("fetchCommitFromRemoteMirror() error = %v", err)
	}

	if len(gotLog) == 0 {
		t.Fatal("fetchCommitFromRemoteMirror() ran no git command")
	}
	fetch := gotLog[0]
	var separator int
	for i, arg := range fetch {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == 0 || separator+1 >= len(fetch) || fetch[separator+1] != attempt.url {
		t.Errorf("git fetch args = %q, want -- immediately before mirror URL", fetch)
	}
}

func TestFetchCommitFromRemoteMirrorDoesNotFailOpenAfterCancellation(t *testing.T) {
	t.Parallel()

	e := New(ExecutorConfig{Commit: strings.Repeat("a", 40)})
	e.shell = shell.NewTestShell(t, shell.WithDryRun(true))
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteExistingCheckout,
		url:  "https://mirror.example/acme/widgets.git",
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	hit, err := e.fetchCommitFromRemoteMirror(ctx, &attempt, ".git", "", e.Commit)
	if hit {
		t.Error("fetchCommitFromRemoteMirror() hit = true after cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("fetchCommitFromRemoteMirror() error = %v, want context.Canceled", err)
	}
	if attempt.outcome != remoteMirrorOutcomeNotReached {
		t.Errorf("outcome = %q, want not reached after cancellation", attempt.outcome)
	}
}

type recordingRemoteMirrorSpan struct {
	attributes map[string]string
}

func (s *recordingRemoteMirrorSpan) AddAttributes(attributes map[string]string) {
	s.attributes = attributes
}

func (*recordingRemoteMirrorSpan) FinishWithError(error) {}
func (*recordingRemoteMirrorSpan) RecordError(error)     {}

func checkoutPathForRemoteMirrorTest(t *testing.T, e *Executor) string {
	t.Helper()
	if checkout, ok := e.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"); ok {
		return checkout
	}
	checkout := t.TempDir()
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkout)
	return checkout
}
