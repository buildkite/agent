package tracetools

import (
	"fmt"
	"time"

	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

func ExampleEnvVarPropagator() {
	// Create a new propagator with the env var key you want to use to pass encoded
	// tracing data with.
	prop := NewEnvVarPropagator("ASD")
	// Start and configure the tracer to use the propagator.
	tracer.Start(
		tracer.WithPropagator(prop),
		// The rest of these args are just to ensure nothing actually gets sent if
		// the test platform actually has a DD agent running locally.
		// This is an unlikely, local, non-default agent address.
		tracer.WithAgentAddr("10.0.0.1:65534"),
		tracer.WithLogger(&nullLogger{}),
	)
	defer tracer.Stop()

	childEnv := map[string]string{}

	// Pretend this is the parent process' code
	func() {
		span := tracer.StartSpan("parent process")
		defer span.Finish()

		// Do stuff..
		time.Sleep(time.Millisecond * 20)

		// Now say we want to launch a child process.
		// Prepare it's env vars. This will be the carrier for the tracing data.
		if err := tracer.Inject(span.Context(), childEnv); err != nil {
			fmt.Println("oops an error for parent process trace injection")
		}
		// Now childEnv will contain the encoded data set with the "ASD" env var key.
		// Print stuff out for the purpose of the example test.
		if childEnv["ASD"] == "" {
			fmt.Println("oops empty tracing data in env vars")
		} else {
			fmt.Println("prepared child env carrier data")
		}

		// Normally, you'd now launch a child process with code like:
		// cmd := exec.Command("echo", "hello", "i", "am", "a", "child")
		// cmd.Env = ... // Propagate the env var here
		// cmd.Run()
		// The important thing is the Env propagation
	}()

	// Pretend this is the child process' code
	func() {
		// Make sure tracing is setup the same way (same env var key)
		// Normally you'd use os.Environ or similar here (the list of strings is
		// supported). We're just reusing childEnv for test simplicity.
		sctx, err := tracer.Extract(childEnv)
		if err != nil {
			fmt.Println("oops an error for child process trace extraction")
		} else {
			fmt.Println("extracted tracing data for child process")
		}

		// Now you can start a 'root' span for the child that will also be linked to
		// the parent process.
		span := tracer.StartSpan("child process", tracer.ChildOf(sctx))
		defer span.Finish()

		// Do stuff...
		time.Sleep(time.Millisecond * 30)
	}()

	// Output:
	// prepared child env carrier data
	// extracted tracing data for child process
}
