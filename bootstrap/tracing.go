package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/env"
	"github.com/opentracing/opentracing-go"
	"golang.org/x/exp/slices"
	ddext "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/opentracer"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// stopper lets us abstract the tracer wrap up code so we can plug in different tracing
// library implementations that are opentracing compatible. Opentracing itself
// doesn't have a Stop function on its Tracer interface.
type stopper func()

func noopStopper() {}

func (b *Bootstrap) startTracing(ctx context.Context) (any, context.Context, stopper) {
	switch b.Config.TracingBackend {
	case "datadog":
		return b.startTracingDatadog(ctx)

	case "opentelemetry-experimental":
		// TODO
		return nil, ctx, noopStopper

	case "":
		return nil, ctx, noopStopper

	default:
		b.shell.Commentf("An invalid tracing backend was provided: %q. Tracing will not occur.", b.Config.TracingBackend)
		b.Config.TracingBackend = "" // Ensure that we don't do any tracing after this, some of the stuff in tracetools uses the bootstrap's tracking backend
		return nil, ctx, noopStopper
	}
}

// startTracingDatadog sets up tracing based on the config values. It uses opentracing as an
// abstraction so the agent can support multiple libraries if needbe.
func (b *Bootstrap) startTracingDatadog(ctx context.Context) (opentracing.Span, context.Context, stopper) {
	// Newer versions of the tracing libs print out diagnostic info which spams the
	// Buildkite agent logs. Disable it by default unless it's been explicitly set.
	if _, has := os.LookupEnv("DD_TRACE_STARTUP_LOGS"); !has {
		os.Setenv("DD_TRACE_STARTUP_LOGS", "false")
	}

	label, hasLabel := b.shell.Env.Get("BUILDKITE_LABEL")
	if !hasLabel {
		label = "job"
	}

	resourceName := b.OrganizationSlug + "/" + b.PipelineSlug + "/" + label
	opts := []tracer.StartOption{
		tracer.WithServiceName("buildkite_agent"),
		tracer.WithSampler(tracer.NewAllSampler()),
		tracer.WithAnalytics(true),
		tracer.WithGlobalTag(ddext.ResourceName, resourceName),
	}

	tags := Merge(GenericTracingExtras(b, *b.shell.Env), DDTracingExtras())
	opts = slices.Grow(opts, len(tags))
	for k, v := range tags {
		opts = append(opts, tracer.WithGlobalTag(k, v))
	}

	opentracing.SetGlobalTracer(opentracer.New(opts...))

	wireContext := b.extractTraceCtx()

	span := opentracing.StartSpan("job.run", opentracing.ChildOf(wireContext))
	ctx = opentracing.ContextWithSpan(ctx, span)

	return span, ctx, tracer.Stop
}

func GenericTracingExtras(b *Bootstrap, env env.Environment) map[string]any {
	buildID, _ := env.Get("BUILDKITE_BUILD_ID")
	buildNumber, _ := env.Get("BUILDKITE_BUILD_NUMBER")
	buildURL, _ := env.Get("BUILDKITE_BUILD_URL")
	jobURL := fmt.Sprintf("%s#%s", buildURL, b.JobID)
	source, _ := env.Get("BUILDKITE_SOURCE")

	retry := 0
	if attemptStr, has := env.Get("BUILDKITE_RETRY_COUNT"); has {
		if parsedRetry, err := strconv.Atoi(attemptStr); err == nil {
			retry = parsedRetry
		}
	}

	parallel := 0
	if parallelStr, has := env.Get("BUILDKITE_PARALLEL_JOB"); has {
		if parsedParallel, err := strconv.Atoi(parallelStr); err == nil {
			parallel = parsedParallel
		}
	}

	rebuiltFromID, has := env.Get("BUILDKITE_REBUILT_FROM_BUILD_NUMBER")
	if !has || rebuiltFromID == "" {
		rebuiltFromID = "n/a"
	}

	triggeredFromID, has := env.Get("BUILDKITE_TRIGGERED_FROM_BUILD_ID")
	if !has || triggeredFromID == "" {
		triggeredFromID = "n/a"
	}

	return map[string]any{
		"buildkite.agent":             b.AgentName,
		"buildkite.version":           agent.Version(),
		"buildkite.queue":             b.Queue,
		"buildkite.org":               b.OrganizationSlug,
		"buildkite.pipeline":          b.PipelineSlug,
		"buildkite.branch":            b.Branch,
		"buildkite.job_id":            b.JobID,
		"buildkite.job_url":           jobURL,
		"buildkite.build_id":          buildID,
		"buildkite.build_number":      buildNumber,
		"buildkite.build_url":         buildURL,
		"buildkite.source":            source,
		"buildkite.retry":             strconv.Itoa(retry),
		"buildkite.parallel":          strconv.Itoa(parallel),
		"buildkite.rebuilt_from_id":   rebuiltFromID,
		"buildkite.triggered_from_id": triggeredFromID,
	}
}

func DDTracingExtras() map[string]any {
	return map[string]any{
		ddext.AnalyticsEvent:   true,
		ddext.SamplingPriority: ddext.PriorityUserKeep,
	}
}

func Merge(maps ...map[string]any) map[string]any {
	fullCap := 0
	for _, m := range maps {
		fullCap += len(m)
	}

	merged := make(map[string]any, fullCap)

	for _, m := range maps {
		for key, val := range m {
			merged[key] = val
		}
	}

	return merged
}
