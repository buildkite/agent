package process

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FormatCommand formats a command amd arguments for human reading
func FormatCommand(command string, args []interface{}) string {
	var truncate = func(s string, i int) string {
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
		sv := fmt.Sprintf("%s", a)

		if strings.Contains(sv, "\n") || strings.Contains(sv, " ") {
			aa := strings.Replace(strings.Replace(sv, "\n", "", -1), "\"", "\\", -1)
			s = append(s, "\""+truncate(aa, 40)+"\"")
		} else {
			s = append(s, sv)
		}
	}

	return strings.Join(s, " ")
}
