package pipeline

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/ordered"
	"github.com/google/go-cmp/cmp"
)

func TestParserParsesYAML(t *testing.T) {
	envMap := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})
	input := strings.NewReader("steps:\n  - command: \"hello ${ENV_VAR_FRIEND}\"")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}
	if err := got.Interpolate(envMap); err != nil {
		t.Fatalf("p.Interpolate(%v) error = %v", envMap, err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}

	const wantJSON = `{
  "steps": [
    {
      "command": "hello \"friend\""
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserParsesYAMLWithNoInterpolation(t *testing.T) {
	input := strings.NewReader("steps:\n  - command: \"hello ${ENV_VAR_FRIEND}\"")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}

	const wantJSON = `{
  "steps": [
    {
      "command": "hello ${ENV_VAR_FRIEND}"
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserSupportsYAMLMergesAndAnchors(t *testing.T) {
	const complexYAML = `---
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

	input := strings.NewReader(complexYAML)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}

	const wantJSON = `{
  "base_step": {
    "agent_query_rules": [
      "queue=default"
    ],
    "type": "script"
  },
  "steps": [
    {
      "agent_query_rules": [
        "queue=default"
      ],
      "agents": {
        "queue": "default"
      },
      "command": "docker build .",
      "name": ":docker: building image",
      "type": "script"
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserReturnsYAMLParsingErrors(t *testing.T) {
	input := strings.NewReader("steps: %blah%")
	_, err := Parse(input)

	// TODO: avoid testing for specific error strings
	got, want := err.Error(), `found character that cannot start any token`
	if got != want {
		t.Errorf("Parse(input) error = %q, want %q", got, want)
	}
}

func TestParserReturnsJSONParsingErrors(t *testing.T) {
	input := strings.NewReader("{")
	_, err := Parse(input)

	// TODO: avoid testing for specific error strings
	got, want := err.Error(), `line 1: did not find expected node content`
	if got != want {
		t.Errorf("Parse(input) error = %q, want %q", got, want)
	}
}

func TestParserParsesJSON(t *testing.T) {
	envMap := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})
	input := strings.NewReader("\n\n     \n  { \"steps\": [{\"command\" : \"bye ${ENV_VAR_FRIEND}\"  } ] }\n")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}
	if err := got.Interpolate(envMap); err != nil {
		t.Fatalf("p.Interpolate(%v) error = %v", envMap, err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}

	const wantJSON = `{
  "steps": [
    {
      "command": "bye \"friend\""
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserParsesJSONArrays(t *testing.T) {
	envMap := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})
	input := strings.NewReader("\n\n     \n  [ { \"command\": \"bye ${ENV_VAR_FRIEND}\" } ]\n")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}
	if err := got.Interpolate(envMap); err != nil {
		t.Fatalf("p.Interpolate(%v) error = %v", envMap, err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}
	const wantJSON = `{
  "steps": [
    {
      "command": "bye \"friend\""
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserParsesTopLevelSteps(t *testing.T) {
	input := strings.NewReader("---\n- name: Build\n  command: echo hello world\n- wait\n")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}
	const wantJSON = `{
  "steps": [
    {
      "command": "echo hello world",
      "name": "Build"
    },
    "wait"
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesBools(t *testing.T) {
	input := strings.NewReader("steps:\n  - trigger: hello\n    async: true")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}
	const wantJSON = `{
  "steps": [
    {
      "async": true,
      "trigger": "hello"
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesInts(t *testing.T) {
	input := strings.NewReader("steps:\n  - command: hello\n    parallelism: 10")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}
	const wantJSON = `{
  "steps": [
    {
      "command": "hello",
      "parallelism": 10
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesNull(t *testing.T) {
	input := strings.NewReader("steps:\n  - wait: ~\n    if: foo")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}
	const wantJSON = `{
  "steps": [
    {
      "if": "foo",
      "wait": null
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesFloats(t *testing.T) {
	input := strings.NewReader("steps:\n  - trigger: hello\n    llamas: 3.142")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}
	const wantJSON = `{
  "steps": [
    {
      "llamas": 3.142,
      "trigger": "hello"
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserHandlesDates(t *testing.T) {
	input := strings.NewReader("steps:\n  - trigger: hello\n    llamas: 2002-08-15T17:18:23.18-06:00")
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}
	const wantJSON = `{
  "steps": [
    {
      "llamas": "2002-08-15T17:18:23.18-06:00",
      "trigger": "hello"
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserInterpolatesKeysAsWellAsValues(t *testing.T) {
	envMap := env.FromSlice([]string{"FROM_ENV=llamas"})
	input := strings.NewReader(`{
	"env": {
		"${FROM_ENV}TEST1": "MyTest",
		"TEST2": "${FROM_ENV}"
	},
	"steps": ["wait"]
}`)

	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}
	if err := got.Interpolate(envMap); err != nil {
		t.Fatalf("p.Interpolate(%v) error = %v", envMap, err)
	}
	want := &Pipeline{
		Env: ordered.MapFromItems(
			ordered.TupleSS{Key: "llamasTEST1", Value: "MyTest"},
			ordered.TupleSS{Key: "TEST2", Value: "llamas"},
		),
		Steps: Steps{
			WaitStep{},
		},
	}
	if diff := cmp.Diff(got, want, cmp.Comparer(ordered.EqualSS), cmp.Comparer(ordered.EqualSA)); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserLoadsGlobalEnvBlockFirst(t *testing.T) {
	envMap := env.FromSlice([]string{"YEAR_FROM_SHELL=1912"})
	input := strings.NewReader(`
{
	"env": {
		"TEAM1": "England",
		"TEAM2": "Australia",
		"HEADLINE": "${TEAM1} smashes ${TEAM2} to win the ashes in ${YEAR_FROM_SHELL}!!"
	},
	"steps": [{
		"command": "echo ${HEADLINE}"
	}]
}`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}
	if err := got.Interpolate(envMap); err != nil {
		t.Fatalf("p.Interpolate(%v) error = %v", envMap, err)
	}
	want := &Pipeline{
		Env: ordered.MapFromItems(
			ordered.TupleSS{Key: "TEAM1", Value: "England"},
			ordered.TupleSS{Key: "TEAM2", Value: "Australia"},
			ordered.TupleSS{Key: "HEADLINE", Value: "England smashes Australia to win the ashes in 1912!!"},
		),
		Steps: Steps{
			&CommandStep{
				Command: "echo England smashes Australia to win the ashes in 1912!!",
			},
		},
	}
	if diff := cmp.Diff(got, want, cmp.Comparer(ordered.EqualSS), cmp.Comparer(ordered.EqualSA)); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}
}

func TestParserPreservesOrderOfPlugins(t *testing.T) {
	input := strings.NewReader(`---
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
      queue: xxx`)

	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}
	const wantJSON = `{
  "steps": [
    {
      "agents": {
        "queue": "xxx"
      },
      "command": "script/buildkite/xxx.sh",
      "name": ":s3: xxx",
      "plugins": [
        {
          "xxx/aws-assume-role#v0.1.0": {
            "role": "arn:aws:iam::xxx:role/xxx"
          }
        },
        {
          "ecr#v1.1.4": {
            "login": true,
            "account_ids": "xxx",
            "registry_region": "us-east-1"
          }
        },
        {
          "docker-compose#v2.5.1": {
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
    }
  ]
}`
	if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
		t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
	}
}

func TestParserParsesConditionalWithEndOfLineAnchorDollarSign(t *testing.T) {
	tests := []struct {
		desc        string
		interpolate bool
		pipeline    string
	}{
		{
			desc:        "with interpolation",
			interpolate: true,
			// dollar sign must be escaped when interpolation is in effect
			pipeline: `---
steps:
 - wait: ~
   if: build.env("ACCOUNT") =~ /^(foo|bar)\$/
`,
		},
		{
			desc:        "without interpolation",
			interpolate: false,
			pipeline: `---
steps:
 - wait: ~
   if: build.env("ACCOUNT") =~ /^(foo|bar)$/
`,
		},
	}

	const wantJSON = `{
  "steps": [
    {
      "if": "build.env(\"ACCOUNT\") =~ /^(foo|bar)$/",
      "wait": null
    }
  ]
}`

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			input := strings.NewReader(test.pipeline)
			got, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse(input) error = %v", err)
			}
			if test.interpolate {
				if err := got.Interpolate(nil); err != nil {
					t.Fatalf("p.Interpolate(nil) error = %v", err)
				}
			}

			gotJSON, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
			}
			if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
				t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
			}
		})
	}
}

func TestPipelinePropagatesTracingDataIfAvailable(t *testing.T) {
	tests := []struct {
		desc     string
		pipeline string
		wantJSON string
	}{
		{
			desc: "without existing env",
			pipeline: `---
steps:
 - command: echo asd
`,
			wantJSON: `{
  "env": {
    "BUILDKITE_TRACE_CONTEXT": "123"
  },
  "steps": [
    {
      "command": "echo asd"
    }
  ]
}`,
		},
		{
			desc: "with existing env",
			pipeline: `---
env:
  ASD: 1
steps:
 - command: echo asd
`,
			wantJSON: `{
  "env": {
    "ASD": "1",
    "BUILDKITE_TRACE_CONTEXT": "123"
  },
  "steps": [
    {
      "command": "echo asd"
    }
  ]
}`,
		},
	}

	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			input := strings.NewReader(test.pipeline)
			e := env.New()
			e.Set("BUILDKITE_TRACE_CONTEXT", "123")
			got, err := Parse(input)
			if err != nil {
				t.Fatalf("Parse(input) error = %v", err)
			}
			if err := got.Interpolate(e); err != nil {
				t.Fatalf("p.Interpolate(%v) error = %v", e, err)
			}

			gotJSON, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
			}
			if diff := cmp.Diff(string(gotJSON), test.wantJSON); diff != "" {
				t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
			}
		})
	}
}
