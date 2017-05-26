package shell

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"sort"
	"strings"
)

type Environment struct {
	env map[string]string
}

var EnvironmentVariableLineRegex = regexp.MustCompile("\\A([a-zA-Z]+[a-zA-Z0-9_]*)=(.+)?\\z")

// Creates a new environment from a string slice of "KEY=VALUE" strings
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

// Creates a new environment from a string with format KEY=VALUE\n or
// KEY=VALUE\0. Parsing environment variables from a string is a little tough.
// For example...
//
//     $ export VAR1="boom\nboom\nshake\nthe\nroom"
//
//     $ echo $VAR1
//     boom
//     boom
//     shake
//     the
//     room
//
//     $ env
//     ...
//     VAR1=boom\nboom\nshake\nthe\nroom
//     _=/usr/bin/env
//
// A multi-line variable works as expected. The variable is flattened and new
// lines are escaped...however...
//
//    $ export VAR2="hello [I hit enter here]
//    dquote> friends"
//
//    $ env
//    ...
//    VAR2=hello
//    friends
//    _=/usr/bin/env
//
// New lines that aren't already escaped appear as 2 sepearte lines when you
// run `env`. This makes it tricky to parse, since `env` joins environment
// variables together with a new line, but doesnt' do anything smart if there
// are new lines within an environment variable.
//
// On Linux, you can run `env --null` which splits each environment varibale
// with a null character, making it easy to split by, but OSX doesn't support
// that.
//
// To solve this, we first try and split by \0 by checking if the last
// character is a null byte. If that's the case, it's an easy parse. If it doesn't,
// then we'll need to do a parse using a split on new lines.
//
// The way we solve the new line issue is by looping over each line and
// checking to see if it "looks" like an environment varible. If it does, we
// set it and move on. If it doesn't, we concat that value to the previous
// environment variable.
//
// The only issue with this appraoch, is that if you have an environment
// variable like this:
//
//    $ export VAR3="hello
//    dquote> friends
//    dquote> OMG=foo
//    dquote> test"
//
//    $ echo $VAR3
//    hello
//    friends
//    OMG=foo
//    test
//
// It will think OMG=foo is an environment variable. That's an acceptable
// caveat for now.
func EnvironmentFromString(str string) (*Environment) {
	// Determine if the string is being seperated by a null byte (such as
	// when you run `env  --null` on Linux)
	if strings.HasSuffix(str, `\0`) {
		return EnvironmentFromSlice(strings.Split(str, `\0`))
	} else {
		env := &Environment{env: make(map[string]string)}

		var currentKeyName string
		var currentValueSlice []string

		// Loop through each of the lines
		for _, line := range strings.Split(str, `\n`) {
			// Run a regular expression to see if the current line
			// "looks" like an environment varible definition.
			matches := EnvironmentVariableLineRegex.FindStringSubmatch(line)

			if len(matches) == 3 {
				// If we've already got a key in the buffer,
				// add it to the environment and clear the
				// buffers
				if currentKeyName != "" && len(currentValueSlice) > 0 {
					env.Set(currentKeyName, strings.Join(currentValueSlice, `\n`))
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
			env.Set(currentKeyName, strings.Join(currentValueSlice, `\n`))
		}

		return env
	}
}

// Creates a new environment from a file with format KEY=VALUE\n
func EnvironmentFromFile(path string) (*Environment, error) {
	body, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return EnvironmentFromString(string(body)), nil
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
		s = append(s, fmt.Sprintf("%v=%v", k, v))
	}

	// Ensure they are in a consistent order (helpful for tests)
	sort.Strings(s)

	return s
}

// Returns a map representation of the environment
func (e *Environment) ToMap() map[string]string {
	return e.env
}
