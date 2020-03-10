package plugin

import (
	"errors"
	"fmt"

	"github.com/buildkite/conditional/ast"
	"github.com/buildkite/conditional/evaluator"
	"github.com/buildkite/conditional/lexer"
	"github.com/buildkite/conditional/object"
	"github.com/buildkite/conditional/parser"
)

type Condition struct {
	Expression ast.Expression
}

func ParseCondition(condition string) (*Condition, error) {
	l := lexer.New(condition)
	p := parser.New(l)
	expr := p.Parse()

	if errs := p.Errors(); len(errs) > 0 {
		return nil, errors.New(errs[0])
	}

	return &Condition{expr}, nil
}

func (f *Condition) Match(p *Plugin) (bool, error) {
	scope := object.Struct{}

	// convert the plugin into a scope for the conditional language
	if err := object.Unmarshal(*p, scope); err != nil {
		return false, err
	}

	obj := evaluator.Eval(f.Expression, object.Struct{
		"plugin": scope,
	})

	err, ok := obj.(*object.Error)
	if ok {
		return false, fmt.Errorf("Failed to evaluate %v => %s", f.Expression, err.Message)
	}

	result, ok := obj.(*object.Boolean)
	if !ok {
		return false, fmt.Errorf("Conditional result is not Boolean. got=%T (%+v)", obj, obj)
	}

	return result.Value, nil
}
