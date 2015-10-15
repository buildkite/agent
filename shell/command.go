package shell

import (
	"strings"
	"unicode/utf8"
)

type Command struct {
	// The command for process
	Command string

	// The arguments that need to be passed to the command
	Args []string

	// The environment to use for the command
	Env *Environment

	// The directory to run the command from
	Dir string
}

func (c *Command) String() string {
	s := []string{c.Command}
	for _, a := range c.Args {
		if strings.Contains(a, "\n") || strings.Contains(a, " ") {
			aa := strings.Replace(strings.Replace(a, "\n", "", -1), "\"", "\\", -1)
			s = append(s, "\""+truncate(aa, 40)+"\"")
		} else {
			s = append(s, a)
		}
	}

	return strings.Join(s, " ")
}

func truncate(s string, i int) string {
	if len(s) < i {
		return s
	}

	if utf8.ValidString(s[:i]) {
		return s[:i] + "..."
	}

	return s[:i+1] + "..." // or i-1
}
