package pipeline

import (
	"fmt"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/env"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
)

func TestParserParsesYAML(t *testing.T) {
	parser := Parser{
		Env:            env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`}),
		Filename:       "awesome.yml",
		PipelineSource: []byte("steps:\n  - command: \"hello ${ENV_VAR_FRIEND}\""),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: `hello "friend"`},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserParsesYAMLWithNoInterpolation(t *testing.T) {
	parser := Parser{
		Filename:        "awesome.yml",
		PipelineSource:  []byte("steps:\n  - command: \"hello ${ENV_VAR_FRIEND}\""),
		NoInterpolation: true,
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: `hello ${ENV_VAR_FRIEND}`},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserSupportsYAMLMergesAndAnchors(t *testing.T) {
	complexYAML := `---
base_step: &base_step
  type: script
  agent_query_rules:
    - queue=default

steps:
  - <<: *base_step
    name: ':docker: building image'
    command: docker build .
    agents:
      queue: default`

	parser := Parser{
		Filename:       "awesome.yml",
		PipelineSource: []byte(complexYAML),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{
				Command: "docker build .",
				RemainingFields: map[string]any{
					"agents": map[string]any{
						"queue": "default",
					},
					"name":              ":docker: building image",
					"type":              "script",
					"agent_query_rules": []any{"queue=default"},
				},
			},
		},
		RemainingFields: map[string]any{
			"base_step": map[string]any{
				"type":              "script",
				"agent_query_rules": []any{"queue=default"},
			},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserReturnsYAMLParsingErrors(t *testing.T) {
	parser := Parser{
		Filename:       "awesome.yml",
		PipelineSource: []byte("steps: %blah%"),
	}
	_, err := parser.Parse()

	assert.Error(t, err, `Failed to parse awesome.yml: found character that cannot start any token`, fmt.Sprintf("%s", err))
}

func TestParserReturnsJSONParsingErrors(t *testing.T) {
	parser := Parser{
		Filename:       "awesome.json",
		PipelineSource: []byte("{"),
	}
	_, err := parser.Parse()

	assert.Error(t, err, `Failed to parse awesome.json: line 1: did not find expected node content`, fmt.Sprintf("%s", err))
}

func TestParserParsesJSON(t *testing.T) {
	parser := Parser{
		Env:            env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`}),
		Filename:       "thing.json",
		PipelineSource: []byte("\n\n     \n  { \"steps\": [{\"command\" : \"bye ${ENV_VAR_FRIEND}\"  } ] }\n"),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: `bye "friend"`},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserParsesJSONArrays(t *testing.T) {
	parser := Parser{
		Env:            env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`}),
		PipelineSource: []byte("\n\n     \n  [ { \"command\": \"bye ${ENV_VAR_FRIEND}\" } ]\n"),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: `bye "friend"`},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserParsesTopLevelSteps(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("---\n- name: Build\n  command: echo hello world\n- wait\n"),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{
				Command: "echo hello world",
				RemainingFields: map[string]any{
					"name": "Build",
				},
			},
			WaitStep{},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesBools(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - trigger: hello\n    async: true"),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			TriggerStep{
				"trigger": "hello",
				"async":   true,
			},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesInts(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - command: hello\n    parallelism: 10"),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{
				Command: "hello",
				RemainingFields: map[string]any{
					"parallelism": 10,
				},
			},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesNull(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - wait: ~"),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			WaitStep{"wait": nil},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesFloats(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - trigger: hello\n    llamas: 3.142"),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			TriggerStep{
				"trigger": "hello",
				"llamas":  3.142,
			},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserHandlesDates(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte("steps:\n  - trigger: hello\n    llamas: 2002-08-15T17:18:23.18-06:00"),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	timestamp := "2002-08-15T17:18:23.18-06:00"
	llamatime, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatalf("time.Parse(time.RFC3339, %q) errorr = %v", timestamp, err)
	}

	want := &Pipeline{
		Steps: Steps{
			TriggerStep{
				"trigger": "hello",
				"llamas":  llamatime,
			},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserInterpolatesKeysAsWellAsValues(t *testing.T) {
	parser := Parser{
		Env: env.FromSlice([]string{"FROM_ENV=llamas"}),
		PipelineSource: []byte(`{
	"env": {
		"${FROM_ENV}TEST1": "MyTest",
		"TEST2": "${FROM_ENV}"
	},
	"steps": ["wait"]
}`),
	}

	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{WaitStep{}},
		Env: map[string]string{
			"llamasTEST1": "MyTest",
			"TEST2":       "llamas",
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserLoadsGlobalEnvBlockFirst(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte(`
{
	"env": {
		"TEAM1": "England",
		"TEAM2": "Australia",
		"HEADLINE": "${TEAM1} smashes ${TEAM2} to win the ashes in ${YEAR_FROM_SHELL}!!"
	},
	"steps": [{
		"command": "echo ${HEADLINE}"
	}]
}`),
		Env: env.FromSlice([]string{"YEAR_FROM_SHELL=1912"}),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{
				Command: "echo England smashes Australia to win the ashes in 1912!!",
			},
		},
		Env: map[string]string{
			"TEAM1":    "England",
			"TEAM2":    "Australia",
			"HEADLINE": "England smashes Australia to win the ashes in 1912!!",
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserMultipleInterpolation(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte(`
{
	"env": {
		"TEAM1": "England",
		"TEAM2": "Australia",
		"HEADLINE": "${TEAM1} vs ${TEAM2}: ${TEAM1} wins!!"
	},
	"steps": [{
		"command": "echo ${HEADLINE}"
	}]
}`),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{
				Command: "echo England vs Australia: England wins!!",
			},
		},
		Env: map[string]string{
			"TEAM1":    "England",
			"TEAM2":    "Australia",
			"HEADLINE": "England vs Australia: England wins!!",
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserEnvInterpolationCycle(t *testing.T) {
	parser := Parser{
		Env: env.FromSlice([]string{"PRESENTING=Presenting", "THREE_STOOGES=Three Stooges"}),
		PipelineSource: []byte(`
{
	"env": {
		"LARRY": "${CURLY}",
		"CURLY": "${MOE}",
		"MOE": "${LARRY}"
	},
	"steps": [{
		"command": "echo ${PRESENTING} the ${THREE_STOOGES}: ${LARRY}, ${CURLY}, and ${MOE}"
	}]
}`),
	}
	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{
				Command: "echo Presenting the Three Stooges: , , and ",
			},
		},
		Env: map[string]string{
			"LARRY": "",
			"CURLY": "",
			"MOE":   "",
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesOrderOfPlugins(t *testing.T) {
	parser := Parser{
		PipelineSource: []byte(`---
steps:
  - name: ":s3: xxx"
    command: "script/buildkite/xxx.sh"
    plugins:
      xxx/aws-assume-role#v0.1.0:
        role: arn:aws:iam::xxx:role/xxx
      ecr#v1.1.4:
        login: true
        account_ids: xxx
        registry_region: us-east-1
      docker-compose#v2.5.1:
        run: xxx
        config: .buildkite/docker/docker-compose.yml
        env:
          - AWS_ACCESS_KEY_ID
          - AWS_SECRET_ACCESS_KEY
          - AWS_SESSION_TOKEN
    agents:
      queue: xxx`),
	}

	got, err := parser.Parse()
	if err != nil {
		t.Fatalf("parser.Parse() error = %v", err)
	}

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{
				Command: "script/buildkite/xxx.sh",
				Plugins: Plugins{
					{
						Name: "xxx/aws-assume-role#v0.1.0",
						Config: map[string]any{
							"role": "arn:aws:iam::xxx:role/xxx",
						},
					},
					{
						Name: "ecr#v1.1.4",
						Config: map[string]any{
							"login":           true,
							"account_ids":     "xxx",
							"registry_region": "us-east-1",
						},
					},
					{
						Name: "docker-compose#v2.5.1",
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
				RemainingFields: map[string]any{
					"name": ":s3: xxx",
					"agents": map[string]any{
						"queue": "xxx",
					},
				},
			},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserParsesConditionalWithEndOfLineAnchorDollarSign(t *testing.T) {
	tests := []struct {
		desc          string
		interpolation bool
		pipeline      string
	}{
		{
			desc:          "with interpolation",
			interpolation: true,
			// dollar sign must be escaped when interpolation is in effect
			pipeline: `---
steps:
 - wait: ~
   if: build.env("ACCOUNT") =~ /^(foo|bar)\$/
`,
		},
		{
			desc:          "without interpolation",
			interpolation: false,
			pipeline: `---
steps:
 - wait: ~
   if: build.env("ACCOUNT") =~ /^(foo|bar)$/
`,
		},
	}

	want := &Pipeline{
		Steps: Steps{
			WaitStep{
				"wait": nil,
				"if":   "build.env(\"ACCOUNT\") =~ /^(foo|bar)$/",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			parser := Parser{
				PipelineSource:  []byte(test.pipeline),
				NoInterpolation: !test.interpolation,
			}
			got, err := parser.Parse()
			if err != nil {
				t.Fatalf("parser.Parse() error = %v", err)
			}

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestPipelinePropagatesTracingDataIfAvailable(t *testing.T) {
	e := env.New()
	e.Set("BUILDKITE_TRACE_CONTEXT", "123")

	tests := []struct {
		desc     string
		pipeline string
		want     *Pipeline
	}{
		{
			desc: "without existing env",
			pipeline: `---
steps:
 - command: echo asd
`,
			want: &Pipeline{
				Env: map[string]string{
					"BUILDKITE_TRACE_CONTEXT": "123",
				},
				Steps: Steps{
					&CommandStep{
						Command: "echo asd",
					},
				},
			},
		},
		{
			desc: "with existing env",
			pipeline: `---
env:
  ASD: 1
steps:
 - command: echo asd
`,
			want: &Pipeline{
				Env: map[string]string{
					"ASD":                     "1",
					"BUILDKITE_TRACE_CONTEXT": "123",
				},
				Steps: Steps{
					&CommandStep{
						Command: "echo asd",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			parser := Parser{
				PipelineSource: []byte(test.pipeline),
				Env:            e,
			}
			got, err := parser.Parse()
			if err != nil {
				t.Fatalf("parser.Parse() error = %v", err)
			}

			if diff := cmp.Diff(got, test.want); diff != "" {
				t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
			}
		})
	}
}
