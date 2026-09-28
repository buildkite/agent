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
	if !enabled {
		sh.Env.Remove("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR")
	}
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
		{"fetch", "fatal: couldn't find remote ref secret-ref", "git_ref_not_found", 128, gitErrorFetchBadReference},
		{"fetch", "fatal: Couldn't find remote ref secret-ref", "git_ref_not_found", 128, gitErrorFetchBadReference},
		{"fetch", "fatal: remote error: upload-pack: not our ref secret-ref", "git_ref_not_on_remote", 128, gitErrorFetchRefNotOnRemote},
		{"fetch", "error: Server does not allow request for unadvertised object secret-ref", "git_ref_not_on_remote", 128, gitErrorFetchRefNotOnRemote},
		{"fetch", "fatal: bad object secret-ref", "git_bad_object", 128, gitErrorFetchBadObject},
		{"checkout", "fatal: reference is not a tree: secret-ref", "git_reference_not_a_tree", 128, gitErrorCheckoutReferenceIsNotATree},
		{"clone", "fatal: unable to access secret-url: Operation too slow", "git_clone_timeout", 128, gitErrorCloneTimeout},
		{"checkout", "error: secret-path", "git_checkout_failed", 1, gitErrorCheckout},
		{"checkout", "fatal: secret-path", "git_checkout_unclassified", 128, gitErrorCheckoutRetryClean},
		{"clone", "fatal: authentication failed secret-token", "git_authentication_failed", 128, gitErrorClone},
		{"clone", "fatal: unable to access 'secret-url': Could not resolve host: secret-host", "git_network_failed", 128, gitErrorClone},
		{"fetch", "fatal: Authentication failed for 'secret-url'", "git_authentication_failed", 128, gitErrorFetchRetryClean},
		{"fetch", "secret-user@secret-host: Permission denied (publickey).", "git_authentication_failed", 128, gitErrorFetchRetryClean},
		{"fetch", "secret-user@secret-host: Permission denied (password,keyboard-interactive).", "git_authentication_failed", 128, gitErrorFetchRetryClean},
		{"fetch", "fatal: unable to access 'secret-url': Could not resolve host: secret-host", "git_network_failed", 128, gitErrorFetchRetryClean},
		{"fetch", "fatal: unable to access 'secret-url': Could not resolve proxy: secret-proxy", "git_network_failed", 128, gitErrorFetchRetryClean},
		{"fetch", "ssh: Could not resolve hostname secret-host: Name or service not known", "git_network_failed", 128, gitErrorFetchRetryClean},
		{"fetch", "fatal: unable to access 'secret-url': Failed to connect to secret-host port 443 after 100 ms: Couldn't connect to server", "git_network_failed", 128, gitErrorFetchRetryClean},
		{"fetch", "ssh: connect to host secret-host port 22: Connection refused", "git_network_failed", 128, gitErrorFetchRetryClean},
		{"fetch", "error: cannot open 'secret-path': Permission denied", "git_fetch_unclassified", 128, gitErrorFetchRetryClean},
		{"fetch", "error: secret-url", "git_fetch_failed", 1, gitErrorFetch},
		{"fetch", "fatal: secret-url", "git_fetch_unclassified", 128, gitErrorFetchRetryClean},
		{"clean", "warning: secret-path", "git_clean_failed", 1, gitErrorClean},
		{"submodules", "warning: secret-path", "git_submodule_clean_failed", 1, gitErrorCleanSubmodules},
		{"repack", "fatal: secret-path", "git_repack_failed", 1, gitErrorRepack},
		{"lfs", "error: secret-url", "git_lfs_failed", 2, gitErrorLFS},
		{"fetch", "Fetched successfully", "", 0, -1},
		{"fetch", "fatal: bad object secret-ref", "", 0, -1},
		{"fetch", "fatal: Authentication failed for 'secret-url'", "", 0, -1},
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
					if !errors.As(err, &gitErr) || gitErr.Type != tc.errorType || gitErr.WasRetried {
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
					if r.Code != tc.code || r.Message == "" || r.Timestamp.IsZero() || r.IdempotencyKey == "" || strings.Contains(string(body), "secret-") {
						t.Fatalf("unexpected or unsafe report: %s", body)
					}
					switch tc.code {
					case "git_authentication_failed":
						if r.Message != "Git reported an authentication failure." {
							t.Fatalf("unexpected authentication message: %q", r.Message)
						}
					case "git_network_failed":
						if r.Message != "Git reported a network connection failure." {
							t.Fatalf("unexpected network message: %q", r.Message)
						}
					case "git_checkout_unclassified", "git_fetch_unclassified":
						if r.Message != fmt.Sprintf("Git %s failed with an unclassified error.", tc.operation) {
							t.Fatalf("unexpected unclassified message: %q", r.Message)
						}
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
		{"fetch", "git_ref_not_found", 2, 3, 2},
		{"lfs", "git_lfs_failed", 5, 5, 1},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			t.Parallel()
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			var pending gitErrorReports
			attemptCtx := context.WithValue(ctx, gitErrorReportsKey{}, &pending)
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
				err = gitFetch(attemptCtx, gitFetchArgs{Shell: e.shell, Repository: "secret-url", Retry: true})
				if err != nil {
					t.Fatalf("fetch did not recover: %v", err)
				}
			} else {
				err = gitLFSFetchCheckout(attemptCtx, gitLFSFetchCheckoutArgs{Shell: e.shell, Retry: true})
				var gitErr *gitError
				if !errors.As(err, &gitErr) || gitErr.Type != gitErrorLFS || !gitErr.WasRetried || shell.ExitCode(err) != 128 {
					t.Fatalf("LFS terminal error changed: %v", err)
				}
			}
			if len(reports) != 0 {
				t.Fatal("reported errors before retries finished")
			}
			deliveryStarted := time.Now()
			pending.deliver(ctx, e.shell)
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
				if r.Timestamp.IsZero() || !r.Timestamp.Before(deliveryStarted) {
					t.Fatalf("timestamp = %v, want observation time before delivery at %v", r.Timestamp, deliveryStarted)
				}
				keys[r.IdempotencyKey] = true
			}
		})
	}
}

func TestGitFetchErrorCaptureUsesCurrentAttempt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git fixture uses a POSIX shell")
	}
	t.Parallel()
	ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
	var pending gitErrorReports
	attemptCtx := context.WithValue(ctx, gitErrorReportsKey{}, &pending)
	dir := t.TempDir()
	attemptsFile := filepath.Join(dir, "attempts")
	e.shell.Env.Set("ATTEMPTS_FILE", attemptsFile)
	script := `#!/bin/sh
attempt=0
if [ -f "$ATTEMPTS_FILE" ]; then read -r attempt < "$ATTEMPTS_FILE"; fi
attempt=$((attempt + 1))
printf '%s\n' "$attempt" > "$ATTEMPTS_FILE"
case "$attempt" in
  1) echo "fatal: couldn't find remote ref secret-ref" >&2; exit 128 ;;
  2) echo "fatal: Authentication failed for 'secret-url'" >&2; exit 1 ;;
  3) echo "fatal: unable to access 'secret-url': Could not resolve host: secret-host" >&2; exit 1 ;;
  4) echo "fatal: unable to access 'secret-url': The requested URL returned error: 500" >&2; exit 128 ;;
  *) exit 0 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e.shell.Env.Set("PATH", dir)
	err := gitFetch(attemptCtx, gitFetchArgs{Shell: e.shell, Repository: "secret-url", Retry: true})
	var gitErr *gitError
	if !errors.As(err, &gitErr) || gitErr.Type != gitErrorFetchRetryClean || gitErr.WasRetried || shell.ExitCode(err) != 128 {
		t.Fatalf("fetch recovery classification changed: %v", err)
	}
	pending.deliver(ctx, e.shell)
	captureCheckoutError(ctx, e.shell, err)
	if attempts, err := os.ReadFile(attemptsFile); err != nil || string(attempts) != "4\n" {
		t.Fatalf("attempts = %q, %v; want 4", attempts, err)
	}
	if len(reports) != 4 {
		t.Fatalf("reports = %d, want 4", len(reports))
	}
	for _, code := range []string{"git_ref_not_found", "git_authentication_failed", "git_network_failed", "git_fetch_unclassified"} {
		if report := <-reports; report.Code != code {
			t.Fatalf("report code = %q, want %q", report.Code, code)
		}
	}
}

func TestGitHTTPAuthenticationErrorCapture(t *testing.T) {
	t.Parallel()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(remote.Close)
	for _, operation := range []string{"clone", "fetch"} {
		t.Run(operation, func(t *testing.T) {
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			sh := e.shell
			if err := sh.Chdir(t.TempDir()); err != nil {
				t.Fatal(err)
			}
			sh.Env.Set("GIT_TERMINAL_PROMPT", "0")
			sh.Env.Set("LC_ALL", "C")
			if err := sh.Command("git", "init").Run(ctx); err != nil {
				t.Fatal(err)
			}
			repository := strings.Replace(remote.URL, "http://", "http://secret-user:secret-token@", 1)
			flags := []string{"-c", "credential.helper=", "-c", "http.proxy="}
			var err error
			if operation == "clone" {
				err = gitClone(ctx, sh, flags, nil, repository, "checkout")
			} else {
				err = gitFetch(ctx, gitFetchArgs{Shell: sh, GitFlags: flags, Repository: repository, Retry: true})
			}
			if shell.ExitCode(err) != 128 {
				t.Fatalf("exit = %d, want 128: %v", shell.ExitCode(err), err)
			}
			captureCheckoutError(ctx, sh, err)
			if len(reports) != 1 {
				t.Fatalf("reports = %d, want 1", len(reports))
			}
			if report := <-reports; report.Code != "git_authentication_failed" || report.Message != "Git reported an authentication failure." {
				t.Fatalf("unexpected authentication report: %#v", report)
			}
		})
	}
}

func TestCheckoutErrorCaptureFromExecutor(t *testing.T) {
	ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
	e.Repository = "secret-url"
	e.GitCloneFlags = "'"
	e.CheckoutAttempts = 1
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", filepath.Join(t.TempDir(), "checkout"))
	t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
		}
	})
	if err := e.checkout(ctx); err == nil || !strings.Contains(err.Error(), "splitting --git-clone-flags") {
		t.Fatalf("checkout error = %v, want invalid clone flags", err)
	}
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want 1", len(reports))
	}
	if r := <-reports; r.Code != "git_checkout_phase_failed" || r.Message != "The default checkout phase failed." {
		t.Fatalf("unexpected checkout report: %#v", r)
	}
}

func TestSubmoduleErrorCapture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Git fixture uses a POSIX shell")
	}
	t.Parallel()
	for _, tc := range []struct {
		command, wantError string
	}{
		{"submodule update", "updating submodules: exit status 128"},
		{"submodule foreach", "resetting submodules: exit status 128"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			t.Parallel()
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			dir := t.TempDir()
			script := `#!/bin/sh
if [ "$1" = "config" ]; then
  printf 'submodule.example.url\nsecret-url\000'
fi
if [ "$1 $2" = "$FAIL_COMMAND" ]; then
  echo 'fatal: secret-error' >&2
  exit 128
fi
exit 0
`
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			e.shell.Env.Set("PATH", dir)
			e.shell.Env.Set("FAIL_COMMAND", tc.command)
			err := e.updateGitSubmodules(ctx)
			if err == nil || err.Error() != tc.wantError || shell.ExitCode(err) != 128 {
				t.Fatalf("submodule error = %v, want %q with exit 128", err, tc.wantError)
			}
			captureCheckoutError(ctx, e.shell, err)
			if len(reports) != 1 {
				t.Fatalf("reports = %d, want 1", len(reports))
			}
			if report := <-reports; report.Code != "git_checkout_phase_failed" || report.Message != "The default checkout phase failed." {
				t.Fatalf("unexpected submodule report: %#v", report)
			}
		})
	}
}

func TestCheckoutPreflightErrorCapture(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, sparseMode, wantError string
		code, message               string
		lfsEnabled                  bool
	}{
		{
			name:       "missing git-lfs",
			lfsEnabled: true,
			wantError:  "BUILDKITE_GIT_LFS_ENABLED=true but `git lfs version` failed; git-lfs may not be installed or not resolvable by git: exit status 1",
			code:       "git_lfs_preflight_failed",
			message:    "Git LFS version check failed.",
		},
		{
			name:       "invalid sparse checkout mode",
			sparseMode: "secret-mode",
			wantError:  `invalid sparse checkout mode "secret-mode", must be one of [cone no-cone]`,
			code:       "git_invalid_sparse_checkout_mode",
			message:    "Git checkout received an invalid sparse checkout mode.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.lfsEnabled && runtime.GOOS == "windows" {
				t.Skip("Git fixture uses a POSIX shell")
			}
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			e.Repository = "secret-url"
			e.GitLFSEnabled = tc.lfsEnabled
			e.GitSparseCheckoutMode = tc.sparseMode
			if tc.lfsEnabled {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\necho 'git: lfs is not a git command' >&2\nexit 1\n"), 0o755); err != nil {
					t.Fatal(err)
				}
				e.shell.Env.Set("PATH", dir)
			}
			if err := e.checkout(ctx); err == nil || err.Error() != tc.wantError {
				t.Fatalf("checkout error = %v, want %q", err, tc.wantError)
			}
			if len(reports) != 1 {
				t.Fatalf("reports = %d, want 1", len(reports))
			}
			if r := <-reports; r.Code != tc.code || r.Message != tc.message {
				t.Fatalf("unexpected checkout report: %#v", r)
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
		{"setup", "git_checkout_phase_failed", errors.New("secret-path")},
		{"merge head validation", "git_checkout_phase_failed", &gitError{error: errors.New("unexpected merge head secret-ref"), Type: gitErrorFetch}},
		{"mirror state repair", "git_checkout_phase_failed", &gitError{error: &shell.ExitError{Code: 128}, Type: gitErrorFetchRetryClean}},
		{"lock timeout", "git_checkout_lock_timeout", fmt.Errorf("secret-path: %w", ErrTimedOutAcquiringLock{Name: "secret-lock", Err: context.DeadlineExceeded})},
		{"cancelled preflight", "", errors.Join(errGitLFSPreflight, context.Canceled)},
		{"signalled preflight", "", errors.Join(errGitLFSPreflight, &shell.ExitError{Code: -1})},
		{"cancelled", "", &gitError{error: context.Canceled, Type: gitErrorFetch}},
		{"signalled", "", &gitError{error: &shell.ExitError{Code: -1}, Type: gitErrorClone}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			var before gitError
			if gitErr, ok := tc.err.(*gitError); ok {
				before = *gitErr
			}
			captureCheckoutError(ctx, e.shell, tc.err)
			if gitErr, ok := tc.err.(*gitError); ok {
				if gitErr.Type != before.Type || gitErr.WasRetried != before.WasRetried || gitErr.error != before.error {
					t.Fatal("capture changed the original error or retry behavior")
				}
				// Outer handling must not resubmit the same observation.
				captureCheckoutError(ctx, e.shell, tc.err)
			}
			if tc.code == "" {
				if len(reports) != 0 {
					t.Fatal("cancellation produced a report")
				}
				return
			}
			if len(reports) != 1 {
				t.Fatalf("reports = %d, want 1", len(reports))
			}
			r := <-reports
			if r.Code != tc.code || strings.Contains(r.Message, "secret-") {
				t.Fatalf("unexpected report: %#v", r)
			}
			if tc.code == "git_checkout_phase_failed" && r.Message != "The default checkout phase failed." {
				t.Fatalf("message = %q, want neutral checkout summary", r.Message)
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
	var pending gitErrorReports
	attemptCtx := context.WithValue(cancelled, gitErrorReportsKey{}, &pending)
	captureGitError(attemptCtx, e.shell, &gitError{error: errors.New("failed"), Type: gitErrorFetch})
	if len(pending) != 1 || len(reports) != 1 {
		t.Fatal("error was not buffered before cancellation")
	}
	cancel()
	pending.deliver(cancelled, e.shell)
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

func TestCheckoutErrorCaptureDeliveryDoesNotCauseTimeout(t *testing.T) {
	ctx, e, reports := gitErrorCaptureServer(t, true, 0)
	e.GitCheckoutTimeout = 1
	e.PullRequest = "999"
	e.Commit = "HEAD"
	e.Branch = "main"
	e.GitCleanFlags = "-f -d -x"
	e.PipelineProvider = "github"
	e.PullRequestUsingMergeRefspec = true
	setupCheckoutTestRepo(t, e, "capture-missing-merge-ref")
	t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
		}
	})

	start := time.Now()
	err := e.runDefaultCheckoutAttempt(ctx, 0)
	if errors.Is(err, errCheckoutAttemptTimedOut) {
		t.Fatalf("reporting turned a completed fetch failure into a checkout timeout: %v", err)
	}
	var gitErr *gitError
	if !errors.As(err, &gitErr) || gitErr.Type != gitErrorFetchBadReference || shell.ExitCode(err) != 128 {
		t.Fatalf("checkout error = %v, want missing remote ref with exit 128", err)
	}
	// The upstream stalls until the delivery deadline, beyond the checkout's
	// one-second budget. Outer handling must not submit it again or add a timeout.
	if elapsed := time.Since(start); elapsed < 2*time.Second {
		t.Fatalf("delivery took %v, want its own two-second budget", elapsed)
	}
	captureCheckoutError(ctx, e.shell, err)
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want one missing-ref report", len(reports))
	}
	if report := <-reports; report.Code != "git_ref_not_found" {
		t.Fatalf("report code = %q, want git_ref_not_found", report.Code)
	}
}

func TestCheckoutErrorCaptureGenuineTimeout(t *testing.T) {
	ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(remote.Close)
	e.Repository = remote.URL
	e.GitCheckoutTimeout = 1
	e.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", filepath.Join(t.TempDir(), "checkout"))
	t.Cleanup(func() {
		if e.checkoutRoot != nil {
			_ = e.checkoutRoot.Close()
		}
	})

	err := e.runDefaultCheckoutAttempt(ctx, 0)
	if !errors.Is(err, errCheckoutAttemptTimedOut) {
		t.Fatalf("checkout error = %v, want checkout timeout", err)
	}
	captureCheckoutError(ctx, e.shell, err)
	if len(reports) != 1 {
		t.Fatalf("reports = %d, want one timeout report", len(reports))
	}
	if report := <-reports; report.Code != "git_checkout_timeout" {
		t.Fatalf("report code = %q, want git_checkout_timeout", report.Code)
	}
}
