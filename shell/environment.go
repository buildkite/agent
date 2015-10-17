package shell

import (
	"fmt"
	"io/ioutil"
	"runtime"
	"strings"
)

type Environment struct {
	env map[string]string
}

// Creates a new environment from a string slice of KEY=VALUE
func EnvironmentFromSlice(s []string) (*Environment, error) {
	env := &Environment{env: make(map[string]string)}

	for _, l := range s {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 2 {
			env.Set(parts[0], parts[1])
		}
	}

	return env, nil
}

// Creates a new environment from a file with format KEY=VALUE\n
func EnvironmentFromFile(path string) (*Environment, error) {
	body, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return EnvironmentFromSlice(strings.Split(string(body), "\n"))
}

// Returns a key from the environment
func (e *Environment) Get(key string) string {
	return e.env[key]
}

// Sets a key in the environment
func (e *Environment) Set(key string, value string) string {
	// Trim the values
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	// Check if we've got quoted values
	if strings.Count(value, "\"") == 2 || strings.Count(value, "'") == 2 {
		// Pull the quotes off the edges
		value = strings.Trim(value, "\"'")

		// Expand quotes
		value = strings.Replace(value, "\\\"", "\"", -1)

		// Expand newlines
		value = strings.Replace(value, "\\n", "\n", -1)
	}

	// Environment variable keys are case-insensitive on Windows, so we'll
	// just convert them all to uppercase.
	if runtime.GOOS == "windows" {
		key = strings.ToUpper(key)
	}

	e.env[key] = value

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

// Returns a slice representation of the environment
func (e *Environment) ToSlice() []string {
	s := []string{}
	for k, v := range e.env {
		if strings.Contains(v, "\n") {
			s = append(s, fmt.Sprintf("%v=\"%v\"", k, strings.Replace(v, "\n", "\\n", -1)))
		} else {
			s = append(s, fmt.Sprintf("%v=%v", k, v))
		}
	}

	return s
}

// Returns a map representation of the environment
func (e *Environment) ToMap() map[string]string {
	return e.env
}
