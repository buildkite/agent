package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvironmentExists(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{})

	env.Set("FOO", "bar")
	env.Set("EMPTY", "")

	assert.Equal(t, env.Exists("FOO"), true)
	assert.Equal(t, env.Exists("EMPTY"), true)
	assert.Equal(t, env.Exists("does not exist"), false)
}

func TestEnvironmentSet(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{})

	env.Set("    THIS_IS_THE_BEST   \n\n", "\"IT SURE IS\"\n\n")

	v, ok := env.Get("    THIS_IS_THE_BEST   \n\n")
	assert.Equal(t, v, "\"IT SURE IS\"\n\n")
	assert.True(t, ok)
}

func TestEnvironmentGetBool(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{
		"LLAMAS_ENABLED=1",
		"ALPACAS_ENABLED=false",
		"PLATYPUS_ENABLED=",
		"BUNYIP_ENABLED=off",
	})

	assert.True(t, env.GetBool(`LLAMAS_ENABLED`, false))
	assert.False(t, env.GetBool(`ALPACAS_ENABLED`, true))
	assert.False(t, env.GetBool(`PLATYPUS_ENABLED`, false))
	assert.True(t, env.GetBool(`PLATYPUS_ENABLED`, true))
	assert.False(t, env.GetBool(`BUNYIP_ENABLED`, true))
}

func TestEnvironmentRemove(t *testing.T) {
	env := FromSlice([]string{"FOO=bar"})

	v, ok := env.Get("FOO")
	assert.Equal(t, v, "bar")
	assert.True(t, ok)

	assert.Equal(t, env.Remove("FOO"), "bar")

	v, ok = env.Get("FOO")
	assert.Equal(t, v, "")
	assert.False(t, ok)
}

func TestEnvironmentMerge(t *testing.T) {
	t.Parallel()

	env1 := FromSlice([]string{"FOO=bar"})
	env2 := FromSlice([]string{"BAR=foo"})

	env3 := env1.Merge(env2)

	assert.Equal(t, env3.ToSlice(), []string{"BAR=foo", "FOO=bar"})
}

func TestEnvironmentCopy(t *testing.T) {
	t.Parallel()

	env1 := FromSlice([]string{"FOO=bar"})
	env2 := env1.Copy()

	assert.Equal(t, []string{"FOO=bar"}, env2.ToSlice())

	env1.Set("FOO", "not-bar-anymore")

	assert.Equal(t, []string{"FOO=bar"}, env2.ToSlice())
}

func TestEnvironmentToSlice(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{"THIS_IS_GREAT=totes", "ZOMG=greatness"})

	assert.Equal(t, []string{"THIS_IS_GREAT=totes", "ZOMG=greatness"}, env.ToSlice())
}
