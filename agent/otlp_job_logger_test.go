package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/api"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type otlpJobLogTestExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *otlpJobLogTestExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, record := range records {
		e.records = append(e.records, record.Clone())
	}
	return nil
}

func (e *otlpJobLogTestExporter) Shutdown(context.Context) error {
	return nil
}

func (e *otlpJobLogTestExporter) ForceFlush(context.Context) error {
	return nil
}

func (e *otlpJobLogTestExporter) Records() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	records := make([]sdklog.Record, len(e.records))
	copy(records, e.records)
	return records
}

func TestOTLPJobLoggerEmitsStructuredLineRecords(t *testing.T) {
	t.Parallel()

	exporter := &otlpJobLogTestExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	conf := JobRunnerConfig{
		Job: &api.Job{
			ID: "job-123",
			Env: map[string]string{
				"BUILDKITE_ORGANIZATION_SLUG":     "my-org",
				"BUILDKITE_PIPELINE_SLUG":         "my-pipeline",
				"BUILDKITE_BRANCH":                "main",
				"BUILDKITE_AGENT_META_DATA_QUEUE": "default",
				"BUILDKITE_AGENT_NAME":            "agent-1",
				"BUILDKITE_AGENT_ID":              "agent-id-1",
				"BUILDKITE_BUILD_ID":              "build-456",
				"BUILDKITE_BUILD_NUMBER":          "42",
				"BUILDKITE_BUILD_URL":             "https://buildkite.com/my-org/my-pipeline/builds/42",
				"BUILDKITE_LABEL":                 "Test",
				"BUILDKITE_STEP_KEY":              "test",
			},
			TraceParent: "00-abcdef1234567890abcdef1234567890-1234567890abcdef-01",
		},
	}
	logger := newOTLPJobLoggerWithLogger(
		contextWithJobTraceparent(context.Background(), conf.Job.TraceParent, conf.Job.TraceState),
		provider.Logger("test"),
		provider,
		otlpJobLogAttributes(conf),
	)

	if _, err := logger.Write([]byte("first line\nsecond line\n")); err != nil {
		t.Fatalf("logger.Write() = %v", err)
	}

	records := exporter.Records()
	if got, want := len(records), 2; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}
	if got, want := records[0].Body().AsString(), "first line"; got != want {
		t.Errorf("record body = %q, want %q", got, want)
	}
	if records[0].Timestamp().IsZero() {
		t.Errorf("record timestamp is zero")
	}
	if records[0].ObservedTimestamp().IsZero() {
		t.Errorf("record observed timestamp is zero")
	}
	if got, want := records[0].Severity(), log.SeverityInfo; got != want {
		t.Errorf("record severity = %v, want %v", got, want)
	}
	if got, want := records[0].TraceID().String(), "abcdef1234567890abcdef1234567890"; got != want {
		t.Errorf("record trace ID = %q, want %q", got, want)
	}
	if got, want := records[0].SpanID().String(), "1234567890abcdef"; got != want {
		t.Errorf("record span ID = %q, want %q", got, want)
	}

	attrs := recordAttributes(records[0])
	for key, want := range map[string]string{
		"source":                      "job",
		"buildkite.organization.slug": "my-org",
		"buildkite.pipeline.slug":     "my-pipeline",
		"buildkite.branch":            "main",
		"buildkite.queue":             "default",
		"buildkite.agent":             "agent-1",
		"buildkite.agent.id":          "agent-id-1",
		"buildkite.build.id":          "build-456",
		"buildkite.build.number":      "42",
		"buildkite.job.id":            "job-123",
		"buildkite.job.label":         "Test",
		"buildkite.job.key":           "test",
	} {
		if got := attrs[key]; got != want {
			t.Errorf("attribute %s = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"buildkite.build.url", "buildkite.job.url", "trace_id", "span_id"} {
		if got := attrs[key]; got != "" {
			t.Errorf("attribute %s = %q, want empty", key, got)
		}
	}
}

func TestOTLPJobLoggerFlushesPartialLineOnClose(t *testing.T) {
	t.Parallel()

	exporter := &otlpJobLogTestExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	logger := newOTLPJobLoggerWithLogger(context.Background(), provider.Logger("test"), provider, nil)

	if _, err := logger.Write([]byte("partial")); err != nil {
		t.Fatalf("logger.Write() = %v", err)
	}
	if got := len(exporter.Records()); got != 0 {
		t.Fatalf("record count before close = %d, want 0", got)
	}
	if err := logger.Close(context.Background()); err != nil {
		t.Fatalf("logger.Close() = %v", err)
	}
	records := exporter.Records()
	if got, want := len(records), 1; got != want {
		t.Fatalf("record count after close = %d, want %d", got, want)
	}
	if got, want := records[0].Body().AsString(), "partial"; got != want {
		t.Errorf("record body = %q, want %q", got, want)
	}
}

func TestOTLPJobLoggerPreservesBodyWithoutTimestampPrefix(t *testing.T) {
	t.Parallel()

	exporter := &otlpJobLogTestExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	logger := newOTLPJobLoggerWithLogger(context.Background(), provider.Logger("test"), provider, nil)

	before := time.Now()
	if _, err := logger.Write([]byte("hello from the command\n")); err != nil {
		t.Fatalf("logger.Write() = %v", err)
	}
	records := exporter.Records()
	if got, want := records[0].Body().AsString(), "hello from the command"; got != want {
		t.Errorf("record body = %q, want %q", got, want)
	}
	if records[0].Timestamp().Before(before) {
		t.Errorf("record timestamp = %v, want after %v", records[0].Timestamp(), before)
	}
}

func TestOTLPJobLoggerAddsCurrentHookScopeFromHeaders(t *testing.T) {
	t.Parallel()

	exporter := &otlpJobLogTestExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	logger := newOTLPJobLoggerWithLogger(context.Background(), provider.Logger("test"), provider, nil)

	if _, err := logger.Write([]byte("~~~ Running repository post-command hook\nhook output\n~~~ Running commands\ncommand output\n")); err != nil {
		t.Fatalf("logger.Write() = %v", err)
	}

	records := exporter.Records()
	if got, want := len(records), 4; got != want {
		t.Fatalf("record count = %d, want %d", got, want)
	}

	hookAttrs := recordAttributes(records[1])
	for key, want := range map[string]string{
		"buildkite.phase":      "hook",
		"buildkite.hook.name":  "post-command",
		"buildkite.hook.scope": "repository",
	} {
		if got := hookAttrs[key]; got != want {
			t.Errorf("hook output attribute %s = %q, want %q", key, got, want)
		}
	}

	commandAttrs := recordAttributes(records[3])
	if got, want := commandAttrs["buildkite.phase"], "command"; got != want {
		t.Errorf("command output buildkite.phase = %q, want %q", got, want)
	}
	if got := commandAttrs["buildkite.hook.name"]; got != "" {
		t.Errorf("command output buildkite.hook.name = %q, want empty", got)
	}
}

func TestOTLPJobLoggerAddsPluginHookScopeFromHeaders(t *testing.T) {
	t.Parallel()

	exporter := &otlpJobLogTestExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	logger := newOTLPJobLoggerWithLogger(context.Background(), provider.Logger("test"), provider, nil)

	if _, err := logger.Write([]byte("~~~ Running plugin docker#v5.12.0 pre-command hook\nplugin output\n")); err != nil {
		t.Fatalf("logger.Write() = %v", err)
	}

	records := exporter.Records()
	attrs := recordAttributes(records[1])
	for key, want := range map[string]string{
		"buildkite.phase":       "hook",
		"buildkite.hook.name":   "pre-command",
		"buildkite.hook.scope":  "plugin",
		"buildkite.hook.plugin": "docker#v5.12.0",
	} {
		if got := attrs[key]; got != want {
			t.Errorf("plugin hook output attribute %s = %q, want %q", key, got, want)
		}
	}
}

func recordAttributes(record sdklog.Record) map[string]string {
	attrs := map[string]string{}
	record.WalkAttributes(func(kv log.KeyValue) bool {
		attrs[kv.Key] = kv.Value.AsString()
		return true
	})
	return attrs
}
