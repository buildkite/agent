package shell

import "strings"

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
	return strings.Join(append([]string{c.Command}, c.Args...), " ")
}
