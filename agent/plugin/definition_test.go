package plugin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/qri-io/jsonschema"
)

const testPluginDef = `
name: test-plugin
description: A test plugin
author: https://github.com/buildkite
requirements:
  - docker
  - docker-compose
configuration:
  properties:
    run:
      type: string
    build:
      type: [ string, array ]
      minimum: 1
  oneOf:
    - required:
      - run
    - required:
      - build
  additionalProperties: false
`

func TestDefinitionParsesYaml(t *testing.T) {
	def, err := ParseDefinition([]byte(testPluginDef))
	if err != nil {
		t.Fatalf("ParseDefinition(testPluginDef) error = %v", err)
	}
	if got, want := def.Name, "test-plugin"; got != want {
		t.Errorf("def.Name = %q, want %q", got, want)
	}
	if got, want := def.Requirements, []string{"docker", "docker-compose"}; !cmp.Equal(got, want) {
		t.Errorf("def.Requirements = %q, want %q", got, want)
	}
}

func TestDefinitionValidationFailsIfDependenciesNotMet(t *testing.T) {
	validator := &Validator{
		commandExists: func(cmd string) bool {
			return false
		},
	}

	def := &Definition{
		Requirements: []string{"llamas"},
	}

	res := validator.Validate(context.Background(), def, nil)

	if res.Valid() {
		t.Errorf("validator.Validate(def, nil).Valid() = true, want false")
	}
	if got, want := len(res.errors), 1; got != want {
		t.Errorf("len(validator.Validate(def, nil).Errors) = %d, want %d", got, want)
	}
	if got, want := res.errors[0], ErrCommandNotInPATH; !errors.Is(got, want) {
		t.Errorf("validator.Validate(def, nil).Errors[0] = %v, want %v", got, want)
	}
}

func TestDefinitionValidatesConfiguration(t *testing.T) {
	validator := &Validator{
		commandExists: func(cmd string) bool {
			return false
		},
	}

	def := &Definition{
		Configuration: jsonschema.Must(`{
			"type": "object",
			"properties": {
				"llamas": {
					"type": "string"
				},
				"alpacas": {
					"type": "string"
				}
			},
			"required": ["llamas", "alpacas"]
		}`),
	}

	cfg := map[string]any{
		"llamas": "always",
	}
	res := validator.Validate(context.Background(), def, cfg)

	if res.Valid() {
		t.Errorf("validator.Validate(def, % #v).Valid() = true, want false", cfg)
	}
	// TODO: Testing error strings is fragile - replace with a more semantic test.
	if got, want := res.Error(), `/: {"llamas":"always"} "alpacas" value is required`; got != want {
		t.Errorf("validator.Validate(def, % #v).Error() = %q, want %q", cfg, got, want)
	}
}

func TestDefinitionWithoutAdditionalProperties(t *testing.T) {
	validator := &Validator{
		commandExists: func(cmd string) bool {
			return false
		},
	}

	def := &Definition{
		Configuration: jsonschema.Must(`{
			"type": "object",
			"properties": {
				"alpacas": {
					"type": "string"
				}
			},
			"required": ["alpacas"],
			"additionalProperties": false
		}`),
	}

	cfg := map[string]any{
		"alpacas": "definitely",
		"camels":  "never",
	}
	res := validator.Validate(context.Background(), def, cfg)

	if res.Valid() {
		t.Errorf("validator.Validate(def, % #v).Valid() = true, want false", cfg)
	}
	// TODO: Testing error strings is fragile - replace with a more semantic test.
	if got, wantSuffix := res.Error(), "additional properties are not allowed"; !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("validator.Validate(def, % #v).Error() = %q, want suffix %q", cfg, got, wantSuffix)
	}
}

func TestDefinitionWithAdditionalProperties(t *testing.T) {
	validator := &Validator{
		commandExists: func(cmd string) bool {
			return false
		},
	}

	def := &Definition{
		Configuration: jsonschema.Must(`{
			"type": "object",
			"properties": {
				"alpacas": {
					"type": "string"
				}
			},
			"required": ["alpacas"],
			"additionalProperties": true
		}`),
	}

	cfg := map[string]any{
		"alpacas": "definitely",
		"camels":  "never",
	}
	res := validator.Validate(context.Background(), def, cfg)

	if !res.Valid() {
		t.Errorf("validator.Validate(def, % #v).Valid() = false, want true", cfg)
	}
}
