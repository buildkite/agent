package agent

import (
	"regexp"
)

// If you change header parsing here make sure to change it in the
// buildkite.com frontend logic, too

var (
	headerRE          = regexp.MustCompile(`^(---|\+\+\+|~~~)\s`)
	headerExpansionRE = regexp.MustCompile(`^\^\^\^\s\+\+\+`)
	ansiColourRE      = regexp.MustCompile(`\x1b\[([;\d]+)?[mK]`)
)

// isHeaderOrExpansion reports whether a line is a header or header expansion.
func isHeaderOrExpansion(line string) bool {
	// Make sure all ANSI colours are removed from the string before we
	// check to see if it's a header (sometimes a colour escape sequence may
	// be the first thing on the line, which will cause the regex to ignore it)
	line = ansiColourRE.ReplaceAllString(line, "")
	return headerRE.MatchString(line) || headerExpansionRE.MatchString(line)
}
