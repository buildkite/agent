package jspipeline

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluate(t *testing.T) {
	expect := require.New(t)
	parser := Evaluator{}

	result, err := parser.Evaluate([]byte(`
		exports = {
			a: 1 + 1,
			b: "hello" + " world"
		}
	`))

	expect.Equal([]byte(`{"a":2,"b":"hello world"}`), result)
	expect.NoError(err)
}
