package plugin

import (
	"testing"

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

	// j, err := json.Marshal(result)
	// assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))
}
