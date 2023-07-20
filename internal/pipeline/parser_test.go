package pipeline

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: `hello "friend"`},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got, +want):\n%s", diff)
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

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: `hello ${ENV_VAR_FRIEND}`},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
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

func TestParserParsesNoSteps(t *testing.T) {
	tests := []string{
		"steps: null\n",
		"steps:\n\n",
		"steps: []\n",
	}

	for _, test := range tests {
		input := strings.NewReader(test)
		got, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(input) error = %v", err)
		}

		want := &Pipeline{
			Steps: Steps{},
		}
		if diff := cmp.Diff(got, want); diff != "" {
			t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
		}

		gotJSON, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
		}

		const wantJSON = `{
  "steps": []
}`
		if diff := cmp.Diff(string(gotJSON), wantJSON); diff != "" {
			t.Errorf("marshalled JSON diff (-got +want):\n%s", diff)
		}
	}
}

func TestParserParsesGroups(t *testing.T) {
	envMap := env.FromSlice([]string{`ENV_VAR_FRIEND="friend"`})

	input := strings.NewReader(`---
steps:
  - group:
    steps:
      - command: hello ${ENV_VAR_FRIEND}
      - wait
      - block: goodbye
  - group:
    steps: null
`)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}
	if err := got.Interpolate(envMap); err != nil {
		t.Fatalf("p.Interpolate(%v) error = %v", envMap, err)
	}

	want := &Pipeline{
		Steps: Steps{
			&GroupStep{
				Steps: Steps{
					&CommandStep{Command: `hello "friend"`},
					WaitStep{},
					InputStep{"block": "goodbye"},
				},
				RemainingFields: map[string]any{
					"group": nil,
				},
			},
			&GroupStep{
				Steps: Steps{},
				RemainingFields: map[string]any{
					"group": nil,
				},
			},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
	}

	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf(`json.MarshalIndent(got, "", "  ") error = %v`, err)
	}

	const wantJSON = `{
  "steps": [
    {
      "group": null,
      "steps": [
        {
          "command": "hello \"friend\""
        },
        "wait",
        {
          "block": "goodbye"
        }
      ]
    },
    {
      "group": null,
      "steps": []
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

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: `bye "friend"`},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
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

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: `bye "friend"`},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
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

	want := &Pipeline{
		Steps: Steps{
			WaitStep{"wait": nil, "if": "foo"},
		},
	}
	if diff := cmp.Diff(got, want); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
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
	const timestamp = "2002-08-15T17:18:23.18-06:00"
	input := strings.NewReader("steps:\n  - trigger: hello\n    llamas: " + timestamp)
	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}

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

func TestParserInterpolatesPluginConfigs(t *testing.T) {
	envMap := env.New()
	input := strings.NewReader(`env:
  ECR_PLUGIN_VER: "v2.0.0"
  ECR_ACCOUNT: "0123456789"
steps:
- label: ":docker: Docker Build"
  command: echo foo
  plugins:
  - ecr#$ECR_PLUGIN_VER:
      login: true
      account_ids: "$ECR_ACCOUNT"
`)

	got, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(input) error = %v", err)
	}
	if err := got.Interpolate(envMap); err != nil {
		t.Fatalf("p.Interpolate(%v) error = %v", envMap, err)
	}
	want := &Pipeline{
		Env: ordered.MapFromItems(
			ordered.TupleSS{Key: "ECR_PLUGIN_VER", Value: "v2.0.0"},
			ordered.TupleSS{Key: "ECR_ACCOUNT", Value: "0123456789"},
		),
		Steps: Steps{
			&CommandStep{
				Command: "echo foo",
				Plugins: Plugins{
					{
						Name: "ecr#v2.0.0",
						Config: ordered.MapFromItems(
							ordered.TupleSA{Key: "login", Value: true},
							ordered.TupleSA{Key: "account_ids", Value: "0123456789"},
						),
					},
				},
				RemainingFields: map[string]any{
					"label": string(":docker: Docker Build"),
				},
			},
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

	want := &Pipeline{
		Steps: Steps{
			&CommandStep{
				Command: "script/buildkite/xxx.sh",
				Plugins: Plugins{
					{
						Name: "xxx/aws-assume-role#v0.1.0",
						Config: ordered.MapFromItems(
							ordered.TupleSA{Key: "role", Value: "arn:aws:iam::xxx:role/xxx"},
						),
					},
					{
						Name: "ecr#v1.1.4",
						Config: ordered.MapFromItems(
							ordered.TupleSA{Key: "login", Value: true},
							ordered.TupleSA{Key: "account_ids", Value: "xxx"},
							ordered.TupleSA{Key: "registry_region", Value: "us-east-1"},
						),
					},
					{
						Name: "docker-compose#v2.5.1",
						Config: ordered.MapFromItems(
							ordered.TupleSA{Key: "run", Value: "xxx"},
							ordered.TupleSA{Key: "config", Value: ".buildkite/docker/docker-compose.yml"},
							ordered.TupleSA{Key: "env", Value: []any{
								"AWS_ACCESS_KEY_ID",
								"AWS_SECRET_ACCESS_KEY",
								"AWS_SESSION_TOKEN",
							}},
						),
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
	if diff := cmp.Diff(got, want, cmp.Comparer(ordered.EqualSA)); diff != "" {
		t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
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

	want := &Pipeline{
		Steps: Steps{
			WaitStep{
				"wait": nil,
				"if":   "build.env(\"ACCOUNT\") =~ /^(foo|bar)$/",
			},
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

			if diff := cmp.Diff(got, want); diff != "" {
				t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
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
		want     *Pipeline
		wantJSON string
	}{
		{
			desc: "without existing env",
			pipeline: `---
steps:
 - command: echo asd
`,
			want: &Pipeline{
				Env: ordered.MapFromItems(
					ordered.TupleSS{Key: "BUILDKITE_TRACE_CONTEXT", Value: "123"},
				),
				Steps: Steps{
					&CommandStep{Command: "echo asd"},
				},
			},
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
			want: &Pipeline{
				Env: ordered.MapFromItems(
					ordered.TupleSS{Key: "ASD", Value: "1"},
					ordered.TupleSS{Key: "BUILDKITE_TRACE_CONTEXT", Value: "123"},
				),
				Steps: Steps{
					&CommandStep{Command: "echo asd"},
				},
			},
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

func TestParserCommandVersusCommands(t *testing.T) {
	t.Parallel()

	want1Cmd := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: "echo foo"},
		},
	}

	want1CmdJSON := `{
  "steps": [
    {
      "command": "echo foo"
    }
  ]
}`

	want2Cmd := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: "echo foo\necho bar"},
		},
	}

	want2CmdJSON := `{
  "steps": [
    {
      "command": "echo foo\necho bar"
    }
  ]
}`

	want2CmdNewline := &Pipeline{
		Steps: Steps{
			&CommandStep{Command: "echo foo\necho bar\n"},
		},
	}

	want2CmdNewlineJSON := `{
  "steps": [
    {
      "command": "echo foo\necho bar\n"
    }
  ]
}`

	tests := []struct {
		desc     string
		input    string
		want     *Pipeline
		wantJSON string
	}{
		{
			desc: "Step with one command (scalar)",
			input: `---
steps:
  - command: echo foo
`,
			want:     want1Cmd,
			wantJSON: want1CmdJSON,
		},
		{
			desc: "Step with one command (sequence)",
			input: `---
steps:
  - command:
    - echo foo
`,
			want:     want1Cmd,
			wantJSON: want1CmdJSON,
		},
		{
			desc: "Step with two command (scalar)",
			input: `---
steps:
  - command: |
      echo foo
      echo bar
`,
			want:     want2CmdNewline,
			wantJSON: want2CmdNewlineJSON,
		},
		{
			desc: "Step with two command (sequence)",
			input: `---
steps:
  - command:
    - echo foo
    - echo bar
`,
			want:     want2Cmd,
			wantJSON: want2CmdJSON,
		},
		{
			desc: "Step with one commands (scalar)",
			input: `---
steps:
  - commands: echo foo
`,
			want:     want1Cmd,
			wantJSON: want1CmdJSON,
		},
		{
			desc: "Step with one commands (sequence)",
			input: `---
steps:
  - commands:
    - echo foo
`,
			want:     want1Cmd,
			wantJSON: want1CmdJSON,
		},
		{
			desc: "Step with two commands (scalar)",
			input: `---
steps:
  - commands: |
      echo foo
      echo bar
`,
			want:     want2CmdNewline,
			wantJSON: want2CmdNewlineJSON,
		},
		{
			desc: "Step with two commands (sequence)",
			input: `---
steps:
  - commands:
    - echo foo
    - echo bar
`,
			want:     want2Cmd,
			wantJSON: want2CmdJSON,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(strings.NewReader(test.input))
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", test.input, err)
			}
			if diff := cmp.Diff(got, test.want, cmp.Comparer(ordered.EqualSA)); err != nil {
				t.Errorf("parsed pipeline diff (-got +want):\n%s", diff)
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
