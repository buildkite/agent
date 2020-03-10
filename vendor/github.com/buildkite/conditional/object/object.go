package object

import (
	"bytes"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

type ObjectType string

const (
	NULL_OBJ  = "NULL"
	ERROR_OBJ = "ERROR"

	STRING_OBJ  = "STRING"
	INTEGER_OBJ = "INTEGER"
	BOOLEAN_OBJ = "BOOLEAN"
	REGEXP_OBJ  = "REGEXP"

	STRUCT_OBJ   = "STRUCT"
	FUNCTION_OBJ = "FUNCTION"
	ARRAY_OBJ    = "ARRAY"
)

type Object interface {
	Type() ObjectType
	String() string
	Equals(o Object) bool
}

type Integer struct {
	Value int64
}

func (i *Integer) Type() ObjectType     { return INTEGER_OBJ }
func (i *Integer) String() string       { return fmt.Sprintf("%d", i.Value) }
func (i *Integer) Equals(o Object) bool { return i.String() == o.String() }

type Boolean struct {
	Value bool
}

func (b *Boolean) Type() ObjectType     { return BOOLEAN_OBJ }
func (b *Boolean) String() string       { return fmt.Sprintf("%t", b.Value) }
func (b *Boolean) Equals(o Object) bool { return b == o }

type String struct {
	Value string
}

func (s *String) Type() ObjectType     { return STRING_OBJ }
func (s *String) String() string       { return fmt.Sprintf("%q", s.Value) }
func (s *String) Equals(o Object) bool { return s.String() == o.String() }

type Regexp struct {
	*regexp.Regexp
}

func (r *Regexp) Type() ObjectType     { return REGEXP_OBJ }
func (r *Regexp) String() string       { return r.Regexp.String() }
func (r *Regexp) Equals(o Object) bool { return r.String() == o.String() }

type Null struct{}

func (n *Null) Type() ObjectType { return NULL_OBJ }
func (n *Null) String() string   { return "null" }
func (n *Null) Equals(o Object) bool {
	_, ok := o.(*Null)
	return ok
}

type Function func(args []Object) Object

func (f Function) Type() ObjectType     { return FUNCTION_OBJ }
func (f Function) String() string       { return "function" }
func (f Function) Equals(o Object) bool { return false }

type Struct map[string]Object

func (s Struct) Type() ObjectType { return STRUCT_OBJ }
func (s Struct) String() string   { return "struct" }

func (s Struct) Get(key string) (Object, bool) {
	obj, ok := s[key]
	return obj, ok
}

func (s Struct) Set(key string, obj Object) {
	s[key] = obj
}

func (s Struct) Has(key string) bool {
	_, ok := s[key]
	return ok
}

func (s Struct) Equals(o Object) bool {
	return reflect.DeepEqual(s, o)
}

type Array struct {
	Elements []Object
}

func (ao *Array) Type() ObjectType { return ARRAY_OBJ }
func (ao *Array) String() string {
	var out bytes.Buffer

	elements := []string{}
	for _, e := range ao.Elements {
		elements = append(elements, e.String())
	}

	out.WriteString("[")
	out.WriteString(strings.Join(elements, ", "))
	out.WriteString("]")

	return out.String()
}
func (ao *Array) Equals(o Object) bool {
	return reflect.DeepEqual(ao, o)
}

type Error struct {
	Message string
}

func (e *Error) Type() ObjectType     { return ERROR_OBJ }
func (e *Error) String() string       { return "ERROR: " + e.Message }
func (e *Error) Equals(o Object) bool { return e == o }
