package shellwords

import (
	"strings"
	"unicode"
)

const (
	batchSpecialChars = "^&;,=%"
	batchEscape       = '^'
)

// SplitBatch splits a command string into words like Windows CMD.EXE would
// See https://ss64.com/nt/syntax-esc.html
func SplitBatch(line string) ([]string, error) {
	p := parser{
		Input:            line,
		QuoteChars:       []rune{'\'', '"'},
		EscapeChar:       batchEscape,
		QuoteEscapeChars: []rune{batchEscape, '"'},
		FieldSeperators:  []rune{'\n', '\t', ' '},
	}
	return p.Parse()
}

// QuoteBatch returns the string such that a CMD.EXE shell would parse it as a single word
func QuoteBatch(s string) string {
	var builder strings.Builder
	var needsQuotes bool

	for _, c := range s {
		if strings.ContainsRune(batchSpecialChars, c) {
			builder.WriteString(string(batchEscape) + string(c))
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
