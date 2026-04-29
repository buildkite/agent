package env

import (
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestEnvironmentExists(t *testing.T) {
	t.Parallel()

	env := New()

	env.Set("FOO", "bar")
	env.Set("EMPTY", "")

	if got, want := env.Exists("FOO"), true; got != want {
		t.Errorf("env.Exists(\"FOO\") = %t, want %t", got, want)
	}
	if got, want := env.Exists("EMPTY"), true; got != want {
		t.Errorf("env.Exists(\"EMPTY\") = %t, want %t", got, want)
	}
	if got, want := env.Exists("does not exist"), false; got != want {
		t.Errorf("env.Exists(\"does not exist\") = %t, want %t", got, want)
	}
}

func TestEnvironmentSet(t *testing.T) {
	t.Parallel()

	env := New()

	env.Set("    THIS_IS_THE_BEST   \n\n", "\"IT SURE IS\"\n\n")

	v, ok := env.Get("    THIS_IS_THE_BEST   \n\n")
	if got, want := v, "\"IT SURE IS\"\n\n"; got != want {
		t.Errorf("env.Get(%q) = %q, want %q", "    THIS_IS_THE_BEST   \n\n", got, want)
	}
	if got := ok; !got {
		t.Errorf("env.Get(%q) = %t, want true", "    THIS_IS_THE_BEST   \n\n", got)
	}
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
		if got := e.Exists(mountain); !got {
			t.Errorf("e.Exists(mountain) = %t, want true", got)
		}
		if got := e.Exists(strings.ToUpper(mountain)); !got {
			t.Errorf("e.Exists(strings.ToUpper(mountain)) = %t, want true", got)
		}

		v, _ := e.Get(strings.ToUpper(mountain))
		if got, want := v, "Cerro Torre"; got != want {
			t.Errorf("e.Get(%q) = %q, want %q", strings.ToUpper(mountain), got, want)
		}

		e.Set(strings.ToUpper(mountain), "Cerro Poincenot")

		v, _ = e.Get(mountain)
		if got, want := v, "Cerro Poincenot"; got != want {
			t.Errorf("e.Get(%q) = %q, want %q", mountain, got, want)
		}

		v, _ = e.Get(strings.ToUpper(mountain))
		if got, want := v, "Cerro Poincenot"; got != want {
			t.Errorf("e.Get(%q) = %q, want %q", strings.ToUpper(mountain), got, want)
		}

	default:
		// Two keys with the same letters but different cases can coexist
		// (i.e. "Mountain", "mountain", "MOUNTAIN" are treated as three different keys)
		if got := e.Exists(mountain); !got {
			t.Errorf("e.Exists(mountain) = %t, want true", got)
		}
		if got := e.Exists(strings.ToUpper(mountain)); got {
			t.Errorf("e.Exists(strings.ToUpper(mountain)) = %t, want false", got)
		}

		e.Set(strings.ToUpper(mountain), "Cerro Poincenot")

		camel, _ := e.Get(mountain)
		if got, want := camel, "Cerro Torre"; got != want {
			t.Errorf("e.Get(%q) = %q, want %q", mountain, got, want)
		}

		upper, _ := e.Get(strings.ToUpper(mountain))
		if got, want := upper, "Cerro Poincenot"; got != want {
			t.Errorf("e.Get(%q) = %q, want %q", strings.ToUpper(mountain), got, want)
		}
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

	if got := env.GetBool("LLAMAS_ENABLED", false); !got {
		t.Errorf("env.GetBool(\"LLAMAS_ENABLED\", false) = %t, want true", got)
	}
	if got := env.GetBool("ALPACAS_ENABLED", true); got {
		t.Errorf("env.GetBool(\"ALPACAS_ENABLED\", true) = %t, want false", got)
	}
	if got := env.GetBool("PLATYPUS_ENABLED", false); got {
		t.Errorf("env.GetBool(\"PLATYPUS_ENABLED\", false) = %t, want false", got)
	}
	if got := env.GetBool("PLATYPUS_ENABLED", true); !got {
		t.Errorf("env.GetBool(\"PLATYPUS_ENABLED\", true) = %t, want true", got)
	}
	if got := env.GetBool("BUNYIP_ENABLED", true); got {
		t.Errorf("env.GetBool(\"BUNYIP_ENABLED\", true) = %t, want false", got)
	}
}

func TestEnvironmentRemove(t *testing.T) {
	env := FromSlice([]string{"FOO=bar"})

	v, ok := env.Get("FOO")
	if got, want := v, "bar"; got != want {
		t.Errorf("env.Get(%q) = %q, want %q", "FOO", got, want)
	}
	if got := ok; !got {
		t.Errorf("env.Get(%q) = %t, want true", "FOO", got)
	}

	if got, want := env.Remove("FOO"), "bar"; got != want {
		t.Errorf("env.Remove(\"FOO\") = %q, want %q", got, want)
	}

	v, ok = env.Get("FOO")
	if got, want := v, ""; got != want {
		t.Errorf("env.Get(%q) = %q, want %q", "FOO", got, want)
	}
	if got := ok; got {
		t.Errorf("env.Get(%q) = %t, want false", "FOO", got)
	}
}

func TestEnvironmentMerge(t *testing.T) {
	t.Parallel()

	env1 := FromSlice([]string{"FOO=bar"})
	env2 := FromSlice([]string{"BAR=foo"})

	env1.Merge(env2)

	if diff := cmp.Diff(env1.ToSlice(), []string{"BAR=foo", "FOO=bar"}); diff != "" {
		t.Errorf("env1.ToSlice() diff (-got +want):\n%s", diff)
	}
}

func TestEnvironmentCopy(t *testing.T) {
	t.Parallel()

	env1 := FromSlice([]string{"FOO=bar"})
	env2 := env1.Copy()

	if diff := cmp.Diff(env2.ToSlice(), []string{"FOO=bar"}); diff != "" {
		t.Errorf("env2.ToSlice() diff (-got +want):\n%s", diff)
	}

	env1.Set("FOO", "not-bar-anymore")

	if diff := cmp.Diff(env2.ToSlice(), []string{"FOO=bar"}); diff != "" {
		t.Errorf("env2.ToSlice() diff (-got +want):\n%s", diff)
	}
}

func TestEnvironmentToSlice(t *testing.T) {
	t.Parallel()

	env := FromSlice([]string{"THIS_IS_GREAT=totes", "ZOMG=greatness"})

	if diff := cmp.Diff(env.ToSlice(), []string{"THIS_IS_GREAT=totes", "ZOMG=greatness"}); diff != "" {
		t.Errorf("env.ToSlice() diff (-got +want):\n%s", diff)
	}
}

func TestEnvironmentDiff(t *testing.T) {
	t.Parallel()
	a := FromSlice([]string{"A=hello", "B=world"})
	b := FromSlice([]string{"A=hello", "B=there", "C=new", "D="})

	ab := a.Diff(b)
	if diff := cmp.Diff(ab, Diff{
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
	}); diff != "" {
		t.Errorf("a.Diff(b) diff (-got +want):\n%s", diff)
	}

	ba := b.Diff(a)
	if diff := cmp.Diff(ba, Diff{
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
	}); diff != "" {
		t.Errorf("b.Diff(a) diff (-got +want):\n%s", diff)
	}
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

	if diff := cmp.Diff(diff, Diff{
		Added:   map[string]string{},
		Changed: map[string]DiffPair{},
		Removed: map[string]struct{}{},
	}); diff != "" {
		t.Errorf("Diff{\n\tAdded: map[string]string{\n\t\t\"A\": \"new\",\n\t},\n\tChanged: map[string]DiffPair{\n\t\t\"B\": {\n\t\t\tOld:\t\"old\",\n\t\t\tNew:\t\"new\",\n\t\t},\n\t},\n\tRemoved: map[string]struct{}{\n\t\t\"C\": {},\n\t},\n} diff (-got +want):\n%s", diff)
	}
}

func TestEmptyDiff(t *testing.T) {
	t.Parallel()

	empty := Diff{}

	if got, want := empty.Empty(), true; got != want {
		t.Errorf("empty.Empty() = %t, want %t", got, want)
	}
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
	if diff := cmp.Diff(env.Dump(), FromSlice([]string{
		"LLAMAS_ENABLED=1",
	}).Dump()); diff != "" {
		t.Errorf("env.Dump() diff (-got +want):\n%s", diff)
	}

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
	if diff := cmp.Diff(env.Dump(), FromSlice([]string{
		"ALPACAS_ENABLED=1",
		"LLAMAS_ENABLED=0",
	}).Dump()); diff != "" {
		t.Errorf("env.Dump() diff (-got +want):\n%s", diff)
	}

	env.Apply(Diff{
		Added:   map[string]string{},
		Changed: map[string]DiffPair{},
		Removed: map[string]struct{}{
			"LLAMAS_ENABLED":  {},
			"ALPACAS_ENABLED": {},
		},
	})
	if diff := cmp.Diff(env.Dump(), FromSlice([]string{}).Dump()); diff != "" {
		t.Errorf("env.Dump() diff (-got +want):\n%s", diff)
	}
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
