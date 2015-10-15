package shell

import "io"

type Config struct {
	// Where the STDOUR + STDERR of a command will be written to
	Writer io.Writer

	// Whether or not the command should be run in a PTY
	PTY bool
}
