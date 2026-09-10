package dockerbootstrap

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/buildkite/agent/v4/internal/redact"
)

const diagnosticLimit = 16 * 1024

// Docker stdout may contain sensitive inspection data. Capture only stderr,
// redacting before truncation to avoid leaking partial secrets.
type diagnosticClient struct {
	Clienter
	needles []string
}

func withDiagnostics(client Clienter, environment []string, secrets ...string) Clienter {
	patterns := []string{"*_TOKEN", "*_PASSWORD", "*_SECRET", "*_KEY", "*HEADERS*", "BUILDKITE_SECRETS_CONFIG"}
	for _, pair := range environment {
		if value, ok := strings.CutPrefix(pair, "BUILDKITE_REDACTED_VARS="); ok {
			patterns = append(patterns, strings.Split(value, ",")...)
		}
	}
	needles := append([]string(nil), secrets...)
	for _, pair := range environment {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || value == "" {
			continue
		}
		matched, err := redact.MatchAny(patterns, name)
		// Invalid patterns must not leave potential secrets unmasked.
		if matched || err != nil {
			needles = append(needles, value)
		}
	}
	return diagnosticClient{Clienter: client, needles: needles}
}

func (c diagnosticClient) Run(ctx context.Context, args []string, env map[string]string, stdout, stderr io.Writer) (int, error) {
	// JobRunner handles attached logs; a nonzero job exit is not a CLI error.
	if args[0] == "start" {
		return c.Clienter.Run(ctx, args, env, stdout, stderr)
	}
	var capture diagnosticBuffer
	filtered := redact.New(&capture, c.needles)
	code, cause := c.Clienter.Run(ctx, args, env, stdout, filtered)
	_ = filtered.Flush() // diagnosticBuffer.Write cannot fail.
	if cause == nil && code == 0 {
		return code, nil
	}
	action := args[0]
	if (action == "image" || action == "network") && len(args) > 1 {
		action += " " + args[1]
	}
	message := fmt.Sprintf("docker %s failed (exit %d)", action, code)
	if cause != nil {
		message = fmt.Sprintf("docker %s failed: %s", action, sanitizeDiagnostic(redact.String(cause.Error(), c.needles)))
	}
	if detail := sanitizeDiagnostic(capture.String()); detail != "" {
		message += ": " + detail
	}
	return code, &diagnosticError{message: message, cause: cause}
}

type diagnosticError struct {
	message string
	cause   error
}

func (e *diagnosticError) Error() string { return e.message }
func (e *diagnosticError) Unwrap() error { return e.cause }

type diagnosticBuffer struct {
	data      []byte
	truncated bool
}

func (b *diagnosticBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := diagnosticLimit - len(b.data)
	if len(p) > remaining {
		p = p[:remaining]
		b.truncated = true
	}
	b.data = append(b.data, p...)
	return n, nil
}

func (b *diagnosticBuffer) String() string {
	s := string(b.data)
	if b.truncated {
		// Truncated credentials may be unrecognizable to the sanitizer.
		if end := strings.LastIndexByte(s, '\n'); end >= 0 {
			s = s[:end]
		} else {
			s = ""
		}
		s += "\n[Docker diagnostic truncated]"
	}
	return strings.TrimSpace(s)
}

var (
	diagnosticURLUser = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/]*@`)
	diagnosticAuth    = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[^\s"']+`)
)

func sanitizeDiagnostic(s string) string {
	s = diagnosticURLUser.ReplaceAllString(s, "${1}[REDACTED]@")
	s = diagnosticAuth.ReplaceAllString(s, "${1} [REDACTED]")
	return strings.TrimSpace(s)
}
