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

func TestEnvironmentDiff(t *testing.T) {
	t.Parallel()
	a := FromSlice([]string{"A=hello", "B=world"})
	b := FromSlice([]string{"A=hello", "B=there", "C=new", "D="})

	ab := a.Diff(b)
	assert.Equal(t, Diff {
		Added: map[string]string{},
		Changed: map[string]Pair{
			"B": Pair {
				Old: "there",
				New: "world",
			},
		},
		Removed: map[string]struct{} {
			"C": struct{}{},
			"D": struct{}{},
		},
	}, ab)

	ba := b.Diff(a)
	assert.Equal(t, Diff {
		Added: map[string]string{
			"C": "new",
			"D": "",
		},
		Changed: map[string]Pair {
			"B": Pair {
				Old: "world",
				New: "there",
			},
		},
		Removed: map[string]struct{}{},
	}, ba)
}

func TestEnvironmentDiffRemove(t *testing.T) {
	t.Parallel()

	diff := Diff {
		Added: map[string]string {
			"A": "new",
		},
		Changed: map[string]Pair {
			"B": Pair {
				Old: "old",
				New: "new",
			},
		},
		Removed: map[string]struct{} {
			"C": struct{}{},
		},
	}

	diff.Remove("A")
	diff.Remove("B")
	diff.Remove("C")

	assert.Equal(t, Diff {
		Added: map[string]string {},
		Changed: map[string]Pair {},
		Removed: map[string]struct{}{},
	}, diff)
}

func TestEmptyDiff(t *testing.T) {
	t.Parallel()

	empty := Diff{}

	assert.Equal(t, true, empty.Empty())
}

func TestEnvironmentApply(t *testing.T) {
	t.Parallel()

	env := &Environment{}
	env = env.Apply(Diff {
		Added: map[string]string{
			"LLAMAS_ENABLED": "1",
		},
		Changed: map[string]Pair{},
		Removed: map[string]struct{}{},
	})
	assert.Equal(t, FromSlice([]string{
		"LLAMAS_ENABLED=1",
	}), env)

	env = env.Apply(Diff {
		Added: map[string]string{
			"ALPACAS_ENABLED": "1",
		},
		Changed: map[string]Pair{
			"LLAMAS_ENABLED": Pair {
				Old: "1",
				New: "0",
			},
		},
		Removed: map[string]struct{}{},
	})
	assert.Equal(t, FromSlice([]string{
		"ALPACAS_ENABLED=1",
		"LLAMAS_ENABLED=0",
	}), env)

	env = env.Apply(Diff {
		Added: map[string]string{},
		Changed: map[string]Pair{},
		Removed: map[string]struct{} {
			"LLAMAS_ENABLED": struct{}{},
			"ALPACAS_ENABLED": struct{}{},
		},
	})
	assert.Equal(t, FromSlice([]string{}), env)
}
