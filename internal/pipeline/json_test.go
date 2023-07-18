package pipeline

import "testing"

type strukt struct {
	Foo string         `yaml:"foo"`
	Bar string         `yaml:"bar,omitempty"`
	Baz string         `yaml:"-"`
	Qux map[string]any `yaml:",inline"`
}

func TestInlineFriendlyMarshalJSON(t *testing.T) {
	tests := []struct {
		name   string
		strukt strukt
		want   string
	}{
		{
			name: "it combines inline and outline fields into one object",
			want: `{"bar":"bar","country":"ecuador","foo":"foo","mountain":"cotopaxi"}`,
			strukt: strukt{
				Foo: "foo",
				Bar: "bar",
				Qux: map[string]any{
					"mountain": "cotopaxi",
					"country":  "ecuador",
				},
			},
		},
		{
			name: "it correctly omits empty fields when they have omitempty",
			want: `{"foo":""}`,
			strukt: strukt{
				Foo: "", // doesn't have omitempty, should show up in the result object
				Bar: "",
			},
		},
		{
			name: `it correctly omits fields with yaml:"-"`,
			want: `{"foo":"foo"}`,
			strukt: strukt{
				Foo: "foo",
				Baz: "this shouldn't be here",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inlineFriendlyMarshalJSON(tt.strukt)
			if err != nil {
				t.Errorf("inlineFriendlyMarshalJSON() error = %v", err)
				return
			}

			if string(got) != tt.want {
				t.Errorf("inlineFriendlyMarshalJSON() = %v, want %v", string(got), tt.want)
			}
		})
	}
}

func TestInlineFriendlyMarshalJSON_FailsWhenInlineFieldsIsntAMap(t *testing.T) {
	type test struct {
		Qux string `yaml:",inline"`
	}

	_, err := inlineFriendlyMarshalJSON(test{
		Qux: "this isn't a map",
	})

	if err == nil {
		t.Fatalf("inlineFriendlyMarshalJSON() == nil, want error")
	}

	wantError := "inline fields value of pipeline.test.Qux must be a map[string]any, was string instead"
	if err.Error() != wantError {
		t.Errorf("inlineFriendlyMarshalJSON() error = %v, want %v", err, wantError)
	}
}
