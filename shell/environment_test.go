package shell

import (
	"testing"
	"os"

	"github.com/stretchr/testify/assert"
)

func TestEnvironmentExists(t *testing.T) {
	env := EnvironmentFromSlice([]string{})

	env.Set("FOO", "bar")
	env.Set("EMPTY", "")

	assert.Equal(t, env.Exists("FOO"), true)
	assert.Equal(t, env.Exists("EMPTY"), true)
	assert.Equal(t, env.Exists("does not exist"), false)
}

func TestEnvironmentSet(t *testing.T) {
	env := EnvironmentFromSlice([]string{})

	env.Set("FOO_BAR", `quotes " and new lines \n omg`)
	assert.Equal(t, env.Get("FOO_BAR"), `quotes " and new lines \n omg`)
}

func TestEnvironmentRemove(t *testing.T) {
	env := EnvironmentFromSlice([]string{"FOO=bar"})

	assert.Equal(t, env.Get("FOO"), "bar")
	assert.Equal(t, env.Remove("FOO"), "bar")
	assert.Equal(t, env.Get(""), "")
}

func TestEnvironmentMerge(t *testing.T) {
	env1 := EnvironmentFromSlice([]string{"FOO=bar"})
	env2 := EnvironmentFromSlice([]string{"BAR=foo"})

	env3 := env1.Merge(env2)

	assert.Equal(t, env3.ToSlice(), []string{"BAR=foo", "FOO=bar"})
}

func TestEnvironmentCopy(t *testing.T) {
	env1 := EnvironmentFromSlice([]string{"FOO=bar"})
	env2 := env1.Copy()

	assert.Equal(t, env2.ToSlice(), []string{"FOO=bar"})

	env1.Set("FOO", "not-bar-anymore")

	assert.Equal(t, env2.ToSlice(), []string{"FOO=bar"})
}

func TestEnvironmentFromString(t *testing.T) {
	env := EnvironmentFromString(`FOO=bar\nBANG=buck\nNEW_LINE=\nsupported\nand\nstuff`)
	assert.Equal(t, []string{"BANG=buck", "FOO=bar", `NEW_LINE=\nsupported\nand\nstuff`}, env.ToSlice())

	env = EnvironmentFromString(`\n\nTHIS_IS_GREAT="this is the \n best thing"`)
	assert.Equal(t, `"this is the \n best thing"`, env.Get("THIS_IS_GREAT"))
	assert.Equal(t, []string{`THIS_IS_GREAT="this is the \n best thing"`}, env.ToSlice())

	env = EnvironmentFromString(`THIS_IS_GREAT=first\nAND_I_HAVE=multiple\nlines`)
	assert.Equal(t, []string{`AND_I_HAVE=multiple\nlines`, `THIS_IS_GREAT=first`}, env.ToSlice())

	env = EnvironmentFromString(`THIS_IS_GREAT=first\nwith a new line\nLOL=true`)
	assert.Equal(t, []string{"LOL=true", `THIS_IS_GREAT=first\nwith a new line`}, env.ToSlice())

	env = EnvironmentFromString(`FOO=totes\nBLAH=\n NO_STARTING_WITH_SPACE=lol\nOR_A_SPACE_AFTER_THE_KEY =lollies\n1234=can't start with numbers\n_START_A_NEW_ENV=totes\nyes it's great\nNEW_ONE=true`)
	assert.Equal(t, []string{`BLAH=\n NO_STARTING_WITH_SPACE=lol\nOR_A_SPACE_AFTER_THE_KEY =lollies\n1234=can't start with numbers\n_START_A_NEW_ENV=totes\nyes it's great`, `FOO=totes`, `NEW_ONE=true`}, env.ToSlice())

	env = EnvironmentFromString(`FOO=bar\0BANG=buck\0NEW_LINE=\nsupported\nand\nstuff\0`)
	assert.Equal(t, []string{"BANG=buck", "FOO=bar", `NEW_LINE=\nsupported\nand\nstuff`}, env.ToSlice())
}

func TestEnvironmentToSlice(t *testing.T) {
	env := EnvironmentFromSlice([]string{"THIS_IS_GREAT=first", "AND_I_HAVE=another"})
	assert.Equal(t, []string{"AND_I_HAVE=another", "THIS_IS_GREAT=first"}, env.ToSlice())

	env = EnvironmentFromSlice(os.Environ())
	assert.Equal(t, env.Get("USER"), os.Getenv("USER"))
}
