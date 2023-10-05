package pipeline

import (
	"testing"

	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/google/go-cmp/cmp"
)

func TestCommandStepUnmarshalJSON(t *testing.T) {
	// AcceptJob returns a Step that looks like this (but without the
	// indentation):
	input := []byte(`{
  "command": "script/buildkite/xxx.sh",
  "plugins": [
    {
      "github.com/xxx/aws-assume-role-buildkite-plugin#v0.1.0": {
        "role": "arn:aws:iam::xxx:role/xxx"
      }
    },
    {
      "github.com/buildkite-plugins/ecr-buildkite-plugin#v1.1.4": {
        "login": true,
        "account_ids": "xxx",
        "registry_region": "us-east-1"
      }
    },
    {
      "github.com/buildkite-plugins/docker-compose-buildkite-plugin#v2.5.1": {
        "run": "xxx",
        "config": ".buildkite/docker/docker-compose.yml",
        "env": [
          "AWS_ACCESS_KEY_ID",
          "AWS_SECRET_ACCESS_KEY",
          "AWS_SESSION_TOKEN"
        ]
      }
    }
  ]
}`)

	want := &CommandStep{
		Command: "script/buildkite/xxx.sh",
		Plugins: Plugins{
			{
				Source: "github.com/xxx/aws-assume-role-buildkite-plugin#v0.1.0",
				Config: map[string]any{"role": "arn:aws:iam::xxx:role/xxx"},
			},
			{
				Source: "github.com/buildkite-plugins/ecr-buildkite-plugin#v1.1.4",
				Config: map[string]any{
					"login":           true,
					"account_ids":     "xxx",
					"registry_region": "us-east-1",
				},
			},
			{
				Source: "github.com/buildkite-plugins/docker-compose-buildkite-plugin#v2.5.1",
				Config: map[string]any{
					"run":    "xxx",
					"config": ".buildkite/docker/docker-compose.yml",
					"env": []any{
						"AWS_ACCESS_KEY_ID",
						"AWS_SECRET_ACCESS_KEY",
						"AWS_SESSION_TOKEN",
					},
				},
			},
		},
	}

	got := new(CommandStep)
	if err := got.UnmarshalJSON(input); err != nil {
		t.Fatalf("CommandStep.UnmarshalJSON(input) = %v", err)
	}

	if diff := cmp.Diff(got, want, cmp.Comparer(ordered.EqualSA)); diff != "" {
		t.Errorf("CommandStep diff after UnmarshalJSON (-got +want):\n%s", diff)
	}
}

func TestStepCommandMatrixInterpolate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ms         MatrixPermutation
		step, want *CommandStep
	}{
		{
			name: "it does nothing when there's no matrix stuff",
			step: &CommandStep{
				Command: "script/buildkite/xxx.sh",
				Plugins: Plugins{
					{
						Source: "docker#v1.2.3",
						Config: map[string]any{
							"image": "alpine",
						},
					},
				},
			},
			want: &CommandStep{
				Command: "script/buildkite/xxx.sh",
				Plugins: Plugins{
					{
						Source: "docker#v1.2.3",
						Config: map[string]any{
							"image": "alpine",
						},
					},
				},
			},
		},
		{
			name: "it interplates environment variable names and values",
			ms: MatrixPermutation{
				{Dimension: "name", Value: "Taylor Launtner"},
				{Dimension: "demonym_suffix", Value: "DER"},
				{Dimension: "value", Value: "true"},
			},
			step: &CommandStep{
				Command: "script/buildkite/xxx.sh",
				Env: map[string]string{
					"NAME":                              "{{matrix.name}}",
					"MICHIGAN{{matrix.demonym_suffix}}": "{{matrix.value}}",
				},
			},
			want: &CommandStep{
				Command: "script/buildkite/xxx.sh",
				Env: map[string]string{
					"NAME":        "Taylor Launtner",
					"MICHIGANDER": "true",
				},
			},
		},
		{
			name: "it interpolates plugin config",
			ms: MatrixPermutation{
				{Dimension: "docker_version", Value: "4.5.6"},
				{Dimension: "image", Value: "alpine"},
			},
			step: &CommandStep{
				Command: "script/buildkite/xxx.sh",
				Plugins: Plugins{
					{
						Source: "docker#{{matrix.docker_version}}",
						Config: map[string]any{
							"image": "{{matrix.image}}",
						},
					},
				},
			},
			want: &CommandStep{
				Command: "script/buildkite/xxx.sh",
				Plugins: Plugins{
					{
						Source: "docker#4.5.6",
						Config: map[string]any{
							"image": "alpine",
						},
					},
				},
			},
		},
		{
			name: "it interpolates commands",
			ms: MatrixPermutation{
				{Dimension: "goos", Value: "linux"},
				{Dimension: "goarch", Value: "amd64"},
			},
			step: &CommandStep{Command: "GOOS={{matrix.goos}} GOARCH={{matrix.goarch}} go build -o foobar ."},
			want: &CommandStep{Command: "GOOS=linux GOARCH=amd64 go build -o foobar ."},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tc.step.MatrixInterpolate(tc.ms)
			if diff := cmp.Diff(tc.step, tc.want, cmp.Comparer(ordered.EqualSA)); diff != "" {
				t.Errorf("CommandStep diff after MatrixInterpolate (-got +want):\n%s", diff)
			}
		})
	}
}
