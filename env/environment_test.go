package env

import (
	"testing"

	"gotest.tools/assert"
	is "gotest.tools/assert/cmp"
)

func TestEnvironmentExists(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{})

	env.Set("FOO", "bar")
	env.Set("EMPTY", "")

	assert.Check(t, is.Equal(env.Exists("FOO"), true))
	assert.Check(t, is.Equal(env.Exists("EMPTY"), true))
	assert.Check(t, is.Equal(env.Exists("does not exist"), false))
}

func TestEnvironmentSet(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{})

	env.Set("    THIS_IS_THE_BEST   \n\n", "\"IT SURE IS\"\n\n")

	v, ok := env.Get("    THIS_IS_THE_BEST   \n\n")
	assert.Check(t, is.Equal(v, "\"IT SURE IS\"\n\n"))
	assert.Check(t, ok)
}

func TestEnvironmentGetBool(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{
		"LLAMAS_ENABLED=1",
		"ALPACAS_ENABLED=false",
		"PLATYPUS_ENABLED=",
		"BUNYIP_ENABLED=off",
	})

	assert.Check(t, env.GetBool(`LLAMAS_ENABLED`, false))
	assert.Check(t, !env.GetBool(`ALPACAS_ENABLED`, true))
	assert.Check(t, !env.GetBool(`PLATYPUS_ENABLED`, false))
	assert.Check(t, env.GetBool(`PLATYPUS_ENABLED`, true))
	assert.Check(t, !env.GetBool(`BUNYIP_ENABLED`, true))
}

func TestEnvironmentRemove(t *testing.T) {
	env := FromSlice([]string{"FOO=bar"})

	v, ok := env.Get("FOO")
	assert.Check(t, is.Equal(v, "bar"))
	assert.Check(t, ok)

	assert.Check(t, is.Equal(env.Remove("FOO"), "bar"))

	v, ok = env.Get("FOO")
	assert.Check(t, is.Equal(v, ""))
	assert.Check(t, !ok)
}

func TestEnvironmentMerge(t *testing.T) {
	t.Parallel()

	env1 := FromSlice([]string{"FOO=bar"})
	env2 := FromSlice([]string{"BAR=foo"})

	env3 := env1.Merge(env2)

	assert.Check(t, is.DeepEqual(env3.ToSlice(), []string{"BAR=foo", "FOO=bar"}))
}

func TestEnvironmentCopy(t *testing.T) {
	t.Parallel()

	env1 := FromSlice([]string{"FOO=bar"})
	env2 := env1.Copy()

	assert.Check(t, is.DeepEqual([]string{"FOO=bar"}, env2.ToSlice()))

	env1.Set("FOO", "not-bar-anymore")

	assert.Check(t, is.DeepEqual([]string{"FOO=bar"}, env2.ToSlice()))
}

func TestEnvironmentToSlice(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{"THIS_IS_GREAT=totes", "ZOMG=greatness"})

	assert.Check(t, is.DeepEqual([]string{"THIS_IS_GREAT=totes", "ZOMG=greatness"}, env.ToSlice()))
}
