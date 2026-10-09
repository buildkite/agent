package job

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/redact"
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
	sh.Env.Remove("BUILDKITE_CAPTURE_GIT_ERRORS")
	if enabled {
		sh.Env.Set("BUILDKITE_CAPTURE_GIT_ERRORS", "true")
	}
	return withGitErrorNeedles(ctx, e.redactors.Needles), e, reports
}

func TestGitErrorCaptureOptIn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	t.Parallel()
	for _, tc := range []struct {
		name, optIn, capability string
		want                    bool
	}{
		{"unset", "", "true", false},
		{"disabled", "false", "true", false},
		{"invalid", "yes", "true", false},
		{"enabled", "true", "true", true},
		{"API unavailable", "true", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			sh := shell.NewTestShell(t, shell.WithStdout(&logs))
			sh.Env.Remove("BUILDKITE_CAPTURE_GIT_ERRORS")
			if tc.optIn != "" {
				sh.Env.Set("BUILDKITE_CAPTURE_GIT_ERRORS", tc.optIn)
			}
			sh.Env.Set("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR", tc.capability)
			var output gitErrorOutput
			err := sh.Command("sh", "-c", "echo diagnostic; exit 7").Run(t.Context(), shell.ShowPrompt(false), output.tee(sh))
			if shell.ExitCode(err) != 7 || logs.String() != "diagnostic\n" {
				t.Fatalf("error = %v, logs = %q; want exit 7 and unchanged diagnostic", err, logs.String())
			}
			wantOutput, wantReports := "", 0
			if tc.want {
				wantOutput, wantReports = "diagnostic\n", 1
			}
			if got := output.String(); got != wantOutput {
				t.Fatalf("buffered output = %q, want %q", got, wantOutput)
			}
			var pending gitErrorReports
			ctx := context.WithValue(t.Context(), gitErrorReportsKey{}, &pending)
			captureGitError(ctx, sh, &gitError{error: errors.New("exit status 1"), Type: gitErrorClean}, "")
			if len(pending) != wantReports {
				t.Fatalf("buffered reports = %d, want %d", len(pending), wantReports)
			}
		})
	}
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
		{"checkout", "fatal: reference is not a tree: secret-ref", "git_reference_not_a_tree", 128, gitErrorCheckoutReferenceIsNotATree},
		{"checkout", "error: secret-path", "git_checkout_failed", 1, gitErrorCheckout},
		{"checkout", "fatal: secret-path", "git_checkout_unclassified", 128, gitErrorCheckoutRetryClean},
		{"clean", "warning: secret-path", "git_clean_failed", 1, gitErrorClean},
		{"submodules", "warning: secret-path", "git_submodule_clean_failed", 1, gitErrorCleanSubmodules},
		{"repack", "fatal: secret-path", "git_repack_failed", 1, gitErrorRepack},
		{"lfs", "error: secret-url", "git_lfs_failed", 2, gitErrorLFS},
		{"lfs-checkout", "error: unable to write secret-path", "git_lfs_failed", 2, gitErrorLFS},
	} {
		t.Run(tc.operation+"/"+tc.diagnostic, func(t *testing.T) {
			t.Parallel()
			for _, enabled := range []bool{false, true} {
				ctx, e, reports := gitErrorCaptureServer(t, enabled, http.StatusCreated)
				e.redactors.Append(redact.New(io.Discard, []string{"secret-ref", "secret-url", "secret-path", "secret-token", "secret-host", "secret-proxy", "secret-user"}))
				sh := e.shell
				dir := t.TempDir()
				script := "#!/bin/sh\n"
				if tc.operation == "lfs-checkout" {
					// Successful fetch output must not consume the failed checkout's budget.
					script += fmt.Sprintf("if [ \"$2\" = fetch ]; then printf '%%s' %q; exit 0; fi\n", strings.Repeat("x", 4097))
				}
				script += fmt.Sprintf("printf '%%s\\n' %q >&2\nexit %d\n", tc.diagnostic, tc.exit)
				if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				sh.Env.Set("PATH", dir)
				var err error
				switch tc.operation {
				case "checkout":
					err = gitCheckout(ctx, sh, "-f", "secret-ref")
				case "clean":
					err = gitClean(ctx, sh, "-ffxd")
				case "submodules":
					err = gitCleanSubmodules(ctx, sh, "-ffxd")
				case "repack":
					err = gitRepack(ctx, sh, "-a", "-d")
				case "lfs", "lfs-checkout":
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
					wantMessage := strings.NewReplacer(
						"secret-ref", "[REDACTED]", "secret-url", "[REDACTED]",
						"secret-path", "[REDACTED]", "secret-token", "[REDACTED]",
						"secret-host", "[REDACTED]", "secret-proxy", "[REDACTED]",
						"secret-user", "[REDACTED]",
					).Replace(tc.diagnostic) + "\n"
					if !strings.HasSuffix(r.Message, ".\n\nLast lines of Git output:\n"+wantMessage) {
						t.Fatalf("message = %q, want a summary and Git's redacted output %q", r.Message, wantMessage)
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
  echo "fatal: couldn't find remote ref attempt-$attempt" >&2
  exit 128
fi
`
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			e.shell.Env.Set("PATH", dir)
			err := gitLFSFetchCheckout(attemptCtx, gitLFSFetchCheckoutArgs{Shell: e.shell, Retry: true})
			var gitErr *gitError
			if !errors.As(err, &gitErr) || gitErr.Type != gitErrorLFS || !gitErr.WasRetried || shell.ExitCode(err) != 128 {
				t.Fatalf("LFS terminal error changed: %v", err)
			}
			if len(reports) != 0 {
				t.Fatal("reported errors before retries finished")
			}
			deliveryStarted := time.Now()
			pending.deliver(ctx, e.shell)
			attempts, readErr := os.ReadFile(attemptsFile)
			if readErr != nil || strings.TrimSpace(string(attempts)) != fmt.Sprint(tc.attempts) {
				t.Fatalf("attempts = %q, %v; want %d", attempts, readErr, tc.attempts)
			}
			if len(reports) != tc.reportCount {
				t.Fatalf("reports = %d, want %d", len(reports), tc.reportCount)
			}
			keys := make(map[string]bool)
			for i := range tc.reportCount {
				r := <-reports
				if r.Code != tc.code || r.IdempotencyKey == "" || keys[r.IdempotencyKey] {
					t.Fatalf("unexpected or duplicate report: %#v", r)
				}
				attempt := i + 1
				if tc.operation == "lfs" {
					attempt = tc.attempts
				}
				if want := fmt.Sprintf("\nfatal: couldn't find remote ref attempt-%d\n", attempt); !strings.HasSuffix(r.Message, want) {
					t.Fatalf("message = %q, want current attempt's output %q", r.Message, want)
				}
				if r.Timestamp.IsZero() || !r.Timestamp.Before(deliveryStarted) {
					t.Fatalf("timestamp = %v, want observation time before delivery at %v", r.Timestamp, deliveryStarted)
				}
				keys[r.IdempotencyKey] = true
			}
		})
	}
}

func TestGitCaptureDeliveryFailureAndCancellation(t *testing.T) {
	ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusServiceUnavailable)
	gitErr := &gitError{error: &shell.ExitError{Code: 128}, Type: gitErrorClean}
	captureGitError(ctx, e.shell, gitErr, "")
	if len(reports) != 1 || shell.ExitCode(gitErr) != 128 {
		t.Fatal("failed delivery was retried or changed the Git result")
	}
	cancelled, cancel := context.WithCancel(ctx)
	var pending gitErrorReports
	attemptCtx := context.WithValue(cancelled, gitErrorReportsKey{}, &pending)
	captureGitError(attemptCtx, e.shell, &gitError{error: errors.New("failed"), Type: gitErrorClean}, "")
	if len(pending) != 1 || len(reports) != 1 {
		t.Fatal("error was not buffered before cancellation")
	}
	cancel()
	pending.deliver(cancelled, e.shell)
	captureGitError(cancelled, e.shell, &gitError{error: errors.New("failed"), Type: gitErrorClean}, "")
	if len(reports) != 1 {
		t.Fatal("cancelled job reported an error")
	}
	e.shell.Env.Remove("BUILDKITE_AGENT_JOB_API_CAPTURE_ERROR")
	captureGitError(ctx, e.shell, &gitError{error: errors.New("failed"), Type: gitErrorClean}, "")
	if len(reports) != 1 {
		t.Fatal("unavailable Job API was used")
	}
}

func TestGitCaptureDeadline(t *testing.T) {
	ctx, e, reports := gitErrorCaptureServer(t, true, 0)
	start := time.Now()
	captureGitError(ctx, e.shell, &gitError{error: errors.New("failed"), Type: gitErrorClean}, "")
	if took := time.Since(start); took < time.Second || took > 5*time.Second || len(reports) != 1 {
		t.Fatalf("blocked delivery took %v with %d requests; want one attempt bounded to two seconds", took, len(reports))
	}
}

func TestGitErrorOutputCapture(t *testing.T) {
	t.Parallel()
	const fallback = "Git checkout failed with an unclassified error."
	const heading = "\n\nLast lines of Git output:\n"
	// Each numbered line is 9 characters, so 91 of them fit after the summary.
	var numbered []string
	for i := range 200 {
		numbered = append(numbered, fmt.Sprintf("line %03d\n", i))
	}
	for _, tc := range []struct {
		name, want string
		chunks     []string
	}{
		{"empty", fallback, nil},
		{"whitespace", fallback, []string{" \n\t"}},
		{"keeps the last lines that fit", fallback + heading + strings.Join(numbered[109:], ""), numbered},
		{"single line too long for the report", fallback, []string{"fatal: " + strings.Repeat("x", maxGitErrorMessage) + "\n"}},
		{"long earlier line is dropped", fallback + heading + "fatal: later output", []string{strings.Repeat("x", maxGitErrorMessage) + "\n", "fatal: later output"}},
		{"output beyond the buffer", fallback + "\n\n" + gitOutputOmitted, []string{strings.Repeat("x", maxGitErrorOutput), "secret", "-suffix\n", "fatal: later output\n"}},
		{"line that fits once its URL is masked", fallback + heading + "fatal: unable to access 'https://xxxxx@example.com/repo?[REDACTED]': denied\n", []string{"fatal: unable to access 'https://user:pass@example.com/repo?sig=" + strings.Repeat("s", maxGitErrorMessage) + "': denied\n"}},
		{"nul", fallback, []string{"fatal: secret\x00-suffix"}},
		{"invalid utf8", fallback, []string{"fatal: secret\xff-suffix"}},
		{"split secret", fallback + heading + "fatal: [REDACTED]\n", []string{"fatal: secret", "-suffix\n"}},
		{"multiline secret", fallback + heading + "fatal: \n[REDACTED]\n\n", []string{"fatal: \nsecret\n", "part\n\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			var output gitErrorOutput
			for _, chunk := range tc.chunks {
				if n, err := io.WriteString(&output, chunk); err != nil || n != len(chunk) {
					t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(chunk))
				}
			}
			var pending gitErrorReports
			attemptCtx := context.WithValue(ctx, gitErrorReportsKey{}, &pending)
			captureGitError(attemptCtx, e.shell, &gitError{error: errors.New("exit status 128"), Type: gitErrorCheckoutRetryClean}, output.String())
			// Secrets registered after observation must still be redacted at delivery.
			e.redactors.Append(redact.New(io.Discard, []string{"secret-suffix", "secret\npart"}))
			pending.deliver(ctx, e.shell)
			if len(reports) != 1 {
				t.Fatalf("reports = %d, want 1", len(reports))
			}
			if report := <-reports; report.Code != "git_checkout_unclassified" || report.Message != tc.want {
				t.Fatalf("report = %#v, want unclassified code and message %q", report, tc.want)
			}
		})
	}
}

func TestGitErrorOutputRedactsSecretsBeforeTheCut(t *testing.T) {
	t.Parallel()
	// A registered multi-line secret whose first line is too long to keep:
	// cutting before redaction would leave its last line unmatched.
	for _, tc := range []struct {
		name       string
		firstLine  int
		wantSuffix string
	}{
		{"too long for the report", maxGitErrorMessage, "remote: [REDACTED]\nfatal: unable to read from remote\n"},
		{"too long for the buffer", maxGitErrorOutput, "\n\n" + gitOutputOmitted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, e, reports := gitErrorCaptureServer(t, true, http.StatusCreated)
			secret := strings.Repeat("A", tc.firstLine) + "\nPRIVATE_CREDENTIAL_SUFFIX"
			e.redactors.Append(redact.New(io.Discard, []string{secret}))
			var output gitErrorOutput
			_, _ = io.WriteString(&output, "remote: "+secret+"\nfatal: unable to read from remote\n")
			captureGitError(ctx, e.shell, &gitError{error: errors.New("exit status 128"), Type: gitErrorClean}, output.String())
			report := <-reports
			if strings.Contains(report.Message, "PRIVATE_CREDENTIAL_SUFFIX") || !strings.HasSuffix(report.Message, tc.wantSuffix) {
				t.Errorf("message = %q, want the secret left out and suffix %q", report.Message, tc.wantSuffix)
			}
		})
	}
}
