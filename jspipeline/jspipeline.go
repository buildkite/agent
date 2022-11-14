package jspipeline

import (
	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	"github.com/pkg/errors"
)

type Evaluator struct{}

func (parser *Evaluator) Evaluate(input []byte) ([]byte, error) {
	registry := &require.Registry{}
	runtime := goja.New()
	registry.Enable(runtime)

	_, err := runtime.RunString(string(input))
	if err != nil {
		return nil, errors.Wrapf(err, "error evaluating pipeline")
	}

	object := runtime.Get("exports").ToObject(runtime)
	json, err := object.MarshalJSON()
	if err != nil {
		return nil, errors.Wrapf(err, "error serializing pipeline")
	}

	return json, nil
}
