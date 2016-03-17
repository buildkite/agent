package agent

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func parse(s string) (string, error) {
	parsed, err := PipelineParser{Data: []byte(s)}.Parse()

	return string(parsed[:]), err
}

func TestPipelineParser(t *testing.T) {
	var result string
	var err error

	// It does nothing to byte slices with no environmnet variables
	result, err = parse("foo")
	assert.Nil(t, err)
	assert.Equal(t, result, "foo")

	// It does nothing to empty strings
	result, err = parse("")
	assert.Nil(t, err)
	assert.Equal(t, result, "")

	// It parses regular env vars
	os.Setenv("WHO", "World!")
	result, err = parse(`
	  steps:
	    - command: "Hello $WHO"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "Hello World!"
	`)

	// It inserts a blank string if the var hasn't been set
	result, err = parse(`
	  steps:
	    - command: "Hello $WHO_REALLY"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "Hello "
	`)

	// It returns an error with an invalid looking env var
	result, err = parse(`
	  steps:
	    - command: "Hello $123"
	`)
	assert.NotNil(t, err)
	assert.Equal(t, string(err.Error()), "Invalid environment variable `$123` - they can only start with a letter")

	// They can be embedded in strings and keys
	os.Setenv("KEY", "command")
	os.Setenv("END_OF_HELLO", "llo")
	result, err = parse(`
	  steps:
	    - $KEY: "He$END_OF_HELLO"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "Hello"
	`)

	// The parser supports the other type of env variable
	os.Setenv("TODAY", "Sunday")
	os.Setenv("TOMORROW", "Monday")
	result, err = parse(`
	  steps:
	    - command: "echo 'Today is ${TODAY}'"
	    - command: "echo 'Tomorrow is ${TOMORROW}'"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "echo 'Today is Sunday'"
	    - command: "echo 'Tomorrow is Monday'"
	`)

	// You can provide default values
	os.Unsetenv("TODAY")
	os.Unsetenv("TOMORROW")
	result, err = parse(`
	  steps:
	    - command: "echo 'Today is ${TODAY-Tuesday}'"
	    - command: "echo 'Tomorrow is ${TOMORROW-Wednesday}'"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "echo 'Today is Tuesday'"
	    - command: "echo 'Tomorrow is Wednesday'"
	`)

	// You can toggle the defaulting behaviour between "use default if
	// value is null" or "use default if value is unset"
	os.Setenv("THIS_VAR_IS_NULL", "")
	result, err = parse(`
	  steps:
            - command: "Do this ${THIS_VAR_IS_NULL:-great thing}"
	    - command: "Do this ${THIS_VAR_IS_NULL-wont show up}"
	    - command: "Don't do this ${THIS_VAR_DOESNT_EXIST:-please}"
	    - command: "Don't do this ${THIS_VAR_DOESNT_EXIST-please (this will show)}"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
            - command: "Do this great thing"
	    - command: "Do this "
	    - command: "Don't do this please"
	    - command: "Don't do this please (this will show)"
	`)

	// It allows you to require some variables
	result, err = parse(`
	  steps:
	    - command: "Hello ${REQUIRED_VAR?}"
	`)
	assert.NotNil(t, err)
	assert.Equal(t, string(err.Error()), "$REQUIRED_VAR: not set")

	// The error message for them can be customized
	result, err = parse(`
	  steps:
	    - command: "Hello ${REQUIRED_VAR?y u no set me? :-{}"
	`)
	assert.NotNil(t, err)
	assert.Equal(t, string(err.Error()), "$REQUIRED_VAR: y u no set me? :-{")

	// Lets you escape variables using 2 different syntaxes
	result, err = parse(`
	  steps:
            - command: "Do this $$ESCAPE_PARTY"
            - command: "Do this \$ESCAPE_PARTY"
            - command: "Do this $${SUCH_ESCAPE}"
            - command: "Do this \${SUCH_ESCAPE}"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
            - command: "Do this $$ESCAPE_PARTY"
            - command: "Do this \$ESCAPE_PARTY"
            - command: "Do this $${SUCH_ESCAPE}"
            - command: "Do this \${SUCH_ESCAPE}"
	`)

	// Lets you use special characters in the default env var option
	result, err = parse(`
	  steps:
	    - command: "${THIS_VAR_IS_NULL:--:{}}"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "-:{}"
	`)

	// Lets you use special characters in the required var option
	result, err = parse(`
	  steps:
	    - command: "Hello ${REQUIRED_VAR?{}}"
	`)
	assert.NotNil(t, err)
	assert.Equal(t, string(err.Error()), "$REQUIRED_VAR: {}")
}
