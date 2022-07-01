package env

import (
	"regexp"
	"strings"
)

var (
	posixExportLineRegex = regexp.MustCompile("\\Adeclare \\-[aAfFgiIlnrtux]+ ([a-zA-Z_]+[a-zA-Z0-9_]*)(=\")?(.+)?\\z")
	//                                                             ^
	//                                                             |
	//                                           all of the available options for declare
	endsWithUnescapedQuoteRegex = regexp.MustCompile("([^\\\\]\"\\z|\\A\"\\z)")

	// There are a bunch of types of bash variable that we want to ignore, becuase supporting them
	// is either a pain, or useless, or both.
	disallowedDeclareOpts = map[rune]struct{}{
		'a': {}, // declare -a - it's an array, and can't be used in the environment anyway
		'A': {}, // declare -A - it's an associative array, and also can't be used in the environment
		'n': {}, // declare -n - it's a reference to another variable, and will be a pain to sort out
		'i': {}, // declare -i - it's an integer, and will break our parser. We theoretically could support this, but it's a pain
	}
)

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
func FromExport(body string) Environment {
	// Create the environment that we'll load values into
	env := Environment{}

	// Remove any white space at the start and the end of the export string
	body = strings.TrimSpace(body)

	// Normalize \r\n to just \n
	body = strings.Replace(body, "\r\n", "\n", -1)

	// Split up the export into lines
	lines := strings.Split(body, "\n")

	// No lines! An empty environment it is!
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

			if strings.HasPrefix(line, "declare") {
				command := strings.SplitN(line, " ", 3)
				if len(command) < 3 {
					// There should be at least three elements to the command; the format is:
					// declare -x VARNAME="VALUE"
					// If there's anything less than that, something is seriously wonky with this line and we won't be able to parse it anyway, so just skip it
					continue
				}

				// Massive assumption here! We're assuming that that the options passed to declare have been coalesced -
				// ie, that the command looks like `declare -axtn VARNAME="BLARGH"` rather than `declare -x -a -t -n VARNAME="BLARGH"`
				// However, the output of `export -p` (which is what we're parsing here) is pretty consistent in coalescing the args
				// God willing, this shouldn't change.
				declareOpts := command[1]

				if containsDisallowedOpts(declareOpts) {
					// The opts contain an option that we can't parse, so skip parsing this line
					continue
				}

				// Remove "declare" and any declare args from the command
				line = command[2]
			}

			// We now have a line that can either be one of these:
			//
			//     1. `FOO="bar"`
			//     2. `FOO="open quote for new lines`
			//     3. `FOO`
			//
			key, val, ok := strings.Cut(line, `="`)
			if ok {
				// If the value ends with an unescaped quote,
				// then we know it's a single line environment
				// variable (see example 1)
				if endsWithUnescapedQuoteRegex.MatchString(val) {
					// Remove the `"` at the end
					singleLineValueWithQuoteRemoved := strings.TrimSuffix(val, `"`)

					// Set the single line env var
					env.Set(key, unquoteShell(singleLineValueWithQuoteRemoved))
				} else {
					// We're in an example 2 situation,
					// where we need to keep keep the
					// environment variable open until we
					// encounter a line that ends with an
					// unescaped quote
					openKeyName = key
					openKeyValue = []string{val}
				}
			} else {
				// Since there wasn't an `=` sign, we assume
				// it's just an environment variable without a
				// value (see example 3)
				env.Set(key, "")
			}
		}

		// Return our parsed environment
		return env
	}

	// Windows exports are easy since they can just be handled by our built-in FromSlice gear
	return FromSlice(lines)
}

func containsDisallowedOpts(opts string) bool {
	for _, opt := range opts {
		if _, present := disallowedDeclareOpts[opt]; present {
			return true
		}
	}

	return false
}

func unquoteShell(value string) string {
	// Turn things like \\n back into \n
	value = strings.Replace(value, `\\`, `\`, -1)

	// Shells escape $ cause of environment variable interpolation
	value = strings.Replace(value, `\$`, `$`, -1)

	// They also escape double quotes when showing a value within double
	// quotes, for example, "this is a \" quote string"
	value = strings.Replace(value, `\"`, `"`, -1)

	// Replace a literal escaped backtick (\`) with just a backtick (`)
	// Some variables (like passwords) can contain backticks that should be interpreted as literal backticks, rather than
	// as the beginning of a subcommand
	value = strings.Replace(value, "\\`", "`", -1)

	return value
}
