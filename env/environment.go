package env

import (
	"runtime"
	"sort"
	"strings"
)

// Environment is a map of environment variables, with the keys normalized
// for case-insensitive operating systems
type Environment struct {
	env map[string]string
}

func New() *Environment {
	return &Environment{env: map[string]string{}}
}

// FromSlice creates a new environment from a string slice of KEY=VALUE
func FromSlice(s []string) *Environment {
	env := &Environment{env: make(map[string]string, len(s))}

	for _, l := range s {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 2 {
			env.Set(parts[0], parts[1])
		}
	}

	return env
}

// Get returns a key from the environment
func (e *Environment) Get(key string) (string, bool) {
	v, ok := e.env[normalizeKeyName(key)]
	return v, ok
}

// Get a boolean value from environment, with a default for empty. Supports true|false, on|off, 1|0
func (e *Environment) GetBool(key string, defaultValue bool) bool {
	v, _ := e.Get(key)

	switch strings.ToLower(v) {
	case "on", "1", "enabled", "true":
		return true
	case "off", "0", "disabled", "false":
		return false
	default:
		return defaultValue
	}
}

// Exists returns true/false depending on whether or not the key exists in the env
func (e *Environment) Exists(key string) bool {
	_, ok := e.env[normalizeKeyName(key)]
	return ok
}

// Set sets a key in the environment
func (e *Environment) Set(key string, value string) string {
	e.env[normalizeKeyName(key)] = value

	return value
}

// Remove a key from the Environment and return its value
func (e *Environment) Remove(key string) string {
	value, ok := e.Get(key)
	if ok {
		delete(e.env, normalizeKeyName(key))
	}
	return value
}

// Length returns the length of the environment
func (e *Environment) Length() int {
	return len(e.env)
}

// Diff returns a new environment with the keys and values from this
// environment which are different in the other one.
func (e *Environment) Diff(other *Environment) *Environment {
	diff := &Environment{env: make(map[string]string)}
	for k, v := range e.env {
		if other, ok := other.Get(k); !ok || other != v {
			diff.Set(k, v)
		}
	}
	return diff
}

// Merge merges another env into this one and returns the result
func (e *Environment) Merge(other *Environment) *Environment {
	c := e.Copy()

	if other == nil {
		return c
	}

	for k, v := range other.ToMap() {
		c.Set(k, v)
	}

	return c
}

// Copy returns a copy of the env
func (e *Environment) Copy() *Environment {
	c := make(map[string]string)

	for k, v := range e.env {
		c[k] = v
	}

	return &Environment{env: c}
}

// ToSlice returns a sorted slice representation of the environment
func (e *Environment) ToSlice() []string {
	s := []string{}
	for k, v := range e.env {
		s = append(s, k+"="+v)
	}

	// Ensure they are in a consistent order (helpful for tests)
	sort.Strings(s)

	return s
}

// ToMap returns a map representation of the environment
func (e *Environment) ToMap() map[string]string {
	return e.env
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
// Users of env.Environment shouldn't need to care about this.
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
