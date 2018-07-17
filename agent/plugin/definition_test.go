package plugin

import (
	"testing"

	"github.com/qri-io/jsonschema"
	"github.com/stretchr/testify/assert"
)

var testPluginDef = `
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

	assert.NoError(t, err)
	assert.Equal(t, def.Name, `test-plugin`)
	assert.Equal(t, def.Requirements, []string{`docker`, `docker-compose`})
}

func TestDefinitionValidationFailsIfDependenciesNotMet(t *testing.T) {
	validator := &Validator{
		commandExists: func(cmd string) bool {
			return false
		},
	}

	def := &Definition{
		Requirements: []string{`llamas`},
	}

	res := validator.Validate(def, nil)

	assert.False(t, res.Valid())
	assert.Equal(t, res.Errors, []string{
		`Required command "llamas" isn't in PATH`,
	})
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

	res := validator.Validate(def, map[string]interface{}{
		"llamas": "always",
	})

	assert.False(t, res.Valid())
	assert.Equal(t, res.Errors, []string{
		`/: {"llamas":"always"} "alpacas" value is required`,
	})
}
