package shellwords

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// This is a recursive descent parser for our basic shellword grammar.

const (
	eof = -1
)

// Parser takes a string and parses out a tree of structs that represent text and Expansions
type parser struct {
	// Input is the string to parse
	Input string

	// Characters to use for quoted strings
	QuoteChars []rune

	// The character used for escaping
	EscapeChar rune

	// The characters used for escaping quotes in quoted strings
	QuoteEscapeChars []rune

	// Field seperators are used for splitting words
	FieldSeperators []rune

	// The current internal position
	pos int
}

func (p *parser) Parse() ([]string, error) {
	var words = []string{}
	var word strings.Builder

	for {
		// Read until we encounter a delimiter character
		scanned := p.scanUntil(func(r rune) bool {
			return r == p.EscapeChar || p.isQuote(r) || p.isFieldSeperator(r)
		})

		if len(scanned) > 0 {
			word.WriteString(scanned)
		}

		// Read the character that caused the scan to stop
		r := p.nextRune()
		if r == eof {
			break
		}

		switch {
		// Handle quotes
		case p.isQuote(r):
			quote, err := p.scanQuote(r)
			if err != nil {
				return nil, err
			}

			// Write to the buffer
			word.WriteString(quote)

		// Handle escaped characters
		case r == p.EscapeChar:
			if escaped := p.nextRune(); escaped != eof {
				word.WriteRune(escaped)
			}
			continue

		// Handle field seperators
		case p.isFieldSeperator(r):
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}

		default:
			return nil, fmt.Errorf("Unhandled character %c at pos %d", r, p.pos)
		}
	}

	if word.Len() > 0 {
		words = append(words, word.String())
		word.Reset()
	}

	return words, nil
}

func (p *parser) scanQuote(delim rune) (string, error) {
	var quote strings.Builder

	for {
		r := p.nextRune()
		if r == eof {
			return "", fmt.Errorf(
				"Expected closing quote %c at offset %d, got EOF", delim, p.pos-1)
		}
		// Check for escaped characters
		if escapeChar, escaped := p.isQuoteEscape(r); escaped {
			// Handle the case where our escape char is our delimiter (e.g "")
			if escapeChar != delim || p.peekRune() == delim {
				if escaped := p.nextRune(); escaped != eof {
					quote.WriteRune(escaped)
				}
				continue
			}
		}
		if r == delim {
			break
		}
		quote.WriteRune(r)
	}

	return quote.String(), nil
}

func (p *parser) isQuote(r rune) bool {
	for _, qr := range p.QuoteChars {
		if qr == r {
			return true
		}
	}
	return false
}

func (p *parser) isQuoteEscape(r rune) (rune, bool) {
	for _, qr := range p.QuoteEscapeChars {
		if qr == r {
			return qr, true
		}
	}
	return r, false
}

func (p *parser) isFieldSeperator(r rune) bool {
	for _, qr := range p.FieldSeperators {
		if qr == r {
			return true
		}
	}
	return false
}

func (p *parser) scanUntil(f func(rune) bool) string {
	start := p.pos
	for int(p.pos) < len(p.Input) {
		c, size := utf8.DecodeRuneInString(p.Input[p.pos:])
		if c == utf8.RuneError || f(c) {
			break
		}
		p.pos += size
	}
	return p.Input[start:p.pos]
}

func (p *parser) nextRune() rune {
	if int(p.pos) >= len(p.Input) {
		return eof
	}
	c, size := utf8.DecodeRuneInString(p.Input[p.pos:])
	p.pos += size
	return c
}

func (p *parser) peekRune() rune {
	if int(p.pos) >= len(p.Input) {
		return eof
	}
	c, _ := utf8.DecodeRuneInString(p.Input[p.pos:])
	return c
}
