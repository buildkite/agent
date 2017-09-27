package env_test

import (
	"testing"

	"github.com/buildkite/agent/env"
	"github.com/stretchr/testify/assert"
)

func TestSubstringExpansion(t *testing.T) {
	var result string
	var err error
	var environ = env.FromSlice([]string{})

	// Missing parameter value:

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:0:7}")
	assert.Nil(t, err)
	assert.Equal(t, "", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:7}")
	assert.Nil(t, err)
	assert.Equal(t, "", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:7:14}")
	assert.Nil(t, err)
	assert.Equal(t, "", result)

	// Basic offsets:

	environ = env.FromSlice([]string{"BUILDKITE_COMMIT=1adf998e39f647b4b25842f107c6ed9d30a3a7c7"})

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:0}")
	assert.Nil(t, err)
	assert.Equal(t, "1adf998e39f647b4b25842f107c6ed9d30a3a7c7", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:7}")
	assert.Nil(t, err)
	assert.Equal(t, "e39f647b4b25842f107c6ed9d30a3a7c7", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:-7}")
	assert.Nil(t, err)
	assert.Equal(t, "0a3a7c7", result)

	// Out of range offsets:

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:-128}")
	assert.Nil(t, err)
	assert.Equal(t, "1adf998e39f647b4b25842f107c6ed9d30a3a7c7", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:128}")
	assert.Nil(t, err)
	assert.Equal(t, "", result)

	// Including lengths:

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:0:7}")
	assert.Nil(t, err)
	assert.Equal(t, "1adf998", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:7:7}")
	assert.Nil(t, err)
	assert.Equal(t, "e39f647", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:7:-7}")
	assert.Nil(t, err)
	assert.Equal(t, "e39f647b4b25842f107c6ed9d3", result)

	// Zero-sized:

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:0:0}")
	assert.Nil(t, err)
	assert.Equal(t, "", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:7:0}")
	assert.Nil(t, err)
	assert.Equal(t, "", result)

	// Out of range lengths:

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:0:128}")
	assert.Nil(t, err)
	assert.Equal(t, "1adf998e39f647b4b25842f107c6ed9d30a3a7c7", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:7:128}")
	assert.Nil(t, err)
	assert.Equal(t, "e39f647b4b25842f107c6ed9d30a3a7c7", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:0:-128}")
	assert.Nil(t, err)
	assert.Equal(t, "", result)

	result, err = environ.Interpolate("${BUILDKITE_COMMIT:7:-128}")
	assert.Nil(t, err)
	assert.Equal(t, "", result)
}

func TestPipelineParser(t *testing.T) {
	t.Parallel()

	var result string
	var err error
	var environ = env.FromSlice([]string{})

	// It does nothing to byte slices with no environmnet variables
	result, err = environ.Interpolate("foo")
	assert.Nil(t, err)
	assert.Equal(t, result, "foo")

	// It does nothing to empty strings
	result, err = environ.Interpolate("")
	assert.Nil(t, err)
	assert.Equal(t, result, "")

	environ = env.FromSlice([]string{"WHO=World!"})

	// It parses regular env vars
	result, err = environ.Interpolate(`
	  steps:
	    - command: "Hello $WHO"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "Hello World!"
	`)

	// It inserts a blank string if the var hasn't been set
	result, err = environ.Interpolate(`
	  steps:
	    - command: "Hello $WHO_REALLY"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "Hello "
	`)

	// It returns an error with an invalid looking env var
	result, err = environ.Interpolate(`
	  steps:
	    - command: "Hello $123"
	`)
	assert.NotNil(t, err)
	assert.Equal(t, string(err.Error()), "Invalid environment variable `$123` - they can only start with a letter")

	// They can be embedded in strings and keys
	environ = env.FromSlice([]string{"KEY=command", "END_OF_HELLO=llo"})

	result, err = environ.Interpolate(`
	  steps:
	    - $KEY: "He$END_OF_HELLO"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "Hello"
	`)

	// The parser supports the other type of env variable
	environ = env.FromSlice([]string{"TODAY=Sunday", "TOMORROW=Monday"})

	result, err = environ.Interpolate(`
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
	environ = env.FromSlice([]string{})

	result, err = environ.Interpolate(`
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
	environ = env.FromSlice([]string{"THIS_VAR_IS_NULL="})

	result, err = environ.Interpolate(`
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
	result, err = environ.Interpolate(`
	  steps:
	    - command: "Hello ${REQUIRED_VAR?}"
	`)
	assert.NotNil(t, err)
	assert.Equal(t, string(err.Error()), "$REQUIRED_VAR: not set")

	// The error message for them can be customized
	result, err = environ.Interpolate(`
	  steps:
	    - command: "Hello ${REQUIRED_VAR?y u no set me? :-{}"
	`)
	assert.NotNil(t, err)
	assert.Equal(t, string(err.Error()), "$REQUIRED_VAR: y u no set me? :-{")

	// Lets you escape variables using 2 different syntaxes
	result, err = environ.Interpolate(`
	  steps:
            - command: "Do this $$ESCAPE_PARTY"
            - command: "Do this \$ESCAPE_PARTY"
            - command: "Do this $${SUCH_ESCAPE}"
            - command: "Do this \${SUCH_ESCAPE}"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
            - command: "Do this $ESCAPE_PARTY"
            - command: "Do this $ESCAPE_PARTY"
            - command: "Do this ${SUCH_ESCAPE}"
            - command: "Do this ${SUCH_ESCAPE}"
	`)

	// Lets you use special characters in the default env var option
	result, err = environ.Interpolate(`
	  steps:
	    - command: "${THIS_VAR_IS_NULL:--:{}}"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
	    - command: "-:{}"
	`)

	// Lets you use special characters in the required var option. In this
	// example, the first `}` character is what is used to complete the ${}
	// var, and the last one is just ignored.
	result, err = environ.Interpolate(`
	  steps:
	    - command: "Hello ${REQUIRED_VAR?{}}"
	`)
	assert.NotNil(t, err)
	assert.Equal(t, string(err.Error()), "$REQUIRED_VAR: {")

	// Lets you parse a full looking pipeline
	environ = env.FromSlice([]string{"BUILDKITE_COMMIT=1adf998e39f647b4b25842f107c6ed9d30a3a7c7"})
	result, err = environ.Interpolate(`
          env:
            IMAGE: registry.dev.example.com/app:${BUILDKITE_COMMIT}
            REVISION: ${BUILDKITE_COMMIT}
          steps:
            - name: ":docker:"
              command: docker build -t $$IMAGE --build-arg REVISION=$$BUILDKITE_COMMIT .
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
          env:
            IMAGE: registry.dev.example.com/app:1adf998e39f647b4b25842f107c6ed9d30a3a7c7
            REVISION: 1adf998e39f647b4b25842f107c6ed9d30a3a7c7
          steps:
            - name: ":docker:"
              command: docker build -t $IMAGE --build-arg REVISION=$BUILDKITE_COMMIT .
	`)

	// The regex isn't greedy. The result of ENV_1 doesn't contain the
	// `BUILDKITE_COMMIT` value, because the interpolator sees the actual
	// variable key as being `BUILDKITE_COMMIT_`. To get this working, the
	// user has to use the ${} syntax.
	environ = env.FromSlice([]string{"BUILDKITE_COMMIT=cfeeee3fa7fa1a6311723f5cbff95b738ec6e683", "BUILDKITE_PARALLEL_JOB=456"})
	result, err = environ.Interpolate(`
	  steps:
            - command: echo "ENV_1=test_$BUILDKITE_COMMIT_$BUILDKITE_PARALLEL_JOB"
            - command: echo "ENV_2=test_${BUILDKITE_COMMIT}_${BUILDKITE_PARALLEL_JOB}"
	`)
	assert.Nil(t, err)
	assert.Equal(t, result, `
	  steps:
            - command: echo "ENV_1=test_456"
            - command: echo "ENV_2=test_cfeeee3fa7fa1a6311723f5cbff95b738ec6e683_456"
	`)
}
