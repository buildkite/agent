package env

import (
	"regexp"
	"strings"
)

var posixExportLineRegex = regexp.MustCompile("\\Adeclare \\-x ([a-zA-Z_]+[a-zA-Z0-9_]*)(=\")?(.+)?\\z")
var endsWithUnescapedQuoteRegex = regexp.MustCompile("([^\\\\]\"\\z|\\A\"\\z)")

// FromExport parses environment variables from a shell export of environment variables. On
// *nix it looks like this:
//
//     $ export -p
//     declare -x USER="keithpitt"
//     declare -x VAR1="boom\\nboom\\nshake\\nthe\\nroom"
//     declare -x VAR2="hello
//     friends"
//     declare -x VAR3="hello
//     friends
//     OMG=foo
//     test"
//     declare -x VAR4="great
//     typeset -x TOTES=''
//     lollies"
//     declare -x XPC_FLAGS="0x0"
//
// And on Windowws...
//
//     $ SET
//     SESSIONNAME=Console
//     SystemDrive=C:
//     SystemRoot=C:\Windows
//     TEMP=C:\Users\IEUser\AppData\Local\Temp
//     TMP=C:\Users\IEUser\AppData\Local\Temp
//     USERDOMAIN=IE11WIN10
//
func FromExport(body string) *Environment {
	// Create the environment that we'll load values into
	env := &Environment{env: make(map[string]string)}

	// Remove any white space at the start and the end of the export string
	body = strings.TrimSpace(body)

	// Normalize \r\n to just \n
	body = strings.Replace(body, "\r\n", "\n", -1)

	// Split up the export into lines
	lines := strings.Split(body, "\n")

	// No lines! An empty environment it is@
	if len(lines) == 0 {
		return env
	}

	// Determine if we're either parsing a Windows or *nix style export
	if posixExportLineRegex.MatchString(lines[0]) {
		var openKeyName string
		var openKeyValue []string

		for _, line := range lines {
			// Is this line part of a previouly un-closed
			// environment variable?
			if openKeyName != "" {
				// Add the current line to the open variable
				openKeyValue = append(openKeyValue, line)

				// If it ends with an unescaped quote `"`, then
				// that's the end of the variable!
				if endsWithUnescapedQuoteRegex.MatchString(line) {
					// Join all the lines together
					joinedLines := strings.Join(openKeyValue, "\n")

					// Remove the `"` at the end
					multiLineValueWithQuoteRemoved := strings.TrimSuffix(joinedLines, `"`)

					// Set the single line env var
					env.Set(openKeyName, unquoteShell(multiLineValueWithQuoteRemoved))

					// Set the variables that track an open environment variable
					openKeyName = ""
					openKeyValue = nil
				}

				// We've finished working on this line, so we
				// can just got the next one
				continue
			}

			// Trim the `declare -x ` off the start of the line
			line = strings.TrimPrefix(line, "declare -x ")

			// We now have a line that can either be one of these:
			//
			//     1. `FOO="bar"`
			//     2. `FOO="open quote for new lines`
			//     3. `FOO`
			//
			parts := strings.SplitN(line, "=\"", 2)
			if len(parts) == 2 {
				// If the value ends with an unescaped quote,
				// then we know it's a single line environment
				// variable (see example 1)
				if endsWithUnescapedQuoteRegex.MatchString(parts[1]) {
					// Remove the `"` at the end
					singleLineValueWithQuoteRemoved := strings.TrimSuffix(parts[1], `"`)

					// Set the single line env var
					env.Set(parts[0], unquoteShell(singleLineValueWithQuoteRemoved))
				} else {
					// We're in an example 2 situation,
					// where we need to keep keep the
					// environment variable open until we
					// encounter a line that ends with an
					// unescaped quote
					openKeyName = parts[0]
					openKeyValue = []string{parts[1]}
				}
			} else {
				// Since there wasn't an `=` sign, we assume
				// it's just an environment variable without a
				// value (see example 3)
				env.Set(parts[0], "")
			}
		}

		// Return our parsed environment
		return env
	}

	// Windows exports are easy since they can just be handled by our built-in FromSlice gear
	return FromSlice(lines)
}

func unquoteShell(value string) string {
	// Turn things like \\n back into \n
	value = strings.Replace(value, `\\`, `\`, -1)

	// Shells escape $ cause of environment variable interpolation
	value = strings.Replace(value, `\$`, `$`, -1)

	// They also escape double quotes when showing a value within double
	// quotes, i.e. "this is a \" quote string"
	value = strings.Replace(value, `\"`, `"`, -1)

	return value
}
