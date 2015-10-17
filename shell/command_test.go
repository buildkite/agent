package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandFromString(t *testing.T) {
	var cmd *Command

	cmd, _ = CommandFromString("ls")
	assert.Equal(t, cmd.Command, "ls")
	assert.Equal(t, cmd.Args, []string{})
	assert.Equal(t, cmd.Dir, "")

	cmd, _ = CommandFromString("/this/script/here.sh")
	assert.Equal(t, cmd.Command, "here.sh")
	assert.Equal(t, cmd.Args, []string{})
	assert.Equal(t, cmd.Dir, "/this/script")

	cmd, _ = CommandFromString("test.sh")
	assert.Equal(t, cmd.Command, "test.sh")
	assert.Equal(t, cmd.Args, []string{})
	assert.Equal(t, cmd.Dir, "")

	cmd, _ = CommandFromString("./foo $FOO")
	assert.Equal(t, cmd.Command, "foo")
	assert.Equal(t, cmd.Args, []string{"$FOO"})
	assert.Equal(t, cmd.Dir, "")

	cmd, _ = CommandFromString("  command        with-sub-command   ")
	assert.Equal(t, cmd.Command, "command")
	assert.Equal(t, cmd.Args, []string{"with-sub-command"})
	assert.Equal(t, cmd.Dir, "")

	cmd, _ = CommandFromString(`/bin/bash -c "execute this \"script\""`)
	assert.Equal(t, cmd.Command, "bash")
	assert.Equal(t, cmd.Dir, "/bin")
	assert.Equal(t, cmd.Args, []string{"-c", `execute this "script"`})

	cmd, _ = CommandFromString(`git clone -qb -b "branch-name" --single-branch -- 'git@github.com/foo.git' .`)
	assert.Equal(t, cmd.Command, "git")
	assert.Equal(t, cmd.Args, []string{"clone", "-qb", "-b", "branch-name", "--single-branch", "--", "git@github.com/foo.git", "."})
	assert.Equal(t, cmd.Dir, "")
}
