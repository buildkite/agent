package bintest

import (
	"fmt"
	"strings"
)

func ArgumentsFromStrings(s []string) Arguments {
	args := make([]interface{}, len(s))

	for idx, v := range s {
		args[idx] = v
	}

	return args
}

type Arguments []interface{}

func (a Arguments) Match(x ...string) (bool, string) {
	for i, expected := range a {
		var formatFail = func(formatter string, args ...interface{}) string {
			return fmt.Sprintf("Argument #%d doesn't match: %s",
				i, fmt.Sprintf(formatter, args...))
		}

		if len(x) <= i {
			return false, formatFail("Expected %q, but missing an argument", expected)
		}

		var actual = x[i]

		if matcher, ok := expected.(Matcher); ok {
			if match, message := matcher.Match(actual); !match {
				return false, formatFail(message)
			}
		} else if s, ok := expected.(string); ok && s != actual {
			return false, formatFail("Expected %q, got %q", expected, actual)
		}
	}
	if len(x) > len(a) {
		return false, fmt.Sprintf(
			"Argument #%d doesn't match: Unexpected extra argument", len(a))
	}

	return true, ""
}

func (a Arguments) String() string {
	return FormatInterfaces(a)
}

type Matcher interface {
	fmt.Stringer
	Match(s string) (bool, string)
}

type MatcherFunc struct {
	f   func(s string) (bool, string)
	str string
}

func (mf MatcherFunc) Match(s string) (bool, string) {
	return mf.f(s)
}

func (mf MatcherFunc) String() string {
	return mf.str
}

func MatchAny() Matcher {
	return MatcherFunc{
		f:   func(s string) (bool, string) { return true, "" },
		str: "bintest.MatchAny()",
	}
}

// Prints a slice of strings as quoted arguments
func FormatStrings(a []string) string {
	var s = make([]string, len(a))
	for idx := range a {
		s[idx] = fmt.Sprintf("%q", a[idx])
	}
	return strings.Join(s, ", ")
}

// Prints a slice of interface{} as quoted arguments
func FormatInterfaces(a []interface{}) string {
	var s = make([]string, len(a))
	for idx := range a {
		switch t := a[idx].(type) {
		case string:
			s[idx] = fmt.Sprintf("%q", t)
		case fmt.Stringer:
			s[idx] = fmt.Sprintf("%s", t.String())
		default:
			s[idx] = fmt.Sprintf("%v", t)
		}
	}
	return strings.Join(s, ", ")
}
