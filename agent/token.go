package agent

import (
	"errors"
	"os"
	"os/exec"

	shellwords "github.com/mattn/go-shellwords"
)

var errEmptyToken = errors.New("Invalid token, empty string supplied")

type TokenReader interface {
	Read() (string, error)
}

// stringToken provides a token that is simply a string
type StringToken string

func (s StringToken) Read() (string, error) {
	str := string(s)
	if str == "" {
		return str, errEmptyToken
	}
	return str, nil
}

// scriptToken delegates to the output of a script to return a token
type ScriptToken struct {
	name string
	args []string
}

// Creates a scriptToken by parsing a string like `ls -lsa`
func ScriptTokenFromString(str string) (*ScriptToken, error) {
	args, err := shellwords.Parse(str)
	if err != nil {
		return nil, err
	}

	return &ScriptToken{name: args[0], args: args[1:]}, nil
}

func (s ScriptToken) Read() (string, error) {
	cmd := exec.Command(s.name, s.args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return string(out), nil
}
