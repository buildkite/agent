package evaluator

import (
	"fmt"

	"github.com/buildkite/conditional/ast"
	"github.com/buildkite/conditional/object"
)

var (
	NULL  = &object.Null{}
	TRUE  = &object.Boolean{Value: true}
	FALSE = &object.Boolean{Value: false}
)

type Scope interface {
	Get(key string) (object.Object, bool)
}

// Eval an ast.Node (either a literal or an expression), with a scope struct
func Eval(node ast.Node, scope Scope) object.Object {
	// defer untrace(trace("Eval", fmt.Sprintf("%T `%s`", node, node.String())))

	switch node := node.(type) {

	// Expressions
	case *ast.IntegerLiteral:
		return &object.Integer{Value: node.Value}

	case *ast.StringLiteral:
		return &object.String{Value: node.Value}

	case *ast.Regexp:
		return &object.Regexp{Regexp: node.Regexp}

	case *ast.Boolean:
		return nativeBoolToBooleanObject(node.Value)

	case *ast.PrefixExpression:
		right := Eval(node.Right, scope)
		if isError(right) {
			return right
		}
		return evalPrefixExpression(node.Operator, right)

	case *ast.InfixExpression:
		left := Eval(node.Left, scope)
		if isError(left) {
			return left
		}

		var right object.Object
		if node.Operator == `.` {
			ident, ok := node.Right.(*ast.Identifier)
			if !ok {
				return newError("dot operator must receive identifier")
			}
			right = &object.String{Value: ident.Value}
		} else {
			right = Eval(node.Right, scope)
		}
		if isError(right) {
			return right
		}

		return evalInfixExpression(node.Operator, left, right)

	case *ast.Identifier:
		return evalIdentifier(node, scope)

	case *ast.CallExpression:
		args := evalExpressions(node.Arguments, scope)
		if len(args) == 1 && isError(args[0]) {
			return args[0]
		}

		obj, ok := scope.Get(node.Function)
		if !ok {
			return newError("function not defined: " + node.Function)
		}

		return applyFunction(obj, args)

	case *ast.ArrayLiteral:
		elements := evalExpressions(node.Elements, scope)
		if len(elements) == 1 && isError(elements[0]) {
			return elements[0]
		}
		return &object.Array{Elements: elements}

	default:
		return newError("unhandled type: %T", node)
	}
}

func nativeBoolToBooleanObject(input bool) *object.Boolean {
	if input {
		return TRUE
	}
	return FALSE
}

func applyFunction(fn object.Object, args []object.Object) object.Object {
	// defer untrace(trace("applyFunction", args))

	switch fn := fn.(type) {
	case object.Function:
		ret := fn(args)
		// tracePrint(fmt.Sprintf("RETURN: %+v", ret))
		return ret

	default:
		return newError("not a function: %s", fn.Type())
	}
}

func evalPrefixExpression(operator string, right object.Object) object.Object {
	// defer untrace(trace("evalPrefixExpression", operator, right))

	switch operator {
	case "!":
		return evalBangOperatorExpression(right)
	default:
		return newError("unknown operator: %s%s", operator, right.Type())
	}
}

func evalInfixExpression(operator string, left, right object.Object) object.Object {
	// defer untrace(trace("evalInfixExpression", operator, left, right))

	switch {
	case left.Type() == object.INTEGER_OBJ && right.Type() == object.INTEGER_OBJ:
		return evalIntegerInfixExpression(operator, left, right)
	case left.Type() == object.STRING_OBJ && right.Type() == object.STRING_OBJ:
		return evalStringInfixExpression(operator, left, right)
	case left.Type() == object.STRUCT_OBJ && right.Type() == object.STRING_OBJ:
		return evalDotExpression(left, right)
	case left.Type() == object.STRING_OBJ && right.Type() == object.REGEXP_OBJ:
		return evalStringRegexpInfixExpression(operator, left, right)
	case left.Type() == object.ARRAY_OBJ:
		return evalArrayInfixExpression(operator, left, right)
	case operator == "==":
		return nativeBoolToBooleanObject(left == right)
	case operator == "!=":
		return nativeBoolToBooleanObject(left != right)
	case left.Type() != right.Type():
		return newError("type mismatch: %s %s %s",
			left.Type(), operator, right.Type())
	default:
		return newError("unknown operator: %s %s %s",
			left.Type(), operator, right.Type())
	}
}

func evalBangOperatorExpression(right object.Object) object.Object {
	// defer untrace(trace("evalBangOperatorExpression", right))

	switch right {
	case TRUE:
		return FALSE
	case FALSE:
		return TRUE
	case NULL:
		return TRUE
	default:
		return FALSE
	}
}

func evalIntegerInfixExpression(operator string, left, right object.Object) object.Object {
	// defer untrace(trace("evalIntegerInfixExpression", operator, left, right))

	leftVal := left.(*object.Integer).Value
	rightVal := right.(*object.Integer).Value

	switch operator {
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("unknown operator: %s %s %s",
			left.Type(), operator, right.Type())
	}
}

func evalStringInfixExpression(operator string, left, right object.Object) object.Object {
	// defer untrace(trace("evalStringInfixExpression", operator, left, right))

	leftVal := left.(*object.String).Value
	rightVal := right.(*object.String).Value

	switch operator {
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newError("unknown operator: %s %s %s",
			left.Type(), operator, right.Type())
	}
}

func evalStringRegexpInfixExpression(operator string, left, right object.Object) object.Object {
	// defer untrace(trace("evalStringRegexpInfixExpression", operator, left, right))

	leftVal := left.(*object.String).Value
	rightVal := right.(*object.Regexp).Regexp

	switch operator {
	case "=~":
		return nativeBoolToBooleanObject(rightVal.MatchString(leftVal))
	case "!~":
		return nativeBoolToBooleanObject(!rightVal.MatchString(leftVal))
	default:
		return newError("unknown operator: %s %s %s",
			left.Type(), operator, right.Type())
	}
}

func arrayContains(arr *object.Array, obj object.Object) (bool, error) {
	// defer untrace(trace("arrayContains", arr, obj))

	for idx, el := range arr.Elements {
		if el.Type() != obj.Type() {
			return false, fmt.Errorf("type mismatch at index %d in array: %s vs %s",
				idx, el.Type(), obj.Type())
		}
		if el.Equals(obj) {
			return true, nil
		}
	}

	return false, nil
}

func evalArrayInfixExpression(operator string, left, right object.Object) object.Object {
	// defer untrace(trace("evalStringArrayInfixExpression", operator, left, right))

	leftVal := left.(*object.Array)

	switch operator {
	case "@>":
		contains, err := arrayContains(leftVal, right)
		if err != nil {
			return newError(err.Error())
		}
		return nativeBoolToBooleanObject(contains)
	default:
		return newError("unknown operator: %s %s %s",
			left.Type(), operator, right.Type())
	}
}

func evalDotExpression(s object.Object, prop object.Object) object.Object {
	// defer untrace(trace("evalDotExpression", s, prop))

	structVal, ok := s.(object.Struct)
	if !ok {
		return newError("type can't be used with the dot operator: %T", s)
	}

	propVal := prop.(*object.String).Value

	val, ok := structVal.Get(propVal)
	if !ok {
		return newError("struct has no property %q", propVal)
	}

	return val

}

func evalIdentifier(node *ast.Identifier, scope Scope) object.Object {
	// defer untrace(trace("evalIdentifier"))

	val, ok := scope.Get(node.Value)
	if !ok {
		return newError("identifier not found: " + node.Value)
	}

	return val
}

func newError(format string, a ...interface{}) *object.Error {
	return &object.Error{Message: fmt.Sprintf(format, a...)}
}

func isError(obj object.Object) bool {
	if obj != nil {
		return obj.Type() == object.ERROR_OBJ
	}
	return false
}

func evalExpressions(exps []ast.Expression, scope Scope) []object.Object {
	// defer untrace(trace("evalExpressions", exps))

	var result []object.Object

	for _, e := range exps {
		evaluated := Eval(e, scope)
		if isError(evaluated) {
			return []object.Object{evaluated}
		}
		result = append(result, evaluated)
	}

	return result
}
