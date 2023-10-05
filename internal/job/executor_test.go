package job

import (
	"context"
	"testing"

	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/redact"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/process"
	"github.com/buildkite/agent/v3/tracetools"
	"github.com/buildkite/shellwords"
	"github.com/google/go-cmp/cmp"
	"github.com/opentracing/opentracing-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/opentracer"
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
		assert.Equal(t, test.expected, dirForAgentName(test.agentName))
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
		assert.Equal(t, test.expected, dirForRepository(test.repositoryName))
	}
}

func TestValuesToRedact(t *testing.T) {
	t.Parallel()

	redactConfig := []string{
		"*_PASSWORD",
		"*_TOKEN",
	}
	environment := map[string]string{
		"BUILDKITE_PIPELINE": "unit-test",
		"DATABASE_USERNAME":  "AzureDiamond",
		"DATABASE_PASSWORD":  "hunter2",
	}

	got := redact.Values(shell.DiscardLogger, redactConfig, environment)
	want := []string{"hunter2"}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("redact.Values(%q, %q) diff (-got +want)\n%s", redactConfig, environment, diff)
	}
}

func TestValuesToRedactEmpty(t *testing.T) {
	t.Parallel()

	redactConfig := []string{}
	environment := map[string]string{
		"FOO":                "BAR",
		"BUILDKITE_PIPELINE": "unit-test",
	}

	got := redact.Values(shell.DiscardLogger, redactConfig, environment)
	if len(got) != 0 {
		t.Errorf("redact.Values(%q, %q) = %q, want empty slice", redactConfig, environment, got)
	}
}

func TestStartTracing_NoTracingBackend(t *testing.T) {
	var err error

	// When there's no tracing backend, the tracer should be a no-op.
	e := New(ExecutorConfig{})

	oriCtx := context.Background()
	e.shell, err = shell.New()
	assert.NoError(t, err)

	span, _, stopper := e.startTracing(oriCtx)
	assert.Equal(t, span, &tracetools.NoopSpan{})
	span.FinishWithError(nil) // Finish the nil span, just for completeness' sake

	// If you call opentracing.GlobalTracer() without having set it first, it returns a NoopTracer
	// In this test case, we haven't touched opentracing at all, so we get the NoopTracer
	assert.IsType(t, opentracing.NoopTracer{}, opentracing.GlobalTracer())
	stopper()
}

func TestStartTracing_Datadog(t *testing.T) {
	var err error

	// With the Datadog tracing backend, the global tracer should be from Datadog.
	cfg := ExecutorConfig{TracingBackend: "datadog"}
	e := New(cfg)

	oriCtx := context.Background()
	e.shell, err = shell.New()
	assert.NoError(t, err)

	span, ctx, stopper := e.startTracing(oriCtx)
	span.FinishWithError(nil)

	assert.IsType(t, opentracer.New(), opentracing.GlobalTracer())
	spanImpl, ok := span.(*tracetools.OpenTracingSpan)
	assert.True(t, ok)

	assert.Equal(t, spanImpl.Span, opentracing.SpanFromContext(ctx))
	stopper()
}

// func TestDefaultCommandPhase_LegacyMode(t *testing.T) {
// 	var err error

// 	bashString := "/bin/bash -e -c"
// 	bashTokens, _ := shellwords.Split(bashString)

// 	cfg := ExecutorConfig{
// 		CommandMode: CommandModeLegacy,
// 		Shell:       bashString,
// 	}
// 	e := New(cfg)
// 	e.shell, err = shell.New()
// 	assert.NoError(t, err)

// 	// don't pollute the test output
// 	e.shell.Logger = shell.DiscardLogger

// 	// the directory where we expect to find repo files
// 	e.shell.Chdir("/tmp")

// 	// we have to create the test file as legacy mode actually checks for its existence and outputs commands differently
// 	os.Create("/tmp/dummy")
// 	defer os.Remove("/tmp/dummy")

// 	trapCtx := context.Background()
// 	noTrapCtx, _ := experiments.Enable(trapCtx, experiments.AvoidRecursiveTrap)

// 	// the error to expect if CommandEval is false and the first token in the command:
// 	// - doesn't refer to a file under shell.wd
// 	// - has more than one token, e.g. arguments
// 	noEvalError := "This agent is not allowed to evaluate console commands. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh)."

// 	type test struct {
// 		context       context.Context
// 		command       string
// 		expect        string
// 		expectedError string
// 		commandEval   bool
// 	}
// 	tests := []test{
// 		// eval allowed
// 		{context: noTrapCtx, command: "echo 'hello'", expect: "echo 'hello'", commandEval: true},
// 		{context: noTrapCtx, command: "dummy", expect: "./dummy", commandEval: true},
// 		{context: noTrapCtx, command: "dummy 1", expect: "dummy 1", commandEval: true},
// 		{context: noTrapCtx, command: "/bin/date", expect: "/bin/date", commandEval: true},
// 		// eval not allowed
// 		{context: noTrapCtx, command: "echo 'hello'", expectedError: noEvalError, commandEval: false},
// 		{context: noTrapCtx, command: "dummy", expect: "./dummy", commandEval: false},
// 		{context: noTrapCtx, command: "dummy 1", expectedError: noEvalError, commandEval: false},
// 		{context: noTrapCtx, command: "/bin/date", expectedError: noEvalError, commandEval: false},
// 		// recursive trap
// 		{context: trapCtx, command: "echo 'hello'", expect: "trap 'kill -- $$' INT TERM QUIT; echo 'hello'", commandEval: true},
// 		{context: trapCtx, command: "dummy", expect: "./dummy", commandEval: true},
// 		{context: trapCtx, command: "dummy 1", expect: "trap 'kill -- $$' INT TERM QUIT; dummy 1", commandEval: true},
// 		{context: trapCtx, command: "/bin/date", expect: "trap 'kill -- $$' INT TERM QUIT; /bin/date", commandEval: true},
// 	}

// 	for _, tc := range tests {
// 		e.Command = tc.command
// 		e.CommandEval = tc.commandEval

// 		runner := new(mockProcessRunner)
// 		if len(tc.expect) > 0 {
// 			runner.Expect(bashTokens[0], append(bashTokens[1:], tc.expect)...)
// 		}
// 		defer runner.Check(t)
// 		e.runner = runner

// 		err = e.defaultCommandPhase(tc.context)
// 		if len(tc.expectedError) > 0 {
// 			require.EqualError(t, err, tc.expectedError)
// 		} else {
// 			require.NoError(t, err)
// 		}
// 	}
// }

func TestDefaultCommandPhase_ShellMode(t *testing.T) {
	var err error

	bashString := "/bin/bash -e -c"
	bashTokens, _ := shellwords.Split(bashString)

	cfg := ExecutorConfig{
		CommandMode: CommandModeShell,
		Shell:       bashString,
	}
	e := New(cfg)
	e.shell, err = shell.New()
	assert.NoError(t, err)

	var processCfg process.Config
	e.shell.NewProcess = func(l logger.Logger, c process.Config) shell.ProcessRunner {
		processCfg = c
		return &mockProcessRunner{}
	}

	tests := []string{
		"echo 'hello'",
		"dummy",
		"dummy 1",
		"dummy\ndummier",
		"/bin/date",
	}

	for _, tc := range tests {
		e.Command = tc
		err = e.defaultCommandPhase(context.Background())
		assert.Equal(t, bashTokens[0], processCfg.Path)
		assert.Equal(t, append(bashTokens[1:], tc), processCfg.Args)
		require.NoError(t, err)
	}
}

var _ shell.ProcessRunner = (*mockProcessRunner)(nil)

// mockProcessRunner implements cmdRunner for testing expected calls.
type mockProcessRunner struct {
}

func (r *mockProcessRunner) Run(_ context.Context) error {
	return nil
}

func (r *mockProcessRunner) Interrupt() error {
	return nil
}

func (r *mockProcessRunner) Terminate() error {
	return nil
}

func (r *mockProcessRunner) WaitResult() error {
	return nil
}

func (r *mockProcessRunner) WaitStatus() process.WaitStatus {
	return nil
}
