package tracetools

import (
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"
)

// FinishWithError is syntactic sugar for opentracing APIs to add errors to a span
// and then finishing it. If the error is nil, the span will only be finished.
func FinishWithError(span opentracing.Span, err error, fields ...log.Field) {
	if err != nil {
		ext.LogError(span, err, fields...)
	}
	span.Finish()
}
