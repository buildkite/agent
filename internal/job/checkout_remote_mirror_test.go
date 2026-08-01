package job

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/self"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/shellwords"
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
	var logs bytes.Buffer
	e.shell = shell.NewTestShell(t, shell.WithLogger(shell.NewWriterLogger(&logs, false, nil)))
	attempt := remoteMirrorAttempt{
		site:    remoteMirrorSiteFreshClone,
		url:     "https://token:secret@mirror.example/acme/widgets.git",
		outcome: remoteMirrorOutcomeNotReached,
	}

	e.emitRemoteMirrorTelemetry(span, attempt)

	if got, want := span.attributes["git.remote_mirror.outcome"], "notReached"; got != want {
		t.Errorf("outcome attribute = %q, want %q", got, want)
	}
	if got, want := span.attributes["git.remote_mirror.site"], "fresh-clone"; got != want {
		t.Errorf("site attribute = %q, want %q", got, want)
	}
	if got, want := len(span.attributes), 2; got != want {
		t.Errorf("attribute count = %d, want %d: %v", got, want, span.attributes)
	}
	for key, value := range span.attributes {
		if strings.Contains(key, "url") || strings.Contains(value, "mirror.example") || strings.Contains(value, "secret") {
			t.Errorf("metric-shaped telemetry leaks URL data: %q=%q", key, value)
		}
	}
	if got := logs.String(); !strings.Contains(got, "outcome=notReached site=fresh-clone") {
		t.Errorf("job log = %q, want exact outcome and site", got)
	}
	if got := logs.String(); strings.Contains(got, "secret") || !strings.Contains(got, "mirror.example") {
		t.Errorf("job log = %q, want redacted mirror URL", got)
	}
}

func TestGitCredentialHelperCommandQuotesExecutablePath(t *testing.T) {
	t.Parallel()

	ctx := self.OverridePath(t.Context(), "/path with spaces/buildkite-agent")
	got, err := shellwords.Split(gitCredentialHelperCommand(ctx))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/path with spaces/buildkite-agent", "git-credentials-helper"}
	if !slices.Equal(got, want) {
		t.Errorf("credential helper words = %q, want %q", got, want)
	}
}

func TestFetchCommitFromRemoteMirrorOutcomes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test git shim is POSIX-only")
	}

	commit := strings.Repeat("a", 40)
	tests := []struct {
		name        string
		mode        string
		wantHit     bool
		wantOutcome remoteMirrorOutcome
	}{
		{name: "hit", mode: "hit", wantHit: true, wantOutcome: remoteMirrorOutcomeHit},
		{name: "successful fetch without commit", mode: "absent", wantOutcome: remoteMirrorOutcomeMiss},
		{name: "remote does not have object", mode: "miss", wantOutcome: remoteMirrorOutcomeMiss},
		{name: "transport error", mode: "error", wantOutcome: remoteMirrorOutcomeError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newRemoteMirrorShimExecutor(t, commit, tc.mode)
			attempt := remoteMirrorAttempt{
				site: remoteMirrorSiteExistingCheckout,
				url:  "https://mirror.example/acme/widgets.git",
			}

			got, err := e.fetchCommitFromRemoteMirror(t.Context(), &attempt, ".git", "", commit)
			if err != nil {
				t.Fatalf("fetchCommitFromRemoteMirror() error = %v", err)
			}
			if got != tc.wantHit {
				t.Errorf("hit = %t, want %t", got, tc.wantHit)
			}
			if attempt.outcome != tc.wantOutcome {
				t.Errorf("outcome = %q, want %q", attempt.outcome, tc.wantOutcome)
			}
			if attempt.duration <= 0 {
				t.Errorf("duration = %s, want positive", attempt.duration)
			}
		})
	}
}

func TestFetchCommitFromRemoteMirrorTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test git shim is POSIX-only")
	}

	oldTimeout := remoteMirrorProbeTimeout
	remoteMirrorProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { remoteMirrorProbeTimeout = oldTimeout })

	commit := strings.Repeat("a", 40)
	e := newRemoteMirrorShimExecutor(t, commit, "timeout")
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteExistingCheckout,
		url:  "https://mirror.example/acme/widgets.git",
	}

	hit, err := e.fetchCommitFromRemoteMirror(t.Context(), &attempt, ".git", "", commit)
	if err != nil {
		t.Fatalf("fetchCommitFromRemoteMirror() error = %v", err)
	}
	if hit {
		t.Error("hit = true, want false")
	}
	if attempt.outcome != remoteMirrorOutcomeTimeout {
		t.Errorf("outcome = %q, want timeout", attempt.outcome)
	}
}

func TestFetchCommitFromRemoteMirrorPropagatesCancellationDuringConfirmation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test git shim is POSIX-only")
	}

	commit := strings.Repeat("a", 40)
	e := newRemoteMirrorShimExecutor(t, commit, "cancel-confirmation")
	revStarted, _ := e.shell.Env.Get("REV_STARTED")
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteExistingCheckout,
		url:  "https://mirror.example/acme/widgets.git",
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go func() {
		for {
			if _, err := os.Stat(revStarted); err == nil {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	hit, err := e.fetchCommitFromRemoteMirror(ctx, &attempt, ".git", "", commit)
	if hit {
		t.Error("hit = true after cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if attempt.outcome != remoteMirrorOutcomeNotReached {
		t.Errorf("outcome = %q, want not reached", attempt.outcome)
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

func newRemoteMirrorShimExecutor(t *testing.T, commit, mode string) *Executor {
	t.Helper()

	binDir := t.TempDir()
	script := `#!/bin/sh
for arg do
	case "$arg" in
	fetch)
		case "$FETCH_MODE" in
		miss) printf '%s\n' "fatal: remote error: upload-pack: not our ref $EXPECTED_COMMIT" >&2; exit 128 ;;
		error) printf '%s\n' "fatal: transport broke" >&2; exit 1 ;;
		timeout) /bin/sleep 30; exit 1 ;;
		*) exit 0 ;;
		esac
		;;
	rev-parse)
		case "$FETCH_MODE" in
		hit) printf '%s\n' "$EXPECTED_COMMIT"; exit 0 ;;
		cancel-confirmation) : > "$REV_STARTED"; /bin/sleep 30; exit 1 ;;
		*) exit 1 ;;
		esac
		;;
	esac
done
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	e := New(ExecutorConfig{Commit: commit})
	e.shell = shell.NewTestShell(t)
	e.shell.Env.Set("PATH", binDir)
	e.shell.Env.Set("FETCH_MODE", mode)
	e.shell.Env.Set("EXPECTED_COMMIT", commit)
	e.shell.Env.Set("REV_STARTED", filepath.Join(t.TempDir(), "rev-started"))
	return e
}
