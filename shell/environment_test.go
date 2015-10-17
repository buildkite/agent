package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvironmentSet(t *testing.T) {
	env, _ := EnvironmentFromSlice([]string{})

	env.Set("    THIS_IS_THE_BEST   \n\n", "\"IT SURE IS\"\n\n")
	assert.Equal(t, env.Get("THIS_IS_THE_BEST"), "IT SURE IS")

	env.Set("NEW_LINES_STAY_IN_SINGLE_QUOTES", "  'indeed \n it\n does\n'  ")
	assert.Equal(t, env.Get("NEW_LINES_STAY_IN_SINGLE_QUOTES"), "indeed \n it\n does\n")

	env.Set("NEW_LINES_STAY_IN_DOUBLE_QUOTES", "  \"indeed \n it\n does\n\"      ")
	assert.Equal(t, env.Get("NEW_LINES_STAY_IN_DOUBLE_QUOTES"), "indeed \n it\n does\n")

	env.Set("REMOVES_WHITESPACE_FROM_NO_QUOTES", "\n       \n  new line party\n  \n  ")
	assert.Equal(t, env.Get("REMOVES_WHITESPACE_FROM_NO_QUOTES"), "new line party")
}

func TestEnvironmentToSlice(t *testing.T) {
	env, _ := EnvironmentFromSlice([]string{"\n\nTHIS_IS_GREAT=\"this is the \n best thing\"      "})

	assert.Equal(t, env.ToSlice(), []string{"THIS_IS_GREAT=\"this is the \\n best thing\""})
}
