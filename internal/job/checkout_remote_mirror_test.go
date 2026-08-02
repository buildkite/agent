package job

import (
	"bytes"
	"context"
	"errors"
	"maps"
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

func TestRemoteMirrorTelemetrySchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		attempt         remoteMirrorAttempt
		wantAttrs       map[string]string
		wantLogContains []string
	}{
		{
			name: "not reached",
			attempt: remoteMirrorAttempt{
				site:    remoteMirrorSiteFreshClone,
				url:     "https://token:secret@mirror.example/acme/widgets.git",
				outcome: remoteMirrorOutcomeNotReached,
			},
			wantAttrs: map[string]string{
				"git.remote_mirror.outcome": "notReached",
				"git.remote_mirror.site":    "fresh-clone",
			},
			wantLogContains: []string{"outcome=notReached site=fresh-clone", "mirror.example"},
		},
		{
			name: "skipped",
			attempt: remoteMirrorAttempt{
				outcome:    remoteMirrorOutcomeSkipped,
				skipReason: remoteMirrorSkipNoURL,
			},
			wantAttrs: map[string]string{
				"git.remote_mirror.outcome":     "skipped",
				"git.remote_mirror.site":        "none",
				"git.remote_mirror.skip_reason": "no-url",
			},
			wantLogContains: []string{"outcome=skipped site=none skip_reason=no-url"},
		},
		{
			name: "hit with duration",
			attempt: remoteMirrorAttempt{
				site:     remoteMirrorSiteExistingCheckout,
				url:      "https://mirror.example/acme/widgets.git",
				outcome:  remoteMirrorOutcomeHit,
				duration: 1500 * time.Millisecond,
			},
			wantAttrs: map[string]string{
				"git.remote_mirror.outcome":     "hit",
				"git.remote_mirror.site":        "existing-checkout",
				"git.remote_mirror.duration_ms": "1500",
			},
			wantLogContains: []string{"outcome=hit site=existing-checkout duration=1.5s", "mirror.example"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			span := &recordingRemoteMirrorSpan{}
			e := New(ExecutorConfig{})
			var logs bytes.Buffer
			e.shell = shell.NewTestShell(t, shell.WithLogger(shell.NewWriterLogger(&logs, false, nil)))

			e.emitRemoteMirrorTelemetry(span, tc.attempt)

			if !maps.Equal(span.attributes, tc.wantAttrs) {
				t.Errorf("attributes = %v, want %v", span.attributes, tc.wantAttrs)
			}
			for key, value := range span.attributes {
				if strings.Contains(key, "url") || strings.Contains(value, "mirror.example") || strings.Contains(value, "secret") {
					t.Errorf("metric-shaped telemetry leaks URL data: %q=%q", key, value)
				}
			}
			for _, want := range tc.wantLogContains {
				if got := logs.String(); !strings.Contains(got, want) {
					t.Errorf("job log = %q, want substring %q", got, want)
				}
			}
			if strings.Contains(logs.String(), "secret") {
				t.Errorf("job log leaks URL credentials: %q", logs.String())
			}
		})
	}
}

func TestGitCredentialHelperCommandQuotesExecutablePath(t *testing.T) {
	t.Parallel()

	ctx := self.OverridePath(t.Context(), "/path with spaces/buildkite-agent")
	helper := gitCredentialHelperCommand(ctx)
	if !strings.HasPrefix(helper, "!") {
		t.Fatalf("credential helper = %q, want Git shell-snippet prefix !", helper)
	}
	got, err := shellwords.Split(strings.TrimPrefix(helper, "!"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/path with spaces/buildkite-agent", "git-credentials-helper"}
	if !slices.Equal(got, want) {
		t.Errorf("credential helper words = %q, want %q", got, want)
	}
}

func TestFormatDebugEnvironmentVariableRedactsRemoteMirrorCredentials(t *testing.T) {
	t.Parallel()

	got := formatDebugEnvironmentVariable(
		"BUILDKITE_GIT_REMOTE_MIRROR_URL=https://token:secret@mirror.example/repo.git",
	)
	if strings.Contains(got, "secret") {
		t.Errorf("formatDebugEnvironmentVariable() = %q, want URL credentials redacted", got)
	}
	if want := "BUILDKITE_GIT_REMOTE_MIRROR_URL=https://token:xxxxx@mirror.example/repo.git"; got != want {
		t.Errorf("formatDebugEnvironmentVariable() = %q, want %q", got, want)
	}
}

func TestRemoteMirrorBulkGitFlags(t *testing.T) {
	t.Parallel()

	flags := remoteMirrorBulkGitFlags(t.Context())
	joined := strings.Join(flags, "\n")
	for _, want := range []string{
		"http.lowSpeedLimit=1000",
		"http.lowSpeedTime=60",
		"protocol.version=2",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("remoteMirrorBulkGitFlags() = %q, want %q", flags, want)
		}
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

func TestFetchCommitFromRemoteMirrorHidesURLPromptInDebug(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test git shim is POSIX-only")
	}

	commit := strings.Repeat("a", 40)
	e := newRemoteMirrorShimExecutor(t, commit, "hit")
	logs := &bytes.Buffer{}
	sh, err := shell.New(
		shell.WithDebug(true),
		shell.WithEnv(e.shell.Env),
		shell.WithStdout(logs),
	)
	if err != nil {
		t.Fatal(err)
	}
	e.shell = sh
	attempt := remoteMirrorAttempt{
		site: remoteMirrorSiteExistingCheckout,
		url:  "https://token:secret@mirror.example/acme/widgets.git",
	}

	hit, err := e.fetchCommitFromRemoteMirror(t.Context(), &attempt, ".git", "", commit)
	if err != nil {
		t.Fatalf("fetchCommitFromRemoteMirror() error = %v", err)
	}
	if !hit {
		t.Fatal("fetchCommitFromRemoteMirror() hit = false, want true")
	}
	if strings.Contains(logs.String(), attempt.url) || strings.Contains(logs.String(), "secret") {
		t.Errorf("debug job log leaks mirror URL credentials: %q", logs.String())
	}
}

func TestFetchCommitFromRemoteMirrorTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test git shim is POSIX-only")
	}

	oldTimeout := remoteMirrorProbeTimeout
	remoteMirrorProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { remoteMirrorProbeTimeout = oldTimeout })

	for _, mode := range []string{"timeout", "timeout-confirmation"} {
		t.Run(mode, func(t *testing.T) {
			commit := strings.Repeat("a", 40)
			e := newRemoteMirrorShimExecutor(t, commit, mode)
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
		})
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
	absoluteGit, err := e.shell.AbsolutePath("git")
	if err != nil {
		t.Fatal(err)
	}
	wantFetch := []string{absoluteGit, "--git-dir", ".git"}
	wantFetch = append(wantFetch, remoteMirrorGitFlags(t.Context())...)
	wantFetch = append(wantFetch, "fetch", "--", attempt.url, e.Commit)
	if !slices.Equal(gotLog[0], wantFetch) {
		t.Errorf("git fetch args = %q, want %q", gotLog[0], wantFetch)
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
		timeout-confirmation) /bin/sleep 30; exit 1 ;;
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
