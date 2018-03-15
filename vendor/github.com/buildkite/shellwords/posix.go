package shellwords

import (
	"strings"
	"unicode"
)

const (
	posixSpecialChars = "!\"#$&'()*,;<=>?[]\\^`{}|~"
	posixEscape       = '\\'
)

// SplitPosix splits a command string into words like a posix shell would
func SplitPosix(line string) ([]string, error) {
	p := parser{
		Input:            line,
		QuoteChars:       []rune{'\'', '"'},
		EscapeChar:       posixEscape,
		QuoteEscapeChars: []rune{posixEscape},
		FieldSeperators:  []rune{'\n', '\t', ' '},
	}
	return p.Parse()
}

// QuotePosix returns the string such that a posix shell would parse it as a single word
func QuotePosix(s string) string {
	var builder strings.Builder
	var needsQuotes bool

	for _, c := range s {
		if strings.ContainsRune(posixSpecialChars, c) {
			builder.WriteString(string(posixEscape) + string(c))
		} else {
			builder.WriteRune(c)
		}
		if unicode.IsSpace(c) {
			needsQuotes = true
		}
	}

	if needsQuotes {
		return `"` + builder.String() + `"`
	}

	return builder.String()
}
