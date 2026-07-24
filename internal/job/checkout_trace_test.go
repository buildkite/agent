package job

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
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

// TestTraceGitCheckout_FlagOn asserts that with --trace-git-checkout on,
// git.* spans are emitted, nest under repo-checkout, and repo-checkout has
// checkout.attempt.
func TestTraceGitCheckout_FlagOn(t *testing.T) {
	spans := runTracedCheckout(t, true)

	repoCheckout := findOnlySpan(t, spans, "repo-checkout")
	assertSpanAttr(t, repoCheckout, "checkout.attempt", "1")

	// A fresh clone into an empty checkout dir exercises these spans, all
	// nested directly under repo-checkout.
	wantSpans := []string{
		"git.clone",
		"git.clean.pre",
		"git.fetch",
		"git.verify_commit",
		"git.sparse_checkout",
		"git.checkout",
		"git.clean.post",
	}
	for _, name := range wantSpans {
		assertSpanChildOf(t, spans, name, "repo-checkout")
	}
}

// TestTraceGitCheckout_FlagOff asserts that with --trace-git-checkout off, no
// git.* spans are emitted, but repo-checkout (with checkout.attempt) still is.
func TestTraceGitCheckout_FlagOff(t *testing.T) {
	spans := runTracedCheckout(t, false)

	if names := gitSpanNames(spans); len(names) != 0 {
		t.Fatalf("git.* spans emitted with flag off: %v, want none", names)
	}

	repoCheckout := findOnlySpan(t, spans, "repo-checkout")
	assertSpanAttr(t, repoCheckout, "checkout.attempt", "1")
}

// TestTraceGitCheckout_Mirrors asserts the git.mirror.* and git.dissociate
// spans are emitted. Two runs share a mirror path and checkout dir: run 1
// clones a fresh mirror, run 2 updates it and dissociates the existing clone.
func TestTraceGitCheckout_Mirrors(t *testing.T) {
	e, _ := newTracedExecutor(t, "trace-mirrors")

	e.GitMirrorsPath = tracingTempDir(t, "git-mirrors-")
	e.GitMirrorsLockTimeout = 30 // seconds; ample for a local, uncontended mirror.
	e.GitMirrorCheckoutMode = "dissociate"
	e.CleanCheckout = true // For git.mirror.snapshot.

	recorder := installGlobalSpanRecorder(t)

	// Run 1: fresh mirror -> clone and snapshot.
	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("first defaultCheckoutPhase error = %v, want nil", err)
	}
	// Run 2: mirror and .git exist; Commit=HEAD forces a fetch and dissociate.
	if err := e.defaultCheckoutPhase(t.Context(), 1); err != nil {
		t.Fatalf("second defaultCheckoutPhase error = %v, want nil", err)
	}

	spans := recorder.Ended()

	// Mirror sub-operations nest under git.mirror.update, itself under repo-checkout.
	assertSpanChildOf(t, spans, "git.mirror.update", "repo-checkout")
	for _, name := range []string{
		"git.mirror.lock_wait.clone",
		"git.mirror.clone",
		"git.mirror.lock_wait.update",
		"git.mirror.fetch",
		"git.mirror.snapshot",
	} {
		assertSpanChildOf(t, spans, name, "git.mirror.update")
	}

	// Dissociation happens directly under repo-checkout.
	assertSpanChildOf(t, spans, "git.dissociate", "repo-checkout")
}

// TestTraceGitCheckout_Submodules asserts the git.submodules and
// git.submodule.update spans are emitted for a repo with a submodule.
func TestTraceGitCheckout_Submodules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local submodule wiring is finicky on Windows temp dirs")
	}

	e, s := newTracedExecutor(t, "trace-submodules-main")
	addTestSubmodule(t, s, "trace-submodules-main", "trace-submodules-sub")
	e.GitSubmodules = true

	recorder := installGlobalSpanRecorder(t)

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase error = %v, want nil", err)
	}

	spans := recorder.Ended()

	assertSpanChildOf(t, spans, "git.submodules", "repo-checkout")
	assertSpanChildOf(t, spans, "git.submodule.update", "git.submodules")
}

// TestTraceGitCheckout_GitLFS asserts the git.lfs.install and git.lfs.fetch
// spans are emitted when Git LFS is enabled. Skipped if git-lfs is absent.
func TestTraceGitCheckout_GitLFS(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("git-lfs not installed")
	}

	e, _ := newTracedExecutor(t, "trace-lfs")
	e.GitLFSEnabled = true

	recorder := installGlobalSpanRecorder(t)

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("defaultCheckoutPhase error = %v, want nil", err)
	}

	spans := recorder.Ended()

	assertSpanChildOf(t, spans, "git.lfs.install", "repo-checkout")
	assertSpanChildOf(t, spans, "git.lfs.fetch", "repo-checkout")
}

// runTracedCheckout runs a default checkout against a throwaway git-over-http
// repo with the given --trace-git-checkout setting, and returns the ended spans.
func runTracedCheckout(t *testing.T, traceGitCheckout bool) []sdktrace.ReadOnlySpan {
	t.Helper()

	e, _ := newTracedExecutor(t, "trace-checkout")
	e.TraceGitCheckout = traceGitCheckout

	recorder := installGlobalSpanRecorder(t)

	if err := e.defaultCheckoutPhase(t.Context(), 0); err != nil {
		t.Fatalf("executor.defaultCheckoutPhase(ctx, 0) error = %v, want nil", err)
	}

	return recorder.Ended()
}

// newTracedExecutor builds an Executor wired to a throwaway git-over-http repo,
// with the OpenTelemetry backend and git-checkout tracing enabled.
func newTracedExecutor(t *testing.T, projectName string) (*Executor, *githttptest.Server) {
	t.Helper()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v, want nil", err)
	}

	e := &Executor{
		shell: sh,
		ExecutorConfig: ExecutorConfig{
			// Commit "HEAD" (rather than the pushed hash returned by
			// setupCheckoutTestRepo) intentionally exercises the fresh-clone
			// path, which is why that hash is discarded below.
			Commit:           "HEAD",
			Branch:           "main",
			GitCleanFlags:    "-f -d -x",
			TracingBackend:   tracetools.BackendOpenTelemetry,
			TraceGitCheckout: true,
		},
	}

	s, _ := setupCheckoutTestRepo(t, e, projectName)
	return e, s
}

// addTestSubmodule creates subRepo on s and adds it as a submodule on mainRepo's
// main branch, so a checkout of main HEAD has a submodule to initialise. Relies
// on the git identity env set by setupCheckoutTestRepo.
func addTestSubmodule(t *testing.T, s *githttptest.Server, mainRepo, subRepo string) {
	t.Helper()

	if err := s.CreateRepository(subRepo); err != nil {
		t.Fatalf("s.CreateRepository(%q) error = %v, want nil", subRepo, err)
	}
	if out, err := s.InitRepository(subRepo); err != nil {
		t.Fatalf("s.InitRepository(%q) error = %v, output: %s", subRepo, err, string(out))
	}

	work := tracingTempDir(t, "submodule-add-")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s error = %v, output: %s", strings.Join(args, " "), err, string(out))
		}
	}

	// -b main: don't rely on remote HEAD, which may point at master, not main.
	git("", "clone", "-b", "main", s.RepoURL(mainRepo), work)
	git(work, "submodule", "add", "-b", "main", s.RepoURL(subRepo), "sub")
	git(work, "commit", "-m", "Add submodule")
	git(work, "push", "origin", "main")
}

// installGlobalSpanRecorder swaps in an in-memory recorder as the *global* OTel
// provider (tracetools uses the global tracer), restoring it on cleanup. Callers
// mutate global state, so can't t.Parallel().
func installGlobalSpanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = provider.Shutdown(context.Background()) //nolint:usetesting // t.Context() is cancelled before Cleanup funcs.
	})
	return recorder
}

// tracingTempDir makes a temp dir with best-effort cleanup. Not t.TempDir():
// on Windows git child processes can hold handles past exit, which
// t.TempDir()'s strict cleanup would fail on.
func tracingTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatalf("os.MkdirTemp(%q) error = %v, want nil", prefix, err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) }) //nolint:errcheck // Best-effort cleanup.
	return dir
}

// assertSpanChildOf asserts some span named childName has a parent named
// parentName. Tolerates repeated names (e.g. mirror spans, once per run).
func assertSpanChildOf(t *testing.T, spans []sdktrace.ReadOnlySpan, childName, parentName string) {
	t.Helper()
	for _, child := range spans {
		if child.Name() != childName {
			continue
		}
		parentID := child.Parent().SpanID()
		for _, p := range spans {
			if p.SpanContext().SpanID() == parentID && p.Name() == parentName {
				return
			}
		}
	}
	t.Errorf("no %q span with parent %q; git.* spans present: %v", childName, parentName, gitSpanNames(spans))
}

// findOnlySpan asserts exactly one ended span has the given name and returns
// it, so attribute assertions can't silently target one of several.
func findOnlySpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var matches []sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == name {
			matches = append(matches, s)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("found %d %q spans, want exactly 1", len(matches), name)
	}
	return matches[0]
}

// assertSpanAttr asserts the named span attribute is present and equals want.
func assertSpanAttr(t *testing.T, s sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	got, ok := spanAttr(s, key)
	if !ok || got != want {
		t.Fatalf("span %q attribute %q = %q (present=%t), want %q", s.Name(), key, got, ok, want)
	}
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
