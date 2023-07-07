package ordered

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestStringsUnmarshal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		desc  string
		input string
		want  Strings
	}{
		{
			desc:  "Two items in a sequence",
			input: "- foo\n- bar",
			want:  Strings{"foo", "bar"},
		},
		{
			desc:  "One item in a sequence",
			input: `- foo`,
			want:  Strings{"foo"},
		},
		{
			desc:  "One scalar",
			input: `"foo"`,
			want:  Strings{"foo"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			var got Strings
			if err := yaml.Unmarshal([]byte(test.input), &got); err != nil {
				t.Errorf("yaml.Unmarshal(%q, &got) = %v", test.input, err)
			}
			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("Strings unmarshal diff (-got +want):\n%s", diff)
			}
		})
	}
}
