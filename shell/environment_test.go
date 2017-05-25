package shell

import (
	"testing"

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

	env.Set("    THIS_IS_THE_BEST   \n\n", "\"IT SURE IS\"\n\n")
	assert.Equal(t, env.Get("THIS_IS_THE_BEST"), "IT SURE IS")

	env.Set("NEW_LINES_STAY_IN_SINGLE_QUOTES", "  'indeed \n it\n does\n'  ")
	assert.Equal(t, env.Get("NEW_LINES_STAY_IN_SINGLE_QUOTES"), "indeed \n it\n does\n")

	env.Set("NEW_LINES_STAY_IN_DOUBLE_QUOTES", "  \"indeed \n it\n does\n\"      ")
	assert.Equal(t, env.Get("NEW_LINES_STAY_IN_DOUBLE_QUOTES"), "indeed \n it\n does\n")

	env.Set("REMOVES_WHITESPACE_FROM_NO_QUOTES", "\n       \n  new line party\n  \n  ")
	assert.Equal(t, env.Get("REMOVES_WHITESPACE_FROM_NO_QUOTES"), "new line party")

	env.Set("DOESNT_AFFECT_QUOTES_INSIDE", `oh "hello" there`)
	assert.Equal(t, env.Get("DOESNT_AFFECT_QUOTES_INSIDE"), `oh "hello" there`)
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

func TestEnvironmentToSlice(t *testing.T) {
	env := EnvironmentFromSlice([]string{"", "", "THIS_IS_GREAT=\"this is the ", " best thing\"      "})
	assert.Equal(t, []string{"THIS_IS_GREAT=this is the \n best thing"}, env.ToSlice())

	env = EnvironmentFromSlice([]string{"THIS_IS_GREAT=first", "AND_I_HAVE=multiple", "lines"})
	assert.Equal(t, []string{"THIS_IS_GREAT=first", "AND_I_HAVE=multiple\nlines"}, env.ToSlice())

	env = EnvironmentFromSlice([]string{"THIS_IS_GREAT=first", "with a new line", "LOL=true"})
	assert.Equal(t, []string{"THIS_IS_GREAT=first\nwith a new line", "LOL=true"}, env.ToSlice())

	env = EnvironmentFromSlice([]string{"FOO=totes", "BLAH=", " NO_STARTING_WITH_SPACE=lol", "OR_A_SPACE_AFTER_THE_KEY =lollies", "1234=can't start with numbers", "_START_A_NEW_ENV=totes", "yes it's great"})
	assert.Equal(t, []string{"THIS_IS_GREAT=first\nwith a new line", "LOL=true"}, env.ToSlice())
}
