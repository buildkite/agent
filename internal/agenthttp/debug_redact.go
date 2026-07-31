package agenthttp

import (
	"bytes"
	"encoding/json"
	"regexp"
)

// tracingExporterHeadersRE matches JSON "headers" objects nested under
// tracing_exporter in debug HTTP dumps. Values are redacted so vendor tokens
// are not written to agent logs when --debug-http is enabled.
var tracingExporterHeadersRE = regexp.MustCompile(
	`("tracing_exporter"\s*:\s*\{[^}]*"headers"\s*:\s*)\{[^}]*\}`,
)

func redactTracingExporterHeadersInDump(dump []byte) []byte {
	// Fast path: no tracing_exporter in the dump.
	if !bytes.Contains(dump, []byte(`"tracing_exporter"`)) {
		return dump
	}

	// Prefer structured redaction when the dump body is JSON (response body
	// after headers). Fall back to a conservative regex on the whole dump.
	if redacted, ok := redactTracingExporterInJSONBody(dump); ok {
		return redacted
	}
	return tracingExporterHeadersRE.ReplaceAll(dump, []byte(`${1}{"[REDACTED]":"[REDACTED]"}`))
}

func redactTracingExporterInJSONBody(dump []byte) ([]byte, bool) {
	// httputil.DumpResponse format: status line, headers, blank line, body.
	idx := bytes.Index(dump, []byte("\r\n\r\n"))
	if idx < 0 {
		idx = bytes.Index(dump, []byte("\n\n"))
		if idx < 0 {
			return nil, false
		}
		idx += 2
	} else {
		idx += 4
	}
	body := dump[idx:]
	if len(bytes.TrimSpace(body)) == 0 || body[0] != '{' {
		return nil, false
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	rawExporter, ok := payload["tracing_exporter"]
	if !ok {
		return nil, false
	}

	var exporter map[string]any
	if err := json.Unmarshal(rawExporter, &exporter); err != nil {
		return nil, false
	}
	if headers, ok := exporter["headers"].(map[string]any); ok {
		for k := range headers {
			headers[k] = "[REDACTED]"
		}
		exporter["headers"] = headers
	}
	redactedExporter, err := json.Marshal(exporter)
	if err != nil {
		return nil, false
	}
	payload["tracing_exporter"] = redactedExporter
	newBody, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	out := append([]byte(nil), dump[:idx]...)
	out = append(out, newBody...)
	return out, true
}
