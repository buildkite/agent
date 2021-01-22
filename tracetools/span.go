package tracetools

import (
	"context"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
	ddext "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/ext"
)

// StartSpanFromContext will start a span from the given context with the given
// operation name. It will also do some common/repeated setup on the span to keep
// code a little more DRY.
func StartSpanFromContext(ctx context.Context, operation string) (opentracing.Span, context.Context) {
	span, ctx := opentracing.StartSpanFromContext(ctx, operation)
	// Make the span available for analytics in Datadog
	span.SetTag(ddext.AnalyticsEvent, true)
	return span, ctx
}

// FinishWithError is syntactic sugar for opentracing APIs to add errors to a span
// and then finishing it. If the error is nil, the span will only be finished.
func FinishWithError(span opentracing.Span, err error, fields ...log.Field) {
	if err != nil {
		ext.LogError(span, err, fields...)
	}
	span.Finish()
}
