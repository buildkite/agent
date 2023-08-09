package ordered

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

type ordinaryStruct struct {
	Key      string
	Mountain string `yaml:"molehill"`
	Ignore   string `yaml:"-"`
	Switch   bool
	Count    int
	Fader    float64
	Slicey   []int
	hidden   string

	Next  *ordinaryStruct
	Inner struct {
		Llama string
	}

	Remaining map[string]any `yaml:",inline"`
}

type inlineAnyStruct struct {
	Llama     string
	Remaining any `yaml:",inline"`
}

type inlineOrderedMapStruct struct {
	Llama     string
	Remaining *Map[string, any] `yaml:",inline"`
}

type customUnmarshalStruct struct {
	Llama string
}

type namedMap map[string]string

func (o *customUnmarshalStruct) UnmarshalOrdered(src any) error {
	if src != "Kuzco" {
		o.Llama = "Not Kuzco"
	} else {
		o.Llama = "Kuzco"
	}
	return nil
}

type testNestedOverride struct {
	Llama   string
	Another *customUnmarshalStruct
}

func ptr[T any](x T) *T { return &x }

func TestUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc           string
		src, dst, want any
	}{
		{
			desc: "nil into nil",
			src:  nil,
			dst:  nil,
			want: nil,
		},
		{
			desc: "nil into nil (but typed)",
			src:  nil,
			dst:  (*struct{})(nil),
			want: (*struct{})(nil),
		},
		{
			desc: "string to *string",
			src:  "hello",
			dst:  new(string),
			want: ptr("hello"),
		},
		{
			desc: "string to *any",
			src:  "hello",
			dst:  new(any),
			want: ptr[any]("hello"),
		},
		{
			desc: "string to *[]string",
			src:  "hello",
			dst:  new([]string),
			want: ptr([]string{"hello"}),
		},
		{
			desc: "string to *[]any",
			src:  "hello",
			dst:  new([]any),
			want: ptr([]any{"hello"}),
		},
		{
			desc: "int to *int",
			src:  42,
			dst:  new(int),
			want: ptr(42),
		},
		{
			desc: "int into *string",
			src:  42,
			dst:  new(string),
			want: ptr("42"),
		},
		{
			desc: "int to *any",
			src:  42,
			dst:  new(any),
			want: ptr[any](42),
		},
		{
			desc: "int to *[]int",
			src:  42,
			dst:  new([]int),
			want: ptr([]int{42}),
		},
		{
			desc: "int to *[]string",
			src:  42,
			dst:  new([]string),
			want: ptr([]string{"42"}),
		},
		{
			desc: "int to *[]any",
			src:  42,
			dst:  new([]any),
			want: ptr([]any{42}),
		},
		{
			desc: "float64 to *float64",
			src:  2.71828,
			dst:  new(float64),
			want: ptr(2.71828),
		},
		{
			desc: "float64 into *string",
			src:  2.71828,
			dst:  new(string),
			want: ptr("2.71828"),
		},
		{
			desc: "float64 to *any",
			src:  2.71828,
			dst:  new(any),
			want: ptr[any](2.71828),
		},
		{
			desc: "float64 to *[]float64",
			src:  2.71828,
			dst:  new([]float64),
			want: ptr([]float64{2.71828}),
		},
		{
			desc: "float64 to *[]string",
			src:  2.71828,
			dst:  new([]string),
			want: ptr([]string{"2.71828"}),
		},
		{
			desc: "float64 to *[]any",
			src:  2.71828,
			dst:  new([]any),
			want: ptr([]any{2.71828}),
		},
		{
			desc: "bool to *bool",
			src:  true,
			dst:  new(bool),
			want: ptr(true),
		},
		{
			desc: "bool to *string",
			src:  true,
			dst:  new(string),
			want: ptr("true"),
		},
		{
			desc: "bool to *any",
			src:  true,
			dst:  new(any),
			want: ptr[any](true),
		},
		{
			desc: "bool to *[]bool",
			src:  true,
			dst:  new([]bool),
			want: ptr([]bool{true}),
		},
		{
			desc: "bool to *[]string",
			src:  true,
			dst:  new([]string),
			want: ptr([]string{"true"}),
		},
		{
			desc: "bool to *[]any",
			src:  true,
			dst:  new([]any),
			want: ptr([]any{true}),
		},
		{
			desc: "[]any to []string",
			src:  []any{"hello", "yes", "this is dog"},
			dst:  new([]string), // it feels so wrong, it feels so right
			want: ptr([]string{"hello", "yes", "this is dog"}),
		},
		{
			desc: "[]any to *[]any",
			src:  []any{"hello", "yes", "this is dog"},
			dst:  new([]any), // it feels so wrong, it feels so right
			want: ptr([]any{"hello", "yes", "this is dog"}),
		},
		{
			desc: "[]any to *any",
			src:  []any{"hello", "yes", "this is dog"},
			dst:  new(any),
			want: ptr[any]([]any{"hello", "yes", "this is dog"}),
		},
		{
			desc: "[]any containing ints into *[]string",
			src:  []any{42, 43},
			dst:  new([]string),
			want: ptr([]string{"42", "43"}),
		},
		{
			desc: "*MapSA to **MapSA",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			),
			dst: new(*MapSA), // &((*MapSA)(nil))
			want: ptr(MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			)),
		},
		{
			desc: "*MapSA to *MapSA",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			),
			dst: NewMap[string, any](0),
			want: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			),
		},
		{
			desc: "*MapSA to *map[string]any",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			),
			dst: new(map[string]any), // it feels so wrong, it feels so right
			want: ptr(map[string]any{
				"key":      "value",
				"molehill": "large",
				"switch":   true,
				"count":    42,
				"fader":    2.71828,
				"slicey":   []any{5, 6, 7, 8},
			}),
		},
		{
			desc: "*MapSA to map[string]any",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			),
			dst: make(map[string]any),
			want: map[string]any{
				"key":      "value",
				"molehill": "large",
				"switch":   true,
				"count":    42,
				"fader":    2.71828,
				"slicey":   []any{5, 6, 7, 8},
			},
		},
		{
			desc: "*MapSA to *any",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			),
			dst: new(any),
			want: ptr(any(MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			))),
		},
		{
			desc: "*MapSA to *any secretly containing *MapSA",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			),
			dst: ptr(any(NewMap[string, any](0))),
			want: ptr(any(MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			))),
		},
		{
			desc: "*MapSA to *testStruct without inline",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
			),
			dst: &ordinaryStruct{},
			want: &ordinaryStruct{
				Key:      "value",
				Mountain: "large",
				Switch:   true,
				Count:    42,
				Fader:    2.71828,
				Slicey:   []int{5, 6, 7, 8},
			},
		},
		{
			desc: "*MapSA to *testStruct with inline",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "mountain", Value: "actually we call them molehills here"},
				TupleSA{Key: "ignore", Value: "YOU CANNOT DO THIS!!!"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
				TupleSA{Key: "notAField", Value: "super important"},
			),
			dst: &ordinaryStruct{},
			want: &ordinaryStruct{
				Key:      "value",
				Mountain: "large",
				Switch:   true,
				Count:    42,
				Fader:    2.71828,
				Slicey:   []int{5, 6, 7, 8},
				Remaining: map[string]any{
					"ignore":    "YOU CANNOT DO THIS!!!",
					"mountain":  "actually we call them molehills here",
					"notAField": "super important",
				},
			},
		},
		{
			desc: "*MapSA to *testStruct with existing",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "ignore", Value: "YOU CANNOT DO THIS!!!"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: nil},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
				TupleSA{Key: "hidden", Value: "no"},
				TupleSA{Key: "notAField", Value: "super important"},
			),
			dst: &ordinaryStruct{
				Key:      "old",
				Mountain: "Kilimanjaro",
				Ignore:   "super secret existing data",
				Switch:   false,
				Count:    69,
				Fader:    3.14159,
				Slicey:   []int{1, 2, 3, 4},
				hidden:   "yes",
				Remaining: map[string]any{
					"existing": "wombat",
				},
			},
			want: &ordinaryStruct{
				Key:      "value",
				Mountain: "large",
				Ignore:   "super secret existing data",
				Switch:   true,
				Count:    0, // nil becomes a SetZero call
				Fader:    2.71828,
				Slicey:   []int{1, 2, 3, 4, 5, 6, 7, 8},
				hidden:   "yes",
				Remaining: map[string]any{
					"existing":  "wombat",
					"hidden":    "no",
					"ignore":    "YOU CANNOT DO THIS!!!",
					"notAField": "super important",
				},
			},
		},
		{
			desc: "nested structs",
			src: MapFromItems(
				TupleSA{Key: "key", Value: "value"},
				TupleSA{Key: "molehill", Value: "large"},
				TupleSA{Key: "switch", Value: true},
				TupleSA{Key: "count", Value: 42},
				TupleSA{Key: "fader", Value: 2.71828},
				TupleSA{Key: "notAField", Value: "super important"},
				TupleSA{Key: "slicey", Value: []any{5, 6, 7, 8}},
				TupleSA{Key: "inner", Value: MapFromItems(
					TupleSA{Key: "llama", Value: "Kuzco"},
				)},
				TupleSA{Key: "next", Value: MapFromItems(
					TupleSA{Key: "key", Value: "another value"},
					TupleSA{Key: "molehill", Value: "extra large"},
					TupleSA{Key: "switch", Value: true},
					TupleSA{Key: "count", Value: 42000},
					TupleSA{Key: "fader", Value: 1.618},
				)},
			),
			dst: &ordinaryStruct{},
			want: &ordinaryStruct{
				Key:      "value",
				Mountain: "large",
				Switch:   true,
				Count:    42,
				Fader:    2.71828,
				Slicey:   []int{5, 6, 7, 8},
				Inner:    struct{ Llama string }{"Kuzco"},
				Next: &ordinaryStruct{
					Key:      "another value",
					Mountain: "extra large",
					Switch:   true,
					Count:    42000,
					Fader:    1.618,
				},
				Remaining: map[string]any{
					"notAField": "super important",
				},
			},
		},
		{
			desc: "inline field is any",
			src: MapFromItems(
				TupleSA{Key: "llama", Value: "Kuzco"},
				TupleSA{Key: "alpaca", Value: "Geronimo"},
			),
			dst: &inlineAnyStruct{},
			want: &inlineAnyStruct{
				Llama: "Kuzco",
				Remaining: MapFromItems(
					TupleSA{Key: "alpaca", Value: "Geronimo"},
				),
			},
		},
		{
			desc: "inline field is *MapSA",
			src: MapFromItems(
				TupleSA{Key: "llama", Value: "Kuzco"},
				TupleSA{Key: "alpaca", Value: "Geronimo"},
			),
			dst: &inlineOrderedMapStruct{},
			want: &inlineOrderedMapStruct{
				Llama: "Kuzco",
				Remaining: MapFromItems(
					TupleSA{Key: "alpaca", Value: "Geronimo"},
				),
			},
		},
		{
			desc: "nil into *any",
			src:  nil,
			dst:  new(any),
			want: new(any),
		},
		{
			desc: "nil into **struct{}",
			src:  nil,
			dst:  ptr(new(struct{})),
			want: ptr((*struct{})(nil)),
		},
		{
			desc: "UnmarshalOrdered override",
			src:  "Kuzco",
			dst:  &customUnmarshalStruct{},
			want: &customUnmarshalStruct{Llama: "Kuzco"},
		},
		{
			desc: "field is UnmarshalOrdered",
			src: MapFromItems(
				TupleSA{Key: "llama", Value: "Kuzco"},
				TupleSA{Key: "another", Value: "Kronk"},
			),
			dst: &testNestedOverride{},
			want: &testNestedOverride{
				Llama: "Kuzco",
				Another: &customUnmarshalStruct{
					Llama: "Not Kuzco",
				},
			},
		},
		{
			desc: "*MapSA into namedMap",
			src: MapFromItems(
				TupleSA{Key: "llama", Value: "Kuzco"},
				TupleSA{Key: "another", Value: "Kronk"},
			),
			dst: make(namedMap),
			want: namedMap{
				"llama":   "Kuzco",
				"another": "Kronk",
			},
		},
		{
			desc: "*MapSA into *namedMap",
			src: MapFromItems(
				TupleSA{Key: "llama", Value: "Kuzco"},
				TupleSA{Key: "another", Value: "Kronk"},
			),
			dst: new(namedMap),
			want: &namedMap{
				"llama":   "Kuzco",
				"another": "Kronk",
			},
		},
		{
			desc: "*yaml.Node into testStruct",
			src: &yaml.Node{
				Kind: yaml.MappingNode,
				Tag:  "!!map",
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "key"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "value"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "molehill"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "large"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "switch"},
					{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "count"},
					{Kind: yaml.ScalarNode, Tag: "!!int", Value: "42"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "fader"},
					{Kind: yaml.ScalarNode, Tag: "!!float", Value: "2.71828"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "slicey"},
					{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Tag: "!!int", Value: "5"},
						{Kind: yaml.ScalarNode, Tag: "!!int", Value: "6"},
						{Kind: yaml.ScalarNode, Tag: "!!int", Value: "7"},
						{Kind: yaml.ScalarNode, Tag: "!!int", Value: "8"},
					}},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "next"},
					{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "key"},
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "another value"},
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "molehill"},
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "extra large"},
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "switch"},
						{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"},
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "count"},
						{Kind: yaml.ScalarNode, Tag: "!!int", Value: "42000"},
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "fader"},
						{Kind: yaml.ScalarNode, Tag: "!!float", Value: "1.618"},
					}},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "inner"},
					{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "llama"},
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "Kuzco"},
					}},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "notAField"},
					{Kind: yaml.ScalarNode, Tag: "!!str", Value: "super important"},
				},
			},
			dst: &ordinaryStruct{},
			want: &ordinaryStruct{
				Key:      "value",
				Mountain: "large",
				Switch:   true,
				Count:    42,
				Fader:    2.71828,
				Slicey:   []int{5, 6, 7, 8},
				Inner:    struct{ Llama string }{"Kuzco"},
				Next: &ordinaryStruct{
					Key:      "another value",
					Mountain: "extra large",
					Switch:   true,
					Count:    42000,
					Fader:    1.618,
				},
				Remaining: map[string]any{
					"notAField": "super important",
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			if err := Unmarshal(test.src, test.dst); err != nil {
				t.Fatalf("Unmarshal(%T, %T) = %v", test.src, test.dst, err)
			}
			if diff := cmp.Diff(test.dst, test.want, cmp.AllowUnexported(ordinaryStruct{}), cmp.Comparer(EqualSA)); diff != "" {
				t.Errorf("Unmarshal(%T, %T) diff (-got, want):\n%s", test.src, test.dst, diff)
			}
		})
	}
}

func TestUnmarshalIntoNilErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc     string
		src, dst any
	}{
		{
			desc: "*MapSA into interface nil",
			src:  MapFromItems(TupleSA{}),
			dst:  nil,
		},
		{
			desc: "*MapSA into **MapSA nil",
			src:  MapFromItems(TupleSA{}),
			dst:  (**MapSA)(nil),
		},
		{
			desc: "*MapSA into *MapSA nil",
			src:  MapFromItems(TupleSA{}),
			dst:  (*MapSA)(nil),
		},
		{
			desc: "*MapSA into *map[string]any nil",
			src:  MapFromItems(TupleSA{}),
			dst:  (*map[string]any)(nil),
		},
		{
			desc: "*MapSA into map[string]any nil",
			src:  MapFromItems(TupleSA{}),
			dst:  (map[string]any)(nil),
		},
		{
			desc: "*MapSA into *any nil",
			src:  MapFromItems(TupleSA{}),
			dst:  (*any)(nil),
		},
		{
			desc: "*MapSA into *struct nil",
			src:  MapFromItems(TupleSA{}),
			dst:  (*struct{})(nil),
		},
		{
			desc: "[]any into *[]any nil",
			src:  []any{},
			dst:  (*[]any)(nil),
		},
		{
			desc: "[]any into *any nil",
			src:  []any{},
			dst:  (*any)(nil),
		},
		{
			desc: "[]any into *[]string nil",
			src:  []any{},
			dst:  (*[]string)(nil),
		},
		{
			desc: "string into *string nil",
			src:  "hello",
			dst:  (*string)(nil),
		},
		{
			desc: "float64 into *float64 nil",
			src:  3.14159,
			dst:  (*float64)(nil),
		},
		{
			desc: "int into *int nil",
			src:  42,
			dst:  (*int)(nil),
		},
		{
			desc: "bool into *bool nil",
			src:  true,
			dst:  (*bool)(nil),
		},
		{
			desc: "string into *any nil",
			src:  "hello",
			dst:  (*any)(nil),
		},
		{
			desc: "float64 into *any nil",
			src:  3.14159,
			dst:  (*any)(nil),
		},
		{
			desc: "int into *any nil",
			src:  42,
			dst:  (*any)(nil),
		},
		{
			desc: "bool into *any nil",
			src:  true,
			dst:  (*any)(nil),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			if err := Unmarshal(test.src, test.dst); !errors.Is(err, ErrIntoNil) {
				t.Errorf("Unmarshal(%T, %T) = %v, want %v", test.src, test.dst, err, ErrIntoNil)
			}
		})
	}
}

func TestUnmarshalIncompatibleTypesErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc     string
		src, dst any
	}{
		{
			desc: "string into *int",
			src:  "forty-two",
			dst:  new(int),
		},
		{
			desc: "bool into *float64",
			src:  true,
			dst:  new(float64),
		},
		{
			desc: "float64 into *bool",
			src:  3.14159,
			dst:  new(bool),
		},
		{
			desc: "[]any into *string",
			src:  []any{"string"},
			dst:  new(string),
		},
		{
			desc: "[]any containing ints into *[]bool",
			src:  []any{42, 43},
			dst:  new([]bool),
		},
		{
			desc: "*MapSA into *[]any",
			src:  MapFromItems(TupleSA{}),
			dst:  new([]any),
		},
		{
			desc: "*MapSA into **[]any",
			src:  MapFromItems(TupleSA{}),
			dst:  ptr(new([]any)),
		},
		{
			desc: "*MapSA into map[int]string",
			src:  MapFromItems(TupleSA{}),
			dst:  map[int]string{},
		},
		{
			desc: "*MapSA into *map[int]string",
			src:  MapFromItems(TupleSA{}),
			dst:  &map[int]string{},
		},
		{
			desc: "*MapSA into map[string]int",
			src:  MapFromItems(TupleSA{Key: "llama", Value: "drama"}),
			dst:  map[string]int{},
		},
		{
			desc: "*MapSA into *map[string]int",
			src:  MapFromItems(TupleSA{Key: "llama", Value: "drama"}),
			dst:  &map[string]int{},
		},
		{
			desc: "*MapSA into string",
			src:  MapFromItems(TupleSA{}),
			dst:  "hello",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			if err := Unmarshal(test.src, test.dst); !errors.Is(err, ErrIncompatibleTypes) {
				t.Errorf("Unmarshal(%T, %T) = %v, want %v", test.src, test.dst, err, ErrIncompatibleTypes)
			}
		})
	}
}

func TestUnmarshalNotAPointerErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc     string
		src, dst any
	}{
		{
			desc: "nil into string",
			src:  nil,
			dst:  "hello",
		},
		{
			desc: "[]any into []any",
			src:  []any{42},
			dst:  []any{69},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			if err := Unmarshal(test.src, test.dst); !errors.Is(err, ErrIntoNonPointer) {
				t.Errorf("Unmarshal(%T, %T) = %v, want %v", test.src, test.dst, err, ErrIntoNonPointer)
			}
		})
	}
}

func TestUnmarshalUnsupportedSrcError(t *testing.T) {
	t.Parallel()
	if err := Unmarshal(make(chan struct{}), new(string)); !errors.Is(err, ErrUnsupportedSrc) {
		t.Errorf("Unmarshal(chan struct{}, new(string)) = %v, want %v", err, ErrUnsupportedSrc)
	}
}

func TestUnmarshalIncompatibleFieldTypeError(t *testing.T) {
	t.Parallel()

	type hasAFloat struct {
		Llama float64
	}
	src := MapFromItems(
		TupleSA{Key: "llama", Value: "drama"},
	)
	if err := Unmarshal(src, &hasAFloat{}); !errors.Is(err, ErrIncompatibleTypes) {
		t.Errorf("Unmarshal(*MapSA, &hasAFloat{}) = %v, want %v", err, ErrIncompatibleTypes)
	}
}

func TestUnmarshalInvalidInlineError(t *testing.T) {
	t.Parallel()

	src := MapFromItems(TupleSA{
		Key: "llama", Value: "not a bool",
	})
	type testInvalidInline struct {
		Llama map[string]bool `yaml:",inline"`
	}
	if err := Unmarshal(src, &testInvalidInline{}); !errors.Is(err, ErrIncompatibleTypes) {
		t.Errorf("Unmarshal(*MapSA, &testInvalidInline{}) = %v, want %v", err, ErrIncompatibleTypes)
	}
}

func TestUnmarshalMultipleInlineError(t *testing.T) {
	t.Parallel()

	type testMultipleInline struct {
		Llama  map[string]any `yaml:",inline"`
		Alpaca map[string]any `yaml:",inline"`
	}
	if err := Unmarshal(MapFromItems(TupleSA{}), &testMultipleInline{}); !errors.Is(err, ErrMultipleInlineFields) {
		t.Errorf("Unmarshal(*MapSA, &testMultipleInline{}) = %v, want %v", err, ErrMultipleInlineFields)
	}
}

func TestMapUnmarshalOrderedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc string
		src  any
		dst  Unmarshaler
	}{
		{
			desc: "receiver with non-string key",
			src:  nil,
			dst:  NewMap[int, string](0),
		},
		{
			desc: "src is not *MapSA",
			src:  map[string]any{},
			dst:  NewMap[string, int](0),
		},
		{
			desc: "incompatible values",
			src:  MapFromItems(TupleSA{Key: "llama", Value: "drama"}),
			dst:  NewMap[string, int](0),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			if err := test.dst.UnmarshalOrdered(test.src); !errors.Is(err, ErrIncompatibleTypes) {
				t.Errorf("%T.UnmarshalOrdered(%T) = %v, want %v", test.dst, test.src, err, ErrIncompatibleTypes)
			}
		})
	}
}

func TestYAMLInlineDashBehaviour(t *testing.T) {
	// This test just confirms a potential edge case in how yaml.v3 does things.
	type what struct {
		Llama     string         `yaml:"-"`
		Remaining map[string]any `yaml:",inline"`
	}

	input := []byte(`---
llama: Kuzco
alpaca: Geronimo
`)

	var got what
	if err := yaml.Unmarshal(input, &got); err != nil {
		t.Fatalf("yaml.Unmarshal(input, &got) = %v", err)
	}

	want := what{
		Llama: "",
		Remaining: map[string]any{
			"llama":  "Kuzco",
			"alpaca": "Geronimo",
		},
	}

	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("yaml.Unmarshal diff (-got +want):\n%s", diff)
	}
}
