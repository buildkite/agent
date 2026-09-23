package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/experiments"
	"github.com/buildkite/agent/v4/internal/replacer"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/internal/socket"
)

func gitErrorCaptureServer(t *testing.T, enabled bool, status int) (context.Context, *Executor, <-chan api.JobCapturedError) {
	t.Helper()
	if !socket.Available() {
		t.Skip("Local Job API unavailable")
	}
	ctx := t.Context()
	if enabled {
		ctx, _ = experiments.Enable(ctx, experiments.CaptureError)
	}
	reports := make(chan api.JobCapturedError, 20)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs/git-job/errors" || r.Header.Get("Authorization") != "Token bkaj_git-token" {
			t.Errorf("unexpected report request: %s %s", r.Method, r.URL.Path)
		}
		var report api.JobCapturedError
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		reports <- report
		if status == 0 {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(upstream.Close)
	sh := shell.NewTestShell(t)
	sh.Env.Set("BUILDKITE_AGENT_ENDPOINT", upstream.URL)
	sh.Env.Set("BUILDKITE_AGENT_ACCESS_TOKEN", "bkaj_git-token")
	e := &Executor{shell: sh, redactors: replacer.NewMux()}
	e.JobID = "git-job"
	e.SocketsPath = os.TempDir()
	cleanup, err := e.startJobAPI(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return ctx, e, reports
}

func TestGitErrorCapture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git fixture uses a POSIX shell")
	}
	t.Parallel()
	for _, tc := range []struct {
		operation, diagnostic, code string
		exit, errorType             int
	}{
		{"fetch", "fatal: couldn't find remote ref secret-ref", "ref_not_found", 128, gitErrorFetchBadReference},
		{"fetch", "fatal: Couldn't find remote ref secret-ref", "ref_not_found", 128, gitErrorFetchBadReference},
		{"fetch", "fatal: remote error: upload-pack: not our ref secret-ref", "git_ref_not_on_remote", 128, gitErrorFetchRefNotOnRemote},
		{"fetch", "error: Server does not allow request for unadvertised object secret-ref", "git_ref_not_on_remote", 128, gitErrorFetchRefNotOnRemote},
		{"fetch", "fatal: bad object secret-ref", "git_bad_object", 128, gitErrorFetchBadObject},
		{"checkout", "fatal: reference is not a tree: secret-ref", "git_reference_not_a_tree", 128, gitErrorCheckoutReferenceIsNotATree},
		{"clone", "fatal: unable to access secret-url: Operation too slow", "git_clone_timeout", 128, gitErrorCloneTimeout},
		{"checkout", "error: secret-path", "git_checkout_failed", 1, gitErrorCheckout},
		{"checkout", "fatal: secret-path", "git_checkout_retry_clean", 128, gitErrorCheckoutRetryClean},
		{"clone", "fatal: authentication failed secret-token", "git_clone_failed", 128, gitErrorClone},
		{"fetch", "error: secret-url", "git_fetch_failed", 1, gitErrorFetch},
		{"fetch", "fatal: secret-url", "git_fetch_retry_clean", 128, gitErrorFetchRetryClean},
		{"clean", "warning: secret-path", "git_clean_failed", 1, gitErrorClean},
		{"submodules", "warning: secret-path", "git_submodule_clean_failed", 1, gitErrorCleanSubmodules},
		{"repack", "fatal: secret-path", "git_repack_failed", 1, gitErrorRepack},
		{"lfs", "error: secret-url", "git_lfs_failed", 2, -1},
		{"fetch", "Fetched successfully", "", 0, -1},
		{"fetch", "fatal: bad object secret-ref", "", 0, -1},
	} {
		t.Run(tc.operation+"/"+tc.diagnostic, func(t *testing.T) {
			t.Parallel()
			for _, enabled := range []bool{false, true} {
				ctx, e, reports := gitErrorCaptureServer(t, enabled, http.StatusCreated)
				sh := e.shell
				dir := t.TempDir()
				script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' %q >&2\nexit %d\n", tc.diagnostic, tc.exit)
				if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				sh.Env.Set("PATH", dir)
				var err error
				switch tc.operation {
				case "fetch":
					err = gitFetch(ctx, gitFetchArgs{Shell: sh, Repository: "secret-url", RefSpecs: []string{"secret-ref"}})
				case "checkout":
					err = gitCheckout(ctx, sh, "-f", "secret-ref")
				case "clone":
					err = gitClone(ctx, sh, nil, nil, "secret-url", "secret-path")
				case "clean":
					err = gitClean(ctx, sh, "-ffxd")
				case "submodules":
					err = gitCleanSubmodules(ctx, sh, "-ffxd")
				case "repack":
					err = gitRepack(ctx, sh, "-a", "-d")
				case "lfs":
					err = gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{Shell: sh})
				}
				if shell.ExitCode(err) != tc.exit {
					t.Fatalf("exit = %d, want %d: %v", shell.ExitCode(err), tc.exit, err)
				}
				if tc.errorType >= 0 {
					var gitErr *gitError
					if !errors.As(err, &gitErr) || gitErr.Type != tc.errorType {
						t.Fatalf("error classification changed: %v", err)
					}
					captureCheckoutError(ctx, sh, fmt.Errorf("secret-wrapper: %w", err))
				}
				want := 0
				if enabled && tc.code != "" {
					want = 1
				}
				if len(reports) != want {
					t.Fatalf("enabled=%v: %d reports, want %d", enabled, len(reports), want)
				}
				if want == 1 {
					r := <-reports
					body, _ := json.Marshal(r)
					if r.Code != tc.code || r.Message == "" || r.Timestamp.IsZero() || r.IdempotencyKey == "" || len(r.Context) != 0 || strings.Contains(string(body), "secret-") {
						t.Fatalf("unexpected or unsafe report: %s", body)
					}
				}
			}
		})
	}
}

func TestGitErrorCaptureRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git fixture uses a POSIX shell")
	}
	t.Parallel()
	for _, tc := range []struct {
		operation, code                  string
		failUntil, attempts, reportCount int
	}{
		{"fetch", "ref_not_found", 2, 3, 2},
		{"lfs", "git_lfs_failed", 5, 5, 1},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			t.Parallel()
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			dir := t.TempDir()
			attemptsFile := filepath.Join(dir, "attempts")
			e.shell.Env.Set("ATTEMPTS_FILE", attemptsFile)
			e.shell.Env.Set("FAIL_UNTIL", fmt.Sprint(tc.failUntil))
			script := `#!/bin/sh
attempt=0
if [ -f "$ATTEMPTS_FILE" ]; then read -r attempt < "$ATTEMPTS_FILE"; fi
attempt=$((attempt + 1))
printf '%s\n' "$attempt" > "$ATTEMPTS_FILE"
if [ "$attempt" -le "$FAIL_UNTIL" ]; then
  echo "fatal: couldn't find remote ref secret-ref" >&2
  exit 128
fi
`
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			e.shell.Env.Set("PATH", dir)
			var err error
			if tc.operation == "fetch" {
				err = gitFetch(ctx, gitFetchArgs{Shell: e.shell, Repository: "secret-url", Retry: true})
				if err != nil {
					t.Fatalf("fetch did not recover: %v", err)
				}
			} else {
				err = gitLFSFetchCheckout(ctx, gitLFSFetchCheckoutArgs{Shell: e.shell, Retry: true})
				var gitErr *gitError
				if !errors.As(err, &gitErr) || gitErr.Type != gitErrorLFS || !gitErr.WasRetried || shell.ExitCode(err) != 128 {
					t.Fatalf("LFS terminal error changed: %v", err)
				}
			}
			captureCheckoutError(ctx, e.shell, err)
			attempts, readErr := os.ReadFile(attemptsFile)
			if readErr != nil || strings.TrimSpace(string(attempts)) != fmt.Sprint(tc.attempts) {
				t.Fatalf("attempts = %q, %v; want %d", attempts, readErr, tc.attempts)
			}
			if len(reports) != tc.reportCount {
				t.Fatalf("reports = %d, want %d", len(reports), tc.reportCount)
			}
			keys := make(map[string]bool)
			for range tc.reportCount {
				r := <-reports
				if r.Code != tc.code || r.IdempotencyKey == "" || keys[r.IdempotencyKey] {
					t.Fatalf("unexpected or duplicate report: %#v", r)
				}
				keys[r.IdempotencyKey] = true
			}
		})
	}
}

func TestCheckoutErrorCapture(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		err        error
	}{
		{"invalid ref", "git_invalid_ref", fmt.Errorf("secret-ref: %w", errInvalidRef)},
		{"verification", "git_commit_verification_failed", fmt.Errorf("secret-ref: %w", ErrCommitVerificationFailed)},
		{"timeout", "git_checkout_timeout", errors.Join(errCheckoutAttemptTimedOut, &gitError{error: &shell.ExitError{Code: -1}, Type: gitErrorClone})},
		{"setup", "git_checkout_failed", errors.New("secret-path")},
		{"cancelled", "", &gitError{error: context.Canceled, Type: gitErrorFetch}},
		{"signalled", "", &gitError{error: &shell.ExitError{Code: -1}, Type: gitErrorClone}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			captureCheckoutError(ctx, e.shell, tc.err)
			if tc.code == "" {
				if len(reports) != 0 {
					t.Fatal("cancellation produced a report")
				}
				return
			}
			if len(reports) != 1 {
				t.Fatalf("reports = %d, want 1", len(reports))
			}
			if r := <-reports; r.Code != tc.code || strings.Contains(r.Message, "secret-") {
				t.Fatalf("unexpected report: %#v", r)
			}
		})
	}
}

func TestGitCaptureDeliveryFailureAndCancellation(t *testing.T) {
	ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusServiceUnavailable)
	gitErr := &gitError{error: &shell.ExitError{Code: 128}, Type: gitErrorClone}
	captureGitError(ctx, e.shell, gitErr)
	captureCheckoutError(ctx, e.shell, gitErr)
	if len(reports) != 1 || shell.ExitCode(gitErr) != 128 {
		t.Fatal("failed delivery was retried or changed the Git result")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	captureGitError(cancelled, e.shell, &gitError{error: errors.New("failed"), Type: gitErrorFetch})
	if len(reports) != 1 {
		t.Fatal("cancelled job reported an error")
	}
	e.shell.Env.Remove("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR")
	captureGitError(ctx, e.shell, &gitError{error: errors.New("failed"), Type: gitErrorFetch})
	if len(reports) != 1 {
		t.Fatal("unavailable Job API was used")
	}
}

func TestGitCaptureDeadline(t *testing.T) {
	ctx, e, reports := gitErrorCaptureServer(t, true, 0)
	start := time.Now()
	captureGitError(ctx, e.shell, &gitError{error: errors.New("failed"), Type: gitErrorFetch})
	if took := time.Since(start); took < time.Second || took > 5*time.Second || len(reports) != 1 {
		t.Fatalf("blocked delivery took %v with %d requests; want one attempt bounded to two seconds", took, len(reports))
	}
}
