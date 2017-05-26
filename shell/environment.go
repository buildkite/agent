package shell

import (
	"sort"
	"strings"
	"regexp"
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

var PosixExportLineRegex = regexp.MustCompile("\\Adeclare \\-x ([a-zA-Z_]+[a-zA-Z0-9_]*)=\"(.+)?\\z")

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
func EnvironmentFromExport(body string) *Environment {
	// Create the environment that we'll load values into
	env := &Environment{env: make(map[string]string)}

	// Split up the export into lines
	lines := strings.Split(body, "\n")

	// No lines! An empty environment it is@
	if len(lines) == 0 {
		return env
	}

	// Determine if we're either parsing a Windows or *nix style export
	if PosixExportLineRegex.MatchString(lines[0]) {
		var currentKeyName string
		var currentValueSlice []string

		// Loop through each of the lines
		for _, line := range lines {
			// Use our regular expression to see if this looks like
			// an export line
			matches := PosixExportLineRegex.FindStringSubmatch(line)

			// If we've got a match, then we can put out the key
			if len(matches) == 3 {
				// If we've already got a key in the buffer,
				// add it to the environment and clear the
				// buffers
				if currentKeyName != "" && len(currentValueSlice) > 0 {
					env.Set(currentKeyName, strings.Trim(strings.Join(currentValueSlice, "\n"), "\""))
					currentValueSlice = nil
				}

				currentKeyName = matches[1]
				currentValueSlice = []string{matches[2]}
			} else {
				if currentKeyName != "" {
					currentValueSlice = append(currentValueSlice, line)
				}
			}
		}

		// Check if there's one last environment varible in the buffer that we need to add
		if currentKeyName != "" && len(currentValueSlice) > 0 {
			env.Set(currentKeyName, strings.Trim(strings.Join(currentValueSlice, "\n"), "\""))
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
	return e.env[key]
}

// Returns true/false depending on whether or not the key exists in the env
func (e *Environment) Exists(key string) bool {
	_, ok := e.env[key]

	return ok
}

// Sets a key in the environment
func (e *Environment) Set(key string, value string) string {
	e.env[key] = value

	return value
}

// Remove a key from the Environment and return it's value
func (e *Environment) Remove(key string) string {
	value := e.Get(key)
	delete(e.env, key)
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
		s = append(s, k + "=" + v)
	}

	// Ensure they are in a consistent order (helpful for tests)
	sort.Strings(s)

	return s
}

// Returns a map representation of the environment
func (e *Environment) ToMap() map[string]string {
	return e.env
}
