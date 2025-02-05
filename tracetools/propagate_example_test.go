package tracetools

import (
	"fmt"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/mocktracer"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

func ExampleEncodeTraceContext() {
	// Start and configure the tracer to use the propagator.
	// A more realistic example would connect to, say, a real DataDog agent
	// instead of using mocktracer.
	t := mocktracer.New()
	opentracing.SetGlobalTracer(t)
	defer tracer.Stop()

	childEnv := map[string]string{}

	// Pretend this is the parent process' code
	func() {
		span := opentracing.StartSpan("parent process")
		defer span.Finish()

		span.SetBaggageItem("asd", "zxc")

		// Do stuff..
		time.Sleep(time.Millisecond * 20)

		// Now say we want to launch a child process.
		// Prepare it's env vars. This will be the carrier for the tracing data.
		if err := EncodeTraceContext(span, childEnv, CodecGob{}); err != nil {
			fmt.Println("oops an error for parent process trace injection")
		}
		// Now childEnv will contain the encoded data set with the env var key.
		// Print stuff out for the purpose of the example test.
		if childEnv["BUILDKITE_TRACE_CONTEXT"] == "" {
			fmt.Println("oops empty tracing data in env vars")
		} else {
			fmt.Println("prepared child env carrier data")
		}

		// Normally, you'd now launch a child process with code like:
		// cmd := exec.Command("echo", "hello", "i", "am", "a", "child")
		// cmd.Env = ... // Propagate the env vars here
		// cmd.Run()
		// The important thing is the Env propagation
	}()

	// Pretend this is the child process' code
	func() {
		// Make sure tracing is setup the same way (same env var key)
		// Normally you'd use os.Environ or similar here (the list of strings is
		// supported). We're just reusing childEnv for test simplicity.
		sctx, err := DecodeTraceContext(childEnv, CodecGob{})
		if err != nil {
			fmt.Println("oops an error for child process trace extraction")
		} else {
			fmt.Println("extracted tracing data for child process")
		}

		sctx.ForeachBaggageItem(func(k, v string) bool {
			fmt.Printf("bag: %s=%s\n", k, v)
			return true
		})

		// Now you can start a 'root' span for the child that will also be linked to
		// the parent process.
		span := opentracing.StartSpan("child process", opentracing.ChildOf(sctx))
		defer span.Finish()

		// Do stuff...
		time.Sleep(time.Millisecond * 30)
	}()

	// Output:
	// prepared child env carrier data
	// extracted tracing data for child process
	// bag: asd=zxc
}
