package ordered

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestMapGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc   string
		input  *MapSS
		key    string
		want   string
		wantOk bool
	}{
		{
			desc:   "nil map",
			input:  nil,
			key:    "foo",
			want:   "",
			wantOk: false,
		},
		{
			desc:   "empty map",
			input:  NewMap[string, string](3),
			key:    "foo",
			want:   "",
			wantOk: false,
		},
		{
			desc:   "empty map created with new()",
			input:  new(MapSS),
			key:    "foo",
			want:   "",
			wantOk: false,
		},
		{
			desc: "present key",
			input: MapFromItems(
				TupleSS{Key: "foo", Value: "bar"},
			),
			key:    "foo",
			want:   "bar",
			wantOk: true,
		},
		{
			desc: "wrong key",
			input: MapFromItems(
				TupleSS{Key: "baz", Value: "bar"},
			),
			key:    "foo",
			want:   "",
			wantOk: false,
		},
		{
			desc: "larger map",
			input: MapFromItems(
				TupleSS{Key: "", Value: "quz"},
				TupleSS{Key: "foo", Value: "bar"},
				TupleSS{Key: "baz", Value: "qux"},
			),
			key:    "foo",
			want:   "bar",
			wantOk: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			got, ok := test.input.Get(test.key)
			if got != test.want || ok != test.wantOk {
				t.Errorf("input.Get(%q) = (%q, %t); want (%q, %t)", test.key, got, ok, test.want, test.wantOk)
			}
		})
	}
}

func TestMapSet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		input *MapSS
		key   string
		value string
		want  *MapSS
	}{
		// setting in a nil map will panic, just like Go's map.
		{
			desc:  "empty map",
			input: NewMap[string, string](3),
			key:   "foo",
			value: "bar",
			want:  MapFromItems(TupleSS{Key: "foo", Value: "bar"}),
		},
		{
			desc:  "empty map created with new()",
			input: new(MapSS),
			key:   "foo",
			value: "bar",
			want:  MapFromItems(TupleSS{Key: "foo", Value: "bar"}),
		},
		{
			desc: "new value",
			input: MapFromItems(
				TupleSS{Key: "foo", Value: "bar"},
			),
			key:   "baz",
			value: "qux",
			want: MapFromItems(
				TupleSS{Key: "foo", Value: "bar"},
				TupleSS{Key: "baz", Value: "qux"},
			),
		},
		{
			desc: "change value",
			input: MapFromItems(
				TupleSS{Key: "baz", Value: "bar"},
				TupleSS{Key: "foo", Value: "bar"},
			),
			key:   "baz",
			value: "qux",
			want: MapFromItems(
				TupleSS{Key: "baz", Value: "qux"},
				TupleSS{Key: "foo", Value: "bar"},
			),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			test.input.Set(test.key, test.value)
			if diff := cmp.Diff(test.input, test.want, cmp.Comparer(EqualSS)); diff != "" {
				t.Errorf("after Set(%q, %q), map diff (-got +want):\n%s", test.key, test.value, diff)
			}
		})
	}
}

func TestMapReplace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc   string
		input  *MapSS
		oldkey string
		newkey string
		value  string
		want   *MapSS
	}{
		// setting in a nil map will panic, just like Go's map.
		{
			desc:   "empty map",
			input:  NewMap[string, string](3),
			oldkey: "zzz",
			newkey: "foo",
			value:  "bar",
			want:   MapFromItems(TupleSS{Key: "foo", Value: "bar"}),
		},
		{
			desc:   "empty map created with new()",
			input:  new(MapSS),
			oldkey: "zzz",
			newkey: "foo",
			value:  "bar",
			want:   MapFromItems(TupleSS{Key: "foo", Value: "bar"}),
		},
		{
			desc: "old = new",
			input: MapFromItems(
				TupleSS{Key: "foo", Value: "bar"},
			),
			oldkey: "foo",
			newkey: "foo",
			value:  "qux",
			want: MapFromItems(
				TupleSS{Key: "foo", Value: "qux"},
			),
		},
		{
			desc: "old != new",
			input: MapFromItems(
				TupleSS{Key: "baz", Value: "qux"},
				TupleSS{Key: "foo", Value: "bar"},
			),
			oldkey: "baz",
			newkey: "biz",
			value:  "tux",
			want: MapFromItems(
				TupleSS{Key: "biz", Value: "tux"},
				TupleSS{Key: "foo", Value: "bar"},
			),
		},
		{
			desc: "old != new and new already exists",
			input: MapFromItems(
				TupleSS{Key: "baz", Value: "qux"},
				TupleSS{Key: "foo", Value: "bar"},
			),
			oldkey: "baz",
			newkey: "foo",
			value:  "tux",
			want: MapFromItems(
				TupleSS{Key: "foo", Value: "tux"},
			),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			test.input.Replace(test.oldkey, test.newkey, test.value)
			if diff := cmp.Diff(test.input, test.want, cmp.Comparer(EqualSS)); diff != "" {
				t.Errorf("after Replace(%q, %q, %q), map diff (-got +want):\n%s", test.oldkey, test.newkey, test.value, diff)
			}
		})
	}
}

func TestMapDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		input *MapSS
		key   string
		want  *MapSS
	}{
		{
			desc:  "nil map",
			input: nil,
			key:   "foo",
			want:  nil,
		},
		{
			desc:  "empty map",
			input: NewMap[string, string](3),
			key:   "foo",
			want:  NewMap[string, string](0),
		},
		{
			desc:  "empty map created with new()",
			input: new(MapSS),
			key:   "foo",
			want:  NewMap[string, string](0),
		},
		{
			desc: "deleting the one value",
			input: MapFromItems(
				TupleSS{Key: "foo", Value: "bar"},
			),
			key:  "foo",
			want: NewMap[string, string](0),
		},
		{
			desc: "deleting one of two values",
			input: MapFromItems(
				TupleSS{Key: "baz", Value: "bar"},
				TupleSS{Key: "foo", Value: "bar"},
			),
			key: "baz",
			want: MapFromItems(
				TupleSS{Key: "foo", Value: "bar"},
			),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			test.input.Delete(test.key)
			if diff := cmp.Diff(test.input, test.want, cmp.Comparer(EqualSS)); diff != "" {
				t.Errorf("after Delete(%q), map diff (-got +want):\n%s", test.key, diff)
			}
		})
	}
}

func TestMarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		input any
		want  string
	}{
		{
			desc: "String-string map",
			input: MapFromItems(
				TupleSS{Key: "llama", Value: "llama"},
				TupleSS{Key: "alpaca", Value: "drama"},
			),
			want: `{"llama":"llama","alpaca":"drama"}`,
		},
		{
			desc: "String-any map",
			input: MapFromItems(
				TupleSA{Key: "llama", Value: "llama"},
				TupleSA{Key: "alpaca", Value: "drama"},
			),
			want: `{"llama":"llama","alpaca":"drama"}`,
		},
		{
			desc:  "Nil map",
			input: (*MapSA)(nil),
			want:  "null",
		},
		{
			desc:  "Empty map",
			input: NewMap[string, any](0),
			want:  "{}",
		},
		{
			desc: "Nested maps",
			input: MapFromItems(
				TupleSA{
					Key: "llama",
					Value: MapFromItems(
						TupleSS{Key: "Kuzco", Value: "Emperor"},
						TupleSS{Key: "Geronimo", Value: "Incredible"},
					),
				},
				TupleSA{Key: "alpaca", Value: "drama"},
			),
			want: `{"llama":{"Kuzco":"Emperor","Geronimo":"Incredible"},"alpaca":"drama"}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(test.input)
			if err != nil {
				t.Fatalf("json.Marshal(%v) error = %v", test.input, err)
			}
			if diff := cmp.Diff(string(got), test.want); diff != "" {
				t.Errorf("json.Marshal(%v) diff (-got +want):\n%s", test.input, diff)
			}
		})
	}
}

func TestToMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		input *Map[string, any]
		want  map[string]any
	}{
		{
			desc:  "nil input",
			input: nil,
			want:  nil,
		},
		{
			desc:  "empty input",
			input: NewMap[string, any](0),
			want:  map[string]any{},
		},
		{
			desc: "basic input",
			input: MapFromItems(
				TupleSA{Key: "llama", Value: "drama"},
			),
			want: map[string]any{"llama": "drama"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			got := test.input.ToMap()
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("test.input.ToMap() diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestMarshalYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		input any
		want  string
	}{
		{
			desc: "String-string map",
			input: MapFromItems(
				TupleSS{Key: "llama", Value: "llama"},
				TupleSS{Key: "alpaca", Value: "drama"},
			),
			want: "llama: llama\nalpaca: drama\n",
		},
		{
			desc: "String-any map",
			input: MapFromItems(
				TupleSA{Key: "llama", Value: "llama"},
				TupleSA{Key: "alpaca", Value: "drama"},
			),
			want: "llama: llama\nalpaca: drama\n",
		},
		{
			desc:  "Nil map",
			input: (*MapSA)(nil),
			want:  "null\n",
		},
		{
			desc:  "Empty map",
			input: NewMap[string, any](0),
			want:  "{}\n",
		},
		{
			desc: "Nested maps",
			input: MapFromItems(
				TupleSA{
					Key: "llama",
					Value: MapFromItems(
						TupleSS{Key: "Kuzco", Value: "Emperor"},
						TupleSS{Key: "Geronimo", Value: "Incredible"},
					),
				},
				TupleSA{Key: "alpaca", Value: "drama"},
			),
			want: `llama:
    Kuzco: Emperor
    Geronimo: Incredible
alpaca: drama
`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			got, err := yaml.Marshal(test.input)
			if err != nil {
				t.Fatalf("yaml.Marshal(%v) error = %v", test.input, err)
			}
			if diff := cmp.Diff(string(got), test.want); diff != "" {
				t.Errorf("yaml.Marshal(%v) diff (-got +want):\n%s", test.input, diff)
			}
		})
	}
}

func TestUnmarshalYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		input string
		want  any
	}{
		{
			desc: "Map containing sequence containing map",
			input: `---
hello:
  - yes
  - this
  - is: dog
    how: are you today
    i am: good thanks
`,
			want: MapFromItems(
				TupleSA{Key: "hello", Value: []any{
					"yes",
					"this",
					MapFromItems(
						TupleSA{Key: "is", Value: "dog"},
						TupleSA{Key: "how", Value: "are you today"},
						TupleSA{Key: "i am", Value: "good thanks"},
					),
				}},
			),
		},
		{
			desc: "3GZX: Spec Example 7.1. Alias Nodes",
			input: `First occurrence: &anchor Foo
Second occurrence: *anchor
Override anchor: &anchor Bar
Reuse anchor: *anchor`,
			want: MapFromItems(
				TupleSA{Key: "First occurrence", Value: "Foo"},
				TupleSA{Key: "Second occurrence", Value: "Foo"},
				TupleSA{Key: "Override anchor", Value: "Bar"},
				TupleSA{Key: "Reuse anchor", Value: "Bar"},
			),
		},
		{
			desc: "4ABK: Flow Mapping Separate Values",
			input: `{
				unquoted : "separate",
				http://foo.com,
				omitted value:,
}`,
			want: MapFromItems(
				TupleSA{Key: "unquoted", Value: "separate"},
				TupleSA{Key: "http://foo.com", Value: nil},
				TupleSA{Key: "omitted value:", Value: nil},
			),
		},
		{
			desc: "4CQQ: Spec Example 2.18. Multi-line Flow Scalars",
			input: `plain:
  This unquoted scalar
  spans many lines.

quoted: "So does this
  quoted scalar.\n"
`,
			want: MapFromItems(
				TupleSA{Key: "plain", Value: "This unquoted scalar spans many lines."},
				TupleSA{Key: "quoted", Value: "So does this quoted scalar.\n"},
			),
		},
		{
			desc: "Pipe syntax",
			input: `pipe: |
  This is a multiline literal
  here is the other line
  the newlines should be preserved`,
			want: MapFromItems(
				TupleSA{Key: "pipe", Value: "This is a multiline literal\nhere is the other line\nthe newlines should be preserved"},
			),
		},
		{
			desc: "Left crocodile",
			input: `left_croc: >
  This is another multiline literal
  that behaves slightly differently
  (it replaces newlines with spaces)`,
			want: MapFromItems(
				TupleSA{Key: "left_croc", Value: "This is another multiline literal that behaves slightly differently (it replaces newlines with spaces)"},
			),
		},
		{
			desc: "go-yaml/yaml#184",
			input: `---
world: &world
  greeting: Hello
earth:
  << : *world`,
			want: MapFromItems(
				TupleSA{
					Key:   "world",
					Value: MapFromItems(TupleSA{Key: "greeting", Value: "Hello"}),
				},
				TupleSA{
					Key:   "earth",
					Value: MapFromItems(TupleSA{Key: "greeting", Value: "Hello"}),
				},
			),
		},
		{
			desc: "Various map keys",
			input: `---
foo: llama
.nan: llama
!!int 12345: alpaca
!!bool false: gerbil
.inf: hyperllama
-.inf: hypollama`,
			want: MapFromItems(
				TupleSA{Key: "foo", Value: "llama"},
				TupleSA{Key: "NaN", Value: "llama"},
				TupleSA{Key: "12345", Value: "alpaca"},
				TupleSA{Key: "false", Value: "gerbil"},
				TupleSA{Key: "+Inf", Value: "hyperllama"},
				TupleSA{Key: "-Inf", Value: "hypollama"},
			),
		},
		{
			desc: "Various values",
			input: `---
llamas: TRUE
alpacas: False
gerbils: !!bool false
hex: 0x2A
oct: 0o52
unders: 123_456
float: 0.0000000025`,
			want: MapFromItems(
				TupleSA{Key: "llamas", Value: true},
				TupleSA{Key: "alpacas", Value: false},
				TupleSA{Key: "gerbils", Value: false},
				TupleSA{Key: "hex", Value: 42},
				TupleSA{Key: "oct", Value: 42},
				TupleSA{Key: "unders", Value: 123456},
				TupleSA{Key: "float", Value: 2.5e-9},
			),
		},
		{
			desc: "Merge sequence of aliases",
			input: `---
a: &a
  b: c
d: &d
  b: d
e:
  << : [*a, *d]`,
			want: MapFromItems(
				TupleSA{Key: "a", Value: MapFromItems(TupleSA{Key: "b", Value: "c"})},
				TupleSA{Key: "d", Value: MapFromItems(TupleSA{Key: "b", Value: "d"})},
				TupleSA{Key: "e", Value: MapFromItems(TupleSA{Key: "b", Value: "c"})},
			),
		},
		{
			desc: "Infinite recursive merge",
			input: `---
a: &a
  b: c
  << : *a`,
			want: MapFromItems(
				TupleSA{Key: "a", Value: MapFromItems(TupleSA{Key: "b", Value: "c"})},
			),
		},
		{
			desc: "Multiple aliases",
			input: `---
a: &a
  b: c
d:
  da: *a
  db: *a`,
			want: MapFromItems(
				TupleSA{Key: "a", Value: MapFromItems(TupleSA{Key: "b", Value: "c"})},
				TupleSA{Key: "d", Value: MapFromItems(
					TupleSA{Key: "da", Value: MapFromItems(TupleSA{Key: "b", Value: "c"})},
					TupleSA{Key: "db", Value: MapFromItems(TupleSA{Key: "b", Value: "c"})},
				)},
			),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			got := NewMap[string, any](0)
			if err := yaml.Unmarshal([]byte(test.input), &got); err != nil {
				t.Fatalf("yaml.Unmarshal(input, &got) = %v", err)
			}

			if diff := cmp.Diff(got, test.want, cmp.Comparer(EqualSA)); diff != "" {
				t.Errorf("unmarshaled map diff (-got +want):\n%s", diff)
			}

			// Now round-trip via JSON.
			out, err := json.Marshal(got)
			if err != nil {
				t.Errorf("json.Marshal(got) error = %v", err)
			}
			got2 := NewMap[string, any](0)
			if err := json.Unmarshal(out, &got2); err != nil {
				t.Errorf("json.Unmarshal(out, &got2) = %v", err)
			}

			if diff := cmp.Diff(got2, test.want, cmp.Comparer(EqualSA)); diff != "" {
				t.Errorf("unmarshaled JSON-round-trip map diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestUnmarshalInfiniteAlias(t *testing.T) {
	t.Parallel()

	input := `---
a: &a
  b: c
  d: *a`
	m := NewMap[string, any](1)
	if err := yaml.Unmarshal([]byte(input), &m); err == nil {
		t.Errorf("yaml.Unmarshal(%q, &m) error = %v, want non-nil error (and not a stack overflow)", input, err)
	}
}

func TestUnmarshalYAMLUnusual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc     string
		start    any
		input    string
		want     any
		comparer cmp.Option
	}{
		{
			desc:  "Map containing sequence containing map",
			start: NewMap[string, []any](0),
			input: `---
hello:
  - yes
  - this
  - is: dog
    how: are you today
    i am: good thanks
`,
			want: MapFromItems(
				Tuple[string, []any]{Key: "hello", Value: []any{
					"yes",
					"this",
					MapFromItems(
						TupleSA{Key: "is", Value: "dog"},
						TupleSA{Key: "how", Value: "are you today"},
						TupleSA{Key: "i am", Value: "good thanks"},
					),
				}},
			),
			comparer: cmp.Comparer(Equal[string, []any]),
		},
		{
			desc:  "Map containing sequence containing map 2",
			start: NewMap[string, []*MapSS](0),
			input: `---
hello:
  - yes: this
  - is: dog
    how: are you today
    i am: good thanks
`,
			want: MapFromItems(
				Tuple[string, []*MapSS]{Key: "hello", Value: []*MapSS{
					MapFromItems(
						TupleSS{Key: "yes", Value: "this"},
					),
					MapFromItems(
						TupleSS{Key: "is", Value: "dog"},
						TupleSS{Key: "how", Value: "are you today"},
						TupleSS{Key: "i am", Value: "good thanks"},
					),
				}},
			),
			comparer: cmp.Comparer(Equal[string, []*MapSS]),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			if err := yaml.Unmarshal([]byte(test.input), test.start); err != nil {
				t.Fatalf("yaml.Unmarshal(input, &test.start) = %v", err)
			}

			if diff := cmp.Diff(test.start, test.want, test.comparer); diff != "" {
				t.Errorf("unmarshaled map diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestToMapRecursive(t *testing.T) {
	t.Parallel()

	src := MapFromItems(
		TupleSA{Key: "llama", Value: "Kuzco"},
		TupleSA{Key: "alpaca", Value: "Geronimo"},
		TupleSA{Key: "nil", Value: nil},
		TupleSA{Key: "nested", Value: []any{
			MapFromItems(
				TupleSA{Key: "llama", Value: "Kuzco1"},
				TupleSA{Key: "alpaca", Value: "Geronimo1"},
			),
			MapFromItems(
				TupleSA{Key: "llama", Value: "Kuzco2"},
				TupleSA{Key: "alpaca", Value: "Geronimo2"},
				TupleSA{Key: "nested again", Value: MapFromItems(
					TupleSA{Key: "llama", Value: "Kuzco3"},
					TupleSA{Key: "alpaca", Value: "Geronimo3"},
				)},
			),
		}},
	)

	got := ToMapRecursive(src)

	want := map[string]any{
		"llama":  "Kuzco",
		"alpaca": "Geronimo",
		"nil":    nil,
		"nested": []any{
			map[string]any{
				"llama":  "Kuzco1",
				"alpaca": "Geronimo1",
			},
			map[string]any{
				"llama":  "Kuzco2",
				"alpaca": "Geronimo2",
				"nested again": map[string]any{
					"llama":  "Kuzco3",
					"alpaca": "Geronimo3",
				},
			},
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("ToMapRecursive output diff (-got +want):\n%s", diff)
	}
}
