package process

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FormatCommand formats a command amd arguments for human reading
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

	s := []string{command}
	for _, a := range args {
		if strings.Contains(a, "\n") || strings.Contains(a, " ") {
			s = append(s, fmt.Sprintf("%q", truncate(a, 120)))
		} else {
			s = append(s, a)
		}
	}

	return strings.Join(s, " ")
}
