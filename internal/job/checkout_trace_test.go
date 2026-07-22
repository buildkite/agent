package job

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/internal/job/githttptest"
	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/agent/v3/tracetools"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestRedactURLCredentials asserts that an embedded password is masked while
// URLs without a secret — plain HTTPS, SCP-style and ssh:// SSH remotes, and
// relative submodule paths — pass through unchanged (never re-encoded).
func TestRedactURLCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "https with token", in: "https://x-access-token:ghs_secret@github.com/org/repo.git", want: "https://x-access-token:xxxxx@github.com/org/repo.git"},
		{name: "https no creds", in: "https://github.com/org/repo.git", want: "https://github.com/org/repo.git"},
		{name: "https user only", in: "https://user@github.com/org/repo.git", want: "https://user@github.com/org/repo.git"},
		{name: "scp-style ssh", in: "git@github.com:org/repo.git", want: "git@github.com:org/repo.git"},
		{name: "ssh scheme", in: "ssh://git@github.com/org/repo.git", want: "ssh://git@github.com/org/repo.git"},
		{name: "relative submodule", in: "../relative/submodule", want: "../relative/submodule"},
		{name: "empty", in: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := redactURLCredentials(tt.in); got != tt.want {
				t.Errorf("redactURLCredentials(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// --- Layer 2: pure-logic tests (noop backend) ---

// TestTraceGitOp_PropagatesError asserts that traceGitOp returns fn's error
// unchanged, both when the --trace-git-checkout flag is on and off. This is the
// property the retry loop and error wrapping depend on. The noop backend is
// sufficient here because we are asserting the return value, not span state.
func TestTraceGitOp_PropagatesError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")

	tests := []struct {
		name             string
		traceGitCheckout bool
		fnErr            error
	}{
		{name: "flag off, error", traceGitCheckout: false, fnErr: sentinel},
		{name: "flag off, nil", traceGitCheckout: false, fnErr: nil},
		{name: "flag on, error", traceGitCheckout: true, fnErr: sentinel},
		{name: "flag on, nil", traceGitCheckout: true, fnErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := &Executor{ExecutorConfig: ExecutorConfig{
				// BackendNone means even with the flag on, tracetools noops.
				TracingBackend:   tracetools.BackendNone,
				TraceGitCheckout: tt.traceGitCheckout,
			}}

			called := false
			got := e.traceGitOp(t.Context(), "git.test", func(ctx context.Context) error {
				called = true
				return tt.fnErr
			})

			if !called {
				t.Fatal("traceGitOp did not call fn")
			}
			if !errors.Is(got, tt.fnErr) {
				t.Fatalf("traceGitOp error = %v, want %v", got, tt.fnErr)
			}
		})
	}
}

// TestTraceGitOpSpan_FlagOffReturnsNoop asserts that when the flag is off,
// traceGitOpSpan returns a NoopSpan and the unmodified ctx, so callers can
// unconditionally call AddAttributes / FinishWithError with no effect.
func TestTraceGitOpSpan_FlagOffReturnsNoop(t *testing.T) {
	t.Parallel()

	e := &Executor{ExecutorConfig: ExecutorConfig{
		TracingBackend:   tracetools.BackendOpenTelemetry,
		TraceGitCheckout: false,
	}}

	ctx := t.Context()
	span, gotCtx := e.traceGitOpSpan(ctx, "git.test")

	if _, ok := span.(*tracetools.NoopSpan); !ok {
		t.Fatalf("traceGitOpSpan span type = %T, want *tracetools.NoopSpan", span)
	}
	if gotCtx != ctx {
		t.Fatal("traceGitOpSpan returned a different ctx when flag is off, want unmodified ctx")
	}

	// These must not panic.
	span.AddAttributes(map[string]string{"git.repo": "x"})
	span.FinishWithError(errors.New("x"))
}

// --- Layer 3: structural span-emission tests (OTel in-memory SpanRecorder) ---
//
// tracetools uses the global OTel tracer, so these tests register the recorder
// as the global provider and therefore cannot use t.Parallel().

// gitSpanNames returns the names of ended spans that use the git.* prefix.
func gitSpanNames(spans []sdktrace.ReadOnlySpan) []string {
	var names []string
	for _, s := range spans {
		if strings.HasPrefix(s.Name(), "git.") {
			names = append(names, s.Name())
		}
	}
	return names
}

// findSpan returns the first ended span with the given name, or nil.
func findSpan(spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	for _, s := range spans {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

// spanAttr returns the string value of the named attribute on a span, and
// whether it was present.
func spanAttr(s sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// runTracedCheckout runs a full default checkout against a throwaway git-over-http
// repository, with the OpenTelemetry backend and the given --trace-git-checkout
// setting, and returns the ended spans recorded during the checkout.
func runTracedCheckout(t *testing.T, traceGitCheckout bool) []sdktrace.ReadOnlySpan {
	t.Helper()

	ctx := t.Context()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}

	// Keep git config out of the home directory.
	t.Setenv("GIT_AUTHOR_NAME", "Buildkite Agent")
	t.Setenv("GIT_AUTHOR_EMAIL", "agent@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Buildkite Agent")
	t.Setenv("GIT_COMMITTER_EMAIL", "agent@example.com")

	const projectName = "trace-checkout"

	s := githttptest.NewServer()
	defer s.Close()

	if err := s.CreateRepository(projectName); err != nil {
		t.Fatalf("s.CreateRepository(%q) error = %v, want nil", projectName, err)
	}
	if out, err := s.InitRepository(projectName); err != nil {
		t.Fatalf("failed to init repository: %v output: %s", err, string(out))
	}
	if _, out, err := s.PushBranch(projectName, "feature-branch"); err != nil {
		t.Fatalf("failed to push branch: %v output: %s", err, string(out))
	}

	buildDir, err := os.MkdirTemp("", "build-path-")
	if err != nil {
		t.Fatalf("os.MkdirTemp error = %v, want nil", err)
	}
	t.Cleanup(func() { os.RemoveAll(buildDir) }) //nolint:errcheck // Best-effort cleanup.

	checkoutDir, err := os.MkdirTemp("", "checkout-path-")
	if err != nil {
		t.Fatalf("os.MkdirTemp error = %v, want nil", err)
	}
	t.Cleanup(func() { os.RemoveAll(checkoutDir) }) //nolint:errcheck // Best-effort cleanup.
	sh.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", checkoutDir)

	executor := &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			Commit:           "HEAD",
			Branch:           "main",
			GitCleanFlags:    "-f -d -x",
			BuildPath:        buildDir,
			Repository:       s.RepoURL(projectName),
			TracingBackend:   tracetools.BackendOpenTelemetry,
			TraceGitCheckout: traceGitCheckout,
		},
	}

	// Register an in-memory recorder as the global provider for the duration of
	// this test, since tracetools uses otel.Tracer("buildkite-agent").
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = provider.Shutdown(context.Background()) //nolint:usetesting // t.Context() is cancelled before Cleanup funcs.
	})

	if err := executor.defaultCheckoutPhase(ctx, 1); err != nil {
		t.Fatalf("executor.defaultCheckoutPhase(ctx, 1) error = %v, want nil", err)
	}

	return recorder.Ended()
}

// TestTraceGitCheckout_FlagOn asserts that with the flag on, the new git.* spans
// are emitted and nest under the repo-checkout span, and that repo-checkout
// carries the checkout.attempt attribute.
func TestTraceGitCheckout_FlagOn(t *testing.T) {
	spans := runTracedCheckout(t, true)

	repoCheckout := findSpan(spans, "repo-checkout")
	if repoCheckout == nil {
		t.Fatal("repo-checkout span not found")
	}
	if got, ok := spanAttr(repoCheckout, "checkout.attempt"); !ok || got != "1" {
		t.Fatalf("repo-checkout checkout.attempt = %q (present=%t), want %q", got, ok, "1")
	}

	// A fresh clone against an empty checkout dir should exercise these spans.
	wantSpans := []string{
		"git.clone",
		"git.clean.pre",
		"git.fetch",
		"git.verify_commit",
		"git.sparse_checkout",
		"git.checkout",
		"git.clean.post",
	}

	repoSpanID := repoCheckout.SpanContext().SpanID()
	for _, name := range wantSpans {
		s := findSpan(spans, name)
		if s == nil {
			t.Errorf("expected span %q not found; got git.* spans: %v", name, gitSpanNames(spans))
			continue
		}
		if parent := s.Parent().SpanID(); parent != repoSpanID {
			t.Errorf("span %q parent = %s, want repo-checkout %s", name, parent, repoSpanID)
		}
	}
}

// TestTraceGitCheckout_FlagOff asserts that with the flag off, no git.* spans are
// emitted, while the unconditional repo-checkout span (with checkout.attempt) is
// still present.
func TestTraceGitCheckout_FlagOff(t *testing.T) {
	spans := runTracedCheckout(t, false)

	if names := gitSpanNames(spans); len(names) != 0 {
		t.Fatalf("git.* spans emitted with flag off: %v, want none", names)
	}

	repoCheckout := findSpan(spans, "repo-checkout")
	if repoCheckout == nil {
		t.Fatal("repo-checkout span not found")
	}
	if got, ok := spanAttr(repoCheckout, "checkout.attempt"); !ok || got != "1" {
		t.Fatalf("repo-checkout checkout.attempt = %q (present=%t), want %q", got, ok, "1")
	}
}
