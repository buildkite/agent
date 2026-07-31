package agenthttp

import (
	"bytes"
	"encoding/json"
)

func redactTracingExporterHeadersInDump(dump []byte) []byte {
	// Fast path: no tracing_exporter in the dump.
	if !bytes.Contains(dump, []byte(`"tracing_exporter"`)) {
		return dump
	}
	if redacted, ok := redactTracingExporterInJSONBody(dump); ok {
		return redacted
	}
	// If the dump is not parseable JSON (partial dump on error), blank the
	// headers object with a conservative textual replacement.
	const needle = `"headers":`
	out := append([]byte(nil), dump...)
	searchFrom := 0
	for {
		idx := bytes.Index(out[searchFrom:], []byte(`"tracing_exporter"`))
		if idx < 0 {
			break
		}
		idx += searchFrom
		headersIdx := bytes.Index(out[idx:], []byte(needle))
		if headersIdx < 0 {
			break
		}
		headersIdx += idx + len(needle)
		// Skip whitespace
		for headersIdx < len(out) && (out[headersIdx] == ' ' || out[headersIdx] == '\t' || out[headersIdx] == '\n' || out[headersIdx] == '\r') {
			headersIdx++
		}
		if headersIdx >= len(out) || out[headersIdx] != '{' {
			searchFrom = idx + 1
			continue
		}
		end := bytes.IndexByte(out[headersIdx:], '}')
		if end < 0 {
			break
		}
		end += headersIdx
		replacement := []byte(`{"[REDACTED]":"[REDACTED]"}`)
		out = append(out[:headersIdx], append(replacement, out[end+1:]...)...)
		searchFrom = headersIdx + len(replacement)
	}
	return out
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
