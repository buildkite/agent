package agenthttp

import (
	"strings"
	"testing"
)

func TestRedactTracingExporterHeadersInDump(t *testing.T) {
	t.Parallel()

	dump := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" +
		`{"id":"job-1","tracing_exporter":{"endpoint":"https://otel.example/v1/traces","protocol":"http/protobuf","headers":{"Authorization":"Bearer secret-token"}}}`)

	got := string(redactTracingExporterHeadersInDump(dump))
	if strings.Contains(got, "Bearer secret-token") {
		t.Fatalf("expected Authorization value redacted, got:\n%s", got)
	}
	if !strings.Contains(got, `"endpoint":"https://otel.example/v1/traces"`) {
		t.Fatalf("expected endpoint preserved, got:\n%s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redacted marker, got:\n%s", got)
	}
}

func TestRedactTracingExporterHeadersInDump_NoExporter(t *testing.T) {
	t.Parallel()

	dump := []byte("HTTP/1.1 200 OK\r\n\r\n{\"id\":\"job-1\"}")
	got := redactTracingExporterHeadersInDump(dump)
	if string(got) != string(dump) {
		t.Fatalf("expected unchanged dump without tracing_exporter")
	}
}
