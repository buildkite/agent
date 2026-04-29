package job

import (
	"context"
	"testing"

	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/agent/v4/tracetools"
)

var agentNameTests = []struct {
	agentName string
	expected  string
}{
	{"My Agent", "My-Agent"},
	{":docker: My Agent", "-docker--My-Agent"},
	{"My \"Agent\"", "My--Agent-"},
}

func TestDirForAgentName(t *testing.T) {
	t.Parallel()

	for _, test := range agentNameTests {
		if got, want := dirForAgentName(test.agentName), test.expected; got != want {
			t.Errorf("dirForAgentName(test.agentName) = %q, want %q", got, want)
		}
	}
}

var repositoryNameTests = []struct {
	repositoryName string
	expected       string
}{
	{"git@github.com:acme-inc/my-project.git", "git-github-com-acme-inc-my-project-git"},
	{"https://github.com/acme-inc/my-project.git", "https---github-com-acme-inc-my-project-git"},
}

func TestDirForRepository(t *testing.T) {
	t.Parallel()

	for _, test := range repositoryNameTests {
		if got, want := dirForRepository(test.repositoryName), test.expected; got != want {
			t.Errorf("dirForRepository(test.repositoryName) = %q, want %q", got, want)
		}
	}
}

func TestStartTracing_NoTracingBackend(t *testing.T) {
	var err error

	// When there's no tracing backend, the span should be non-recording.
	e := New(ExecutorConfig{})

	oriCtx := context.Background()
	e.shell, err = shell.New()
	if err != nil {
		t.Errorf("shell.New() error = %v, want nil", err)
	}

	span, _, stopper := e.startTracing(oriCtx)
	if span.IsRecording() {
		t.Errorf("span.IsRecording() = true, want false (no tracing backend should produce a non-recording span)")
	}
	tracetools.FinishWithError(span, nil) // Finish the span, just for completeness' sake
	stopper()
}
