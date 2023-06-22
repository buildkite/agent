package pipeline

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gopkg.in/yaml.v3"
)

func TestPipelineUnmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		input string
		want  *Pipeline
	}{
		{
			desc: "Legacy pipeline",
			input: `---
- command: "echo llama"
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{Command: "echo llama"},
				},
			},
		},
		{
			desc: "Legacy pipeline, no dashes",
			input: `- command: "echo llama"
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{Command: "echo llama"},
				},
			},
		},
		{
			desc: "Basic pipeline",
			input: `---
steps:
  - command: "echo llama"
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{Command: "echo llama"},
				},
			},
		},
		{
			desc: "Slightly less basic pipeline",
			input: `---
steps:
  - command: "echo llama"
  - wait
  - command: "echo was here"
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{Command: "echo llama"},
					WaitStep{},
					&CommandStep{Command: "echo was here"},
				},
			},
		},
		{
			desc: "Commands normalised to Command",
			input: `---
steps:
  - commands:
    - "echo llama"
    - "echo was here"
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{Command: "echo llama\necho was here"},
				},
			},
		},
		{
			desc: "Steps and env",
			input: `---
env:
  LLAMA: Kuzco
steps:
  - command: "echo llama"
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{Command: "echo llama"},
				},
				Env: map[string]string{"LLAMA": "Kuzco"},
			},
		},
		{
			desc: "Step with non-normalised plugins",
			input: `---
steps:
  - command: "echo llama"
    plugins:
      new-groove#v1.0.0:
        llama: Kuzco
        villain: Yzma
      docker-compose#v3.0.0:
        config: .buildkite/docker-compose.yml
        run: agent
      library-example#v1.0.0: ~
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{
						Command: "echo llama",
						Plugins: Plugins{
							{
								Name: "new-groove#v1.0.0",
								Config: map[string]any{
									"llama":   "Kuzco",
									"villain": "Yzma",
								},
							},
							{
								Name: "docker-compose#v3.0.0",
								Config: map[string]any{
									"config": ".buildkite/docker-compose.yml",
									"run":    "agent",
								},
							},
							{
								Name: "library-example#v1.0.0",
							},
						},
					},
				},
			},
		},
		{
			desc: "Step with normalised plugins",
			input: `---
steps:
  - command: "echo llama"
    plugins:
      - new-groove#v1.0.0:
          llama: Kuzco
          villain: Yzma
      - docker-compose#v3.0.0:
          config: .buildkite/docker-compose.yml
          run: agent
      - library-example#v1.0.0: ~
`,
			want: &Pipeline{
				Steps: Steps{
					&CommandStep{
						Command: "echo llama",
						Plugins: Plugins{
							{
								Name: "new-groove#v1.0.0",
								Config: map[string]any{
									"llama":   "Kuzco",
									"villain": "Yzma",
								},
							},
							{
								Name: "docker-compose#v3.0.0",
								Config: map[string]any{
									"config": ".buildkite/docker-compose.yml",
									"run":    "agent",
								},
							},
							{
								Name: "library-example#v1.0.0",
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			got := new(Pipeline)
			if err := yaml.Unmarshal([]byte(test.input), got); err != nil {
				t.Fatalf("yaml.Unmarshal(%q, got) = %v", test.input, err)
			}

			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("Unmarshalled Pipeline diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestPipelineUnmarshalErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		desc  string
		input string
		want  error
	}{
		{
			desc: "Pipeline has no steps",
			input: `---
env:
  LLAMA: Kuzco
`,
			want: ErrNoSteps,
		},
		{},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			if err := yaml.Unmarshal([]byte(test.input), new(Pipeline)); !errors.Is(err, test.want) {
				t.Fatalf("yaml.Unmarshal(%q, new(Pipeline)) = %v, want %v", test.input, err, test.want)
			}
		})
	}
}
