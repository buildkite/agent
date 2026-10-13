package process

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/buildkite/agent/v4/internal/redact"
)

// FormatCommand formats a command amd arguments for human reading. Credentials
// embedded in URLs (e.g. https://user:token@host/repo.git) are masked.
func FormatCommand(command string, args []string) string {
	truncate := func(s string, i int) string {
		if len(s) < i {
			return s
		}

		if utf8.ValidString(s[:i]) {
			return s[:i] + "..."
		}

		return s[:i+1] + "..." // or i-1
	}

	s := []string{redact.URLCredentialsInText(command)}
	for _, a := range args {
		// Mask before truncating, so truncation can't hide the end of the
		// userinfo from the mask.
		a = redact.URLCredentialsInText(a)
		if strings.Contains(a, "\n") || strings.Contains(a, " ") {
			s = append(s, fmt.Sprintf("%q", truncate(a, 120)))
		} else {
			s = append(s, a)
		}
	}

	return strings.Join(s, " ")
}
