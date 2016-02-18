package shell

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/mattn/go-shellwords"
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

// Creates a command by parsing a string like `ls -lsa`
func CommandFromString(str string) (*Command, error) {
	args, err := shellwords.Parse(str)
	if err != nil {
		return nil, err
	}

	return &Command{Command: args[0], Args: args[1:]}, nil
}

var envPathLock sync.Mutex

// The absolute path to this commands executable
func (c *Command) AbsolutePath() (string, error) {
	// Is the path already absolute?
	if path.IsAbs(c.Command) {
		return c.Command, nil
	}

	var absolutePath string
	var err error

	if c.Env != nil && c.Env.Get("PATH") != "" {
		// This is a little hacky and ugly. Golangs `LookPath` will
		// look in a PATH env for an executable, which is exactly what
		// we want, however it only looks in the current env's PATH,
		// and it can't be customized.
		//
		// Since we can't change it, we'll just hack override the
		// current PATH temporarly as we figure it out. We have to wrap
		// it in a lock so other processes don't try and change the
		// PATH at the same time.
		envPathLock.Lock()
		defer envPathLock.Unlock()

		// Change the PATH
		previousEnvPath := os.Getenv("PATH")
		os.Setenv("PATH", c.Env.Get("PATH"))

		// Now we can look up the path
		absolutePath, err = exec.LookPath(c.Command)

		// Restore the previous PATH
		os.Setenv("PATH", previousEnvPath)
	} else {
		// Since no custom PATH is set, we can just default to the
		// regular behaviour.
		absolutePath, err = exec.LookPath(c.Command)
	}

	if err != nil {
		return "", err
	} else {
		// Since the path returned by LookPath is relative to the
		// current working directory, we need to get the absolute
		// version of that.
		return filepath.Abs(absolutePath)
	}
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
