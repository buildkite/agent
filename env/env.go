package env

import (
	"fmt"
	"io/ioutil"
	"strings"
)

type Environment struct {
	env map[string]string
}

// Creates a new environment from a string slice of KEY=VALUE
func New(s []string) (*Environment, error) {
	env := make(map[string]string)

	for _, l := range s {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}

	return &Environment{env: env}, nil
}

// Creates a new environment from a file with format KEY=VALUE\n
func NewFromFile(path string) (*Environment, error) {
	body, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return New(strings.Split(string(body), "\n"))
}

// Returns a key from the environment
func (e *Environment) Get(key string) string {
	return e.env[key]
}

// Sets a key in the environment
func (e *Environment) Set(key string, value string) string {
	e.env[key] = value

	return value
}

// Returns the length of the environment
func (e *Environment) Length() int {
	return len(e.env)
}

// Returns a new environment with all the variables that have changed
func (e *Environment) Diff(other *Environment) *Environment {
	diff, _ := New([]string{})

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
		s = append(s, fmt.Sprintf("%s=%s", k, v))
	}

	return s
}

// Returns a map representation of the environment
func (e *Environment) ToMap() map[string]string {
	return e.env
}
