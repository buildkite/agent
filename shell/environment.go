package shell

import (
	"regexp"
	"sort"
	"strings"
	"runtime"
)

type Environment struct {
	env map[string]string
}

// Creates a new environment from a string slice of KEY=VALUE
func EnvironmentFromSlice(s []string) *Environment {
	env := &Environment{env: make(map[string]string)}

	for _, l := range s {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 2 {
			env.Set(parts[0], parts[1])
		}
	}

	return env
}

var PosixExportLineRegex = regexp.MustCompile("\\Adeclare \\-x ([a-zA-Z_]+[a-zA-Z0-9_]*)(=\")?(.+)?\\z")
var EndsWithUnescapedQuoteRegex = regexp.MustCompile("([^\\\\]\"\\z|\\A\"\\z)")

// Creates a new environment from a shell export of environment variables. On
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

// Environment variables on Windows are case-insensitive. When you run `SET`
// within a Windows command prompt, you'll see variables like this:
//
//     ...
//     Path=C:\Program Files (x86)\Parallels\Parallels Tools\Applications;...
//     PROCESSOR_IDENTIFIER=Intel64 Family 6 Model 94 Stepping 3, GenuineIntel
//     SystemDrive=C:
//     SystemRoot=C:\Windows
//     ...
//
// There's a mix of both CamelCase and UPPERCASE, but the can all be accessed
// regardless of the case you use. So PATH is the same as Path, PAth, pATH,
// etc.
//
// os.Environ() in Golang returns key/values in the original casing, so it
// returns a slice like this:
//
//     { "Path=...", "PROCESSOR_IDENTIFIER=...", "SystemRoot=..." }
//
// Users of shell.Environment shouldn't need to care about this.
// env.Get("PATH") should "just work" on Windows. This means on Windows
// machines, we'll normalise all the keys that go in/out of this API.
//
// Unix systems _are_ case sensitive when it comes to ENV, so we'll just leave
// that alone.
func normalizeKeyName(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	} else {
		return key
	}
}

func EnvironmentFromExport(body string) *Environment {
	// Create the environment that we'll load values into
	env := &Environment{env: make(map[string]string)}

	// Remove any white space at the start and the end of the export string
	body = strings.TrimSpace(body)

	// Split up the export into lines
	lines := strings.Split(body, "\n")

	// No lines! An empty environment it is@
	if len(lines) == 0 {
		return env
	}

	// Determine if we're either parsing a Windows or *nix style export
	if PosixExportLineRegex.MatchString(lines[0]) {
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
				if EndsWithUnescapedQuoteRegex.MatchString(line) {
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
				if EndsWithUnescapedQuoteRegex.MatchString(parts[1]) {
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
	} else {
		// Windows exports are easy since they can just be handled by
		// out built-in EnvironmentFromSlice gear
		return EnvironmentFromSlice(lines)
	}
}

// Returns a key from the environment
func (e *Environment) Get(key string) string {
	return e.env[normalizeKeyName(key)]
}

// Returns true/false depending on whether or not the key exists in the env
func (e *Environment) Exists(key string) bool {
	_, ok := e.env[normalizeKeyName(key)]

	return ok
}

// Sets a key in the environment
func (e *Environment) Set(key string, value string) string {
	e.env[normalizeKeyName(key)] = value

	return value
}

// Remove a key from the Environment and return it's value
func (e *Environment) Remove(key string) string {
	value := e.Get(key)
	delete(e.env, normalizeKeyName(key))
	return value
}

// Returns the length of the environment
func (e *Environment) Length() int {
	return len(e.env)
}

// Returns a new environment with all the variables that have changed
func (e *Environment) Diff(other *Environment) *Environment {
	diff := &Environment{env: make(map[string]string)}

	for k, v := range e.env {
		if other.Get(k) != v {
			diff.Set(k, v)
		}
	}

	return diff
}

// Merges another env into this one and returns the result
func (e *Environment) Merge(other *Environment) *Environment {
	c := e.Copy()

	for k, v := range other.ToMap() {
		c.Set(k, v)
	}

	return c
}

// Returns a copy of the env
func (e *Environment) Copy() *Environment {
	c := make(map[string]string)

	for k, v := range e.env {
		c[k] = v
	}

	return &Environment{env: c}
}

// Returns a slice representation of the environment
func (e *Environment) ToSlice() []string {
	s := []string{}
	for k, v := range e.env {
		s = append(s, k+"="+v)
	}

	// Ensure they are in a consistent order (helpful for tests)
	sort.Strings(s)

	return s
}

// Returns a map representation of the environment
func (e *Environment) ToMap() map[string]string {
	return e.env
}
