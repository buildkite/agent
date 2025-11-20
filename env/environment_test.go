package env

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvironmentExists(t *testing.T) {
	t.Parallel()

	env := New()

	env.Set("FOO", "bar")
	env.Set("EMPTY", "")

	assert.Equal(t, env.Exists("FOO"), true)
	assert.Equal(t, env.Exists("EMPTY"), true)
	assert.Equal(t, env.Exists("does not exist"), false)
}

func TestEnvironmentSet(t *testing.T) {
	t.Parallel()

	env := New()

	env.Set("    THIS_IS_THE_BEST   \n\n", "\"IT SURE IS\"\n\n")

	v, ok := env.Get("    THIS_IS_THE_BEST   \n\n")
	assert.Equal(t, v, "\"IT SURE IS\"\n\n")
	assert.True(t, ok)
}

func TestEnvironmentSet_NormalizesKeyNames(t *testing.T) {
	t.Parallel()
	e := New()

	mountain := "Mountain"
	e.Set(mountain, "Cerro Torre")

	switch runtime.GOOS {
	case "windows":
		// All keys are treated as being in the same case so long as they have the same letters
		// (i.e. "Mountain", "mountain" and "MOUNTAIN" are treated the same key)
		assert.True(t, e.Exists(mountain))
		assert.True(t, e.Exists(strings.ToUpper(mountain)))

		v, _ := e.Get(strings.ToUpper(mountain))
		assert.Equal(t, v, "Cerro Torre")

		e.Set(strings.ToUpper(mountain), "Cerro Poincenot")

		v, _ = e.Get(mountain)
		assert.Equal(t, v, "Cerro Poincenot")

		v, _ = e.Get(strings.ToUpper(mountain))
		assert.Equal(t, v, "Cerro Poincenot")

	default:
		// Two keys with the same letters but different cases can coexist
		// (i.e. "Mountain", "mountain", "MOUNTAIN" are treated as three different keys)
		assert.True(t, e.Exists(mountain))
		assert.False(t, e.Exists(strings.ToUpper(mountain)))

		e.Set(strings.ToUpper(mountain), "Cerro Poincenot")

		camel, _ := e.Get(mountain)
		assert.Equal(t, camel, "Cerro Torre")

		upper, _ := e.Get(strings.ToUpper(mountain))
		assert.Equal(t, upper, "Cerro Poincenot")
	}
}

func TestEnvironmentGetBool(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{
		"LLAMAS_ENABLED=1",
		"ALPACAS_ENABLED=false",
		"PLATYPUS_ENABLED=",
		"BUNYIP_ENABLED=off",
	})

	assert.True(t, env.GetBool("LLAMAS_ENABLED", false))
	assert.False(t, env.GetBool("ALPACAS_ENABLED", true))
	assert.False(t, env.GetBool("PLATYPUS_ENABLED", false))
	assert.True(t, env.GetBool("PLATYPUS_ENABLED", true))
	assert.False(t, env.GetBool("BUNYIP_ENABLED", true))
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

	env1.Merge(env2)

	assert.Equal(t, env1.ToSlice(), []string{"BAR=foo", "FOO=bar"})
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
	assert.Equal(t, Diff{
		Added: map[string]string{},
		Changed: map[string]DiffPair{
			"B": {
				Old: "there",
				New: "world",
			},
		},
		Removed: map[string]struct{}{
			"C": {},
			"D": {},
		},
	}, ab)

	ba := b.Diff(a)
	assert.Equal(t, Diff{
		Added: map[string]string{
			"C": "new",
			"D": "",
		},
		Changed: map[string]DiffPair{
			"B": {
				Old: "world",
				New: "there",
			},
		},
		Removed: map[string]struct{}{},
	}, ba)
}

func TestEnvironmentDiffRemove(t *testing.T) {
	t.Parallel()

	diff := Diff{
		Added: map[string]string{
			"A": "new",
		},
		Changed: map[string]DiffPair{
			"B": {
				Old: "old",
				New: "new",
			},
		},
		Removed: map[string]struct{}{
			"C": {},
		},
	}

	diff.Remove("A")
	diff.Remove("B")
	diff.Remove("C")

	assert.Equal(t, Diff{
		Added:   map[string]string{},
		Changed: map[string]DiffPair{},
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

	env := New()
	env.Apply(Diff{
		Added: map[string]string{
			"LLAMAS_ENABLED": "1",
		},
		Changed: map[string]DiffPair{},
		Removed: map[string]struct{}{},
	})
	assert.Equal(t, FromSlice([]string{
		"LLAMAS_ENABLED=1",
	}).Dump(), env.Dump())

	env.Apply(Diff{
		Added: map[string]string{
			"ALPACAS_ENABLED": "1",
		},
		Changed: map[string]DiffPair{
			"LLAMAS_ENABLED": {
				Old: "1",
				New: "0",
			},
		},
		Removed: map[string]struct{}{},
	})
	assert.Equal(t, FromSlice([]string{
		"ALPACAS_ENABLED=1",
		"LLAMAS_ENABLED=0",
	}).Dump(), env.Dump())

	env.Apply(Diff{
		Added:   map[string]string{},
		Changed: map[string]DiffPair{},
		Removed: map[string]struct{}{
			"LLAMAS_ENABLED":  {},
			"ALPACAS_ENABLED": {},
		},
	})
	assert.Equal(t, FromSlice([]string{}).Dump(), env.Dump())
}

func TestSplit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in          string
		name, value string
		ok          bool
	}{
		{"key=value", "key", "value", true},
		{"equalsign==", "equalsign", "=", true},
		{"=Windows=Nonsense", "", "", false},
		{"=Bonus=Windows=Nonsense", "", "", false},
		{"no_value=", "no_value", "", true},
		{"NotValid", "", "", false},
		{"=AlsoInvalid", "", "", false},
	}

	for _, test := range tests {
		gotName, gotValue, gotOK := Split(test.in)
		if gotName != test.name || gotValue != test.value || gotOK != test.ok {
			t.Errorf("Split(%q) = (%q, %q, %t), want (%q, %q, %t)", test.in, gotName, gotValue, gotOK, test.name, test.value, test.ok)
		}
	}
}

func TestProtectedEnv(t *testing.T) {
	// Test that ProtectedEnv contains the expected variables
	expectedProtected := []string{
		"BUILDKITE_AGENT_ACCESS_TOKEN",
		"BUILDKITE_AGENT_DEBUG",
		"BUILDKITE_AGENT_ENDPOINT",
		"BUILDKITE_AGENT_PID",
		"BUILDKITE_BIN_PATH",
		"BUILDKITE_BUILD_PATH",
		"BUILDKITE_COMMAND_EVAL",
		"BUILDKITE_CONFIG_PATH",
		"BUILDKITE_CONTAINER_COUNT",
		"BUILDKITE_GIT_CLEAN_FLAGS",
		"BUILDKITE_GIT_CLONE_FLAGS",
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
		"BUILDKITE_GIT_FETCH_FLAGS",
		"BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
		"BUILDKITE_GIT_MIRRORS_PATH",
		"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
		"BUILDKITE_GIT_SUBMODULES",
		"BUILDKITE_HOOKS_PATH",
		"BUILDKITE_KUBERNETES_EXEC",
		"BUILDKITE_LOCAL_HOOKS_ENABLED",
		"BUILDKITE_PLUGINS_ENABLED",
		"BUILDKITE_PLUGINS_PATH",
		"BUILDKITE_SHELL",
		"BUILDKITE_SSH_KEYSCAN",
	}

	// Verify that all expected variables are protected
	for _, envVar := range expectedProtected {
		if !IsProtected(envVar) {
			t.Errorf("Expected %s to be protected, but IsProtected returned false", envVar)
		}
	}

	// Verify that non-protected variables are not protected
	nonProtected := []string{
		"MY_CUSTOM_VAR",
		"SECRET_KEY",
		"DATABASE_URL",
		"API_TOKEN",
		"BUILDKITE_BRANCH",  // This is a standard build env var, not protected
		"BUILDKITE_COMMIT",  // This is a standard build env var, not protected
		"BUILDKITE_MESSAGE", // This is a standard build env var, not protected
	}

	for _, envVar := range nonProtected {
		if IsProtected(envVar) {
			t.Errorf("Expected %s to NOT be protected, but IsProtected returned true", envVar)
		}
	}

	// Verify ProtectedEnv map has the expected size
	expectedSize := len(expectedProtected)
	actualSize := len(ProtectedEnv)
	if actualSize != expectedSize {
		t.Errorf("Expected ProtectedEnv to have %d entries, but got %d", expectedSize, actualSize)
	}
}
