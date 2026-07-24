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

// TestTraceGitOp_PropagatesError asserts that traceGitOp returns fn's error
// unchanged, regardless of --trace-git-checkout.
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
				// BackendNone means even with --trace-git-checkout on, tracetools noops.
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

// TestTraceGitOpSpan_FlagOffReturnsNoop asserts that with --trace-git-checkout
// off, traceGitOpSpan returns a NoopSpan and the unmodified ctx.
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

	// Must not panic.
	span.AddAttributes(map[string]string{"git.repo": "x"})
	span.FinishWithError(errors.New("x"))
}

// tracetools uses the global OTel tracer, so these register the recorder as
// the global provider and can't use t.Parallel().

// gitSpanNames returns the names of ended spans with the git.* prefix.
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

// spanAttr returns the string value of the named attribute, and whether it
// was present.
func spanAttr(s sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// runTracedCheckout runs a default checkout against a throwaway git-over-http
// repo with the OpenTelemetry backend and the given --trace-git-checkout
// setting, and returns the ended spans.
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

	// tracetools uses otel.Tracer("buildkite-agent"), so register an
	// in-memory recorder as the global provider for this test.
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = provider.Shutdown(context.Background()) //nolint:usetesting // t.Context() is cancelled before Cleanup funcs.
	})

	if err := executor.defaultCheckoutPhase(ctx, 0); err != nil {
		t.Fatalf("executor.defaultCheckoutPhase(ctx, 0) error = %v, want nil", err)
	}

	return recorder.Ended()
}

// TestTraceGitCheckout_FlagOn asserts that with --trace-git-checkout on,
// git.* spans are emitted, nest under repo-checkout, and repo-checkout has
// checkout.attempt.
func TestTraceGitCheckout_FlagOn(t *testing.T) {
	spans := runTracedCheckout(t, true)

	repoCheckout := findSpan(spans, "repo-checkout")
	if repoCheckout == nil {
		t.Fatal("repo-checkout span not found")
	}
	if got, ok := spanAttr(repoCheckout, "checkout.attempt"); !ok || got != "1" {
		t.Fatalf("repo-checkout checkout.attempt = %q (present=%t), want %q", got, ok, "1")
	}

	// A fresh clone into an empty checkout dir exercises these spans.
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

// TestTraceGitCheckout_FlagOff asserts that with --trace-git-checkout off, no
// git.* spans are emitted, but repo-checkout (with checkout.attempt) still is.
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
