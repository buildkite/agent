package bintest

import (
	"fmt"
	"regexp"
	"strings"
)

func ArgumentsFromStrings(s []string) Arguments {
	args := make([]interface{}, len(s))

	for idx, v := range s {
		args[idx] = v
	}

	return args
}

type ArgumentsMatchResult struct {
	IsMatch     bool
	MatchCount  int
	Explanation string
}

type Arguments []interface{}

func (a Arguments) Match(x ...string) (result ArgumentsMatchResult) {
	for i, expected := range a {
		var formatArgumentMismatch = func(formatter string, args ...interface{}) string {
			return fmt.Sprintf("Argument #%d doesn't match: %s", i+1, fmt.Sprintf(formatter, args...))
		}

		if len(x) <= i {
			result.Explanation = formatArgumentMismatch("Expected %q, but missing an argument", expected)
			return
		}

		var actual = x[i]

		if matcher, ok := expected.(Matcher); ok {
			if match, message := matcher.Match(actual); !match {
				result.Explanation = formatArgumentMismatch(message)
				return
			}
		} else if s, ok := expected.(string); ok && s != actual {
			idx := findCommonPrefix([]rune(s), []rune(actual))
			if idx == 0 {
				result.Explanation = formatArgumentMismatch(
					"Expected %q, got %q", shorten(s), shorten(actual))
			} else {
				result.Explanation = formatArgumentMismatch(
					"Differs at character %d, expected %q, got %q", idx+1,
					shorten(s[idx:]), shorten(actual[idx:]))
			}
			return
		}

		result.MatchCount++
	}
	if len(x) > len(a) {
		result.Explanation = fmt.Sprintf("Argument #%d doesn't match: Unexpected extra argument", len(a))
		return
	}

	result.IsMatch = true
	return
}

const (
	shortenLength = 10
)

func shorten(s string) string {
	if len(s) > shortenLength {
		return s[:shortenLength] + "…"
	}
	return s
}

func findCommonPrefix(s1, s2 []rune) int {
	var maxLength = len(s1)
	if len(s2) < maxLength {
		maxLength = len(s2)
	}

	for i := 0; i < maxLength; i++ {
		if s1[i] != s2[i] {
			return i
		}
	}

	return maxLength
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

func MatchPattern(pattern string) Matcher {
	re := regexp.MustCompile(pattern)
	return MatcherFunc{
		f: func(s string) (bool, string) {
			if !re.MatchString(s) {
				return false, "Didn't match pattern " + pattern
			}
			return true, ""
		},
		str: fmt.Sprintf("bintest.MatchPattern(%q)", pattern),
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
