package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvironmentExists(t *testing.T) {
	env := FromSlice([]string{})

	env.Set("FOO", "bar")
	env.Set("EMPTY", "")

	assert.Equal(t, env.Exists("FOO"), true)
	assert.Equal(t, env.Exists("EMPTY"), true)
	assert.Equal(t, env.Exists("does not exist"), false)
}

func TestEnvironmentSet(t *testing.T) {
	env := FromSlice([]string{})

	env.Set("    THIS_IS_THE_BEST   \n\n", "\"IT SURE IS\"\n\n")
	assert.Equal(t, env.Get("    THIS_IS_THE_BEST   \n\n"), "\"IT SURE IS\"\n\n")
}

func TestEnvironmentRemove(t *testing.T) {
	env := FromSlice([]string{"FOO=bar"})

	assert.Equal(t, env.Get("FOO"), "bar")
	assert.Equal(t, env.Remove("FOO"), "bar")
	assert.Equal(t, env.Get(""), "")
}

func TestEnvironmentMerge(t *testing.T) {
	env1 := FromSlice([]string{"FOO=bar"})
	env2 := FromSlice([]string{"BAR=foo"})

	env3 := env1.Merge(env2)

	assert.Equal(t, env3.ToSlice(), []string{"BAR=foo", "FOO=bar"})
}

func TestEnvironmentCopy(t *testing.T) {
	env1 := FromSlice([]string{"FOO=bar"})
	env2 := env1.Copy()

	assert.Equal(t, []string{"FOO=bar"}, env2.ToSlice())

	env1.Set("FOO", "not-bar-anymore")

	assert.Equal(t, []string{"FOO=bar"}, env2.ToSlice())
}

func TestEnvironmentToSlice(t *testing.T) {
	env := FromSlice([]string{"THIS_IS_GREAT=totes", "ZOMG=greatness"})

	assert.Equal(t, []string{"THIS_IS_GREAT=totes", "ZOMG=greatness"}, env.ToSlice())
}
