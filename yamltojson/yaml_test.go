package yamltojson

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestJSONise(t *testing.T) {
	tests := []struct {
		desc  string
		input string
		want  string
	}{
		{
			desc: "3GZX: Spec Example 7.1. Alias Nodes",
			input: `First occurrence: &anchor Foo
Second occurrence: *anchor
Override anchor: &anchor Bar
Reuse anchor: *anchor`,
			want: `{"First occurrence":"Foo","Second occurrence":"Foo","Override anchor":"Bar","Reuse anchor":"Bar"}`,
		},
		{
			desc: "4ABK: Flow Mapping Separate Values",
			input: `{
				unquoted : "separate",
				http://foo.com,
				omitted value:,
}`,
			want: `{"unquoted":"separate","http://foo.com":null,"omitted value:":null}`,
		},
		{
			desc: "4CQQ: Spec Example 2.18. Multi-line Flow Scalars",
			input: `plain:
  This unquoted scalar
  spans many lines.

quoted: "So does this
  quoted scalar.\n"
`,
			want: `{"plain":"This unquoted scalar spans many lines.","quoted":"So does this quoted scalar.\n"}`,
		},
		{
			desc: "Pipe syntax",
			input: `pipe: |
  This is a multiline literal
  here is the other line
  the newlines should be preserved`,
			want: `{"pipe":"This is a multiline literal\nhere is the other line\nthe newlines should be preserved"}`,
		},
		{
			desc: "Left crocodile",
			input: `left_croc: >
  This is another multiline literal
  that behaves slightly differently
  (it replaces newlines with spaces)`,
			want: `{"left_croc":"This is another multiline literal that behaves slightly differently (it replaces newlines with spaces)"}`,
		},
		{
			desc: "go-yaml/yaml#184",
			input: `---
world: &world
  greeting: Hello
earth:
  << : *world`,
			want: `{"world":{"greeting":"Hello"},"earth":{"greeting":"Hello"}}`,
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
			want: `{"foo":"llama","NaN":"llama","12345":"alpaca","false":"gerbil","+Inf":"hyperllama","-Inf":"hypollama"}`,
		},
		{
			desc: "Various values",
			input: `---
llamas: TRUE
alpacas: False
gerbils: !!bool NO
hex: 0x2A
oct: 0o52
unders: 123_456
float: 0.0000000025`,
			want: `{"llamas":true,"alpacas":false,"gerbils":false,"hex":42,"oct":42,"unders":123456,"float":2.5e-9}`,
		},
		{
			desc: "Sequence node",
			input: `---
- a
- b
- c:
    d: e
- f:
  - g
  - h
  - i`,
			want: `["a","b",{"c":{"d":"e"}},{"f":["g","h","i"]}]`,
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
			want: `{"a":{"b":"c"},"d":{"b":"d"},"e":{"b":"c"}}`,
		},
		{
			desc: "Infinite recursive merge",
			input: `---
a: &a
  b: c
  << : *a`,
			want: `{"a":{"b":"c"}}`,
		},
		{
			desc: "Multiple aliases",
			input: `---
a: &a
  b: c
d:
  da: *a
  db: *a`,
			want: `{"a":{"b":"c"},"d":{"da":{"b":"c"},"db":{"b":"c"}}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			var n yaml.Node
			if err := yaml.Unmarshal([]byte(test.input), &n); err != nil {
				t.Fatalf("yaml.Unmarshal(input) = %v", err)
			}

			var out strings.Builder
			if err := Encode(&out, &n); err != nil {
				t.Fatalf("Encode(&out, &n) = %v", err)
			}
			got, want := out.String(), test.want
			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("Encode diff (-got +want):\n%s", diff)
			}

			// Smoke test that the output parses as JSON
			var dummy any
			if err := json.Unmarshal([]byte(out.String()), &dummy); err != nil {
				t.Errorf("json.Unmarshal(%q) error = %v", out.String(), err)
			}
		})
	}
}

func TestEncodeInfiniteAlias(t *testing.T) {
	input := `---
a: &a
  b: c
  d: *a`
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(input), &n); err != nil {
		t.Fatalf("yaml.Unmarshal(input) = %v", err)
	}
	var dummy bytes.Buffer
	if err := Encode(&dummy, &n); err == nil {
		t.Errorf("Encode(&dummy, &n) error = %v, want non-nil error (and not a stack overflow)", err)
	}
}

func TestUpsertItem(t *testing.T) {
	y, err := UpsertItem(nil, "a", StringNode("b"))
	if err != nil {
		t.Errorf("UpsertItem(nil, a, b) error = %v", err)
	}
	want0 := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			StringNode("a"),
			StringNode("b"),
		},
	}
	if diff := cmp.Diff(y, want0); diff != "" {
		t.Errorf("UpsertItem(nil, a, b) diff (-got +want):\n%s", diff)
	}

	got1, err := UpsertItem(y, "a", StringNode("c"))
	if err != nil {
		t.Errorf("UpsertItem(y, a, c) error = %v", err)
	}
	want1 := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			StringNode("a"),
			StringNode("c"),
		},
	}
	if diff := cmp.Diff(got1, want1); diff != "" {
		t.Errorf("UpsertItem(y, a, c) diff (-got +want):\n%s", diff)
	}

	got2, err := UpsertItem(y, "b", IntNode(1))
	if err != nil {
		t.Errorf("UpsertItem(y, b, 1) error = %v", err)
	}
	want2 := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			StringNode("a"),
			StringNode("c"),
			StringNode("b"),
			IntNode(1),
		},
	}
	if diff := cmp.Diff(got2, want2); diff != "" {
		t.Errorf("UpsertItem(y, b, 1) diff (-got +want):\n%s", diff)
	}
}

func TestLookupItem(t *testing.T) {
	characters := `---
Kuzco: llama
Yzma: evil
Kronk: himbo`

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(characters), &doc); err != nil {
		t.Fatalf("yaml.Unmarshal(characters, &mapping) = %v", err)
	}
	mapping := doc.Content[0]

	// Unimportant differences for this test
	for _, e := range mapping.Content {
		e.Line, e.Column = 0, 0
	}

	tests := []struct {
		input   string
		want    *yaml.Node
		wantErr error
	}{
		{"Kuzco", StringNode("llama"), nil},
		{"Yzma", StringNode("evil"), nil},
		{"Kronk", StringNode("himbo"), nil},
		{"Pacha", nil, ErrNotFound},
	}

	for _, test := range tests {
		got, err := LookupItem(mapping, test.input)
		if err != test.wantErr {
			t.Errorf("LookupItem(mapping, %q) error = %v, want %v", test.input, err, test.wantErr)
		}
		if diff := cmp.Diff(got, test.want); diff != "" {
			t.Errorf("LookupItem(mapping, %q) diff (-got +want):\n%s", test.input, diff)
		}
	}
}
