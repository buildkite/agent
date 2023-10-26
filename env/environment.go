// Package env provides utilities for dealing with environment variables.
//
// It is intended for internal use by buildkite-agent only.
package env

import (
	"encoding/json"
	"runtime"
	"sort"
	"strings"

	"github.com/puzpuzpuz/xsync/v2"
)

// Environment is a map of environment variables, with the keys normalized
// for case-insensitive operating systems
type Environment struct {
	underlying *xsync.MapOf[string, string]
}

func New() *Environment {
	return &Environment{underlying: xsync.NewMapOf[string]()}
}

func NewWithLength(length int) *Environment {
	return &Environment{underlying: xsync.NewMapOfPresized[string](length)}
}

func FromMap(m map[string]string) *Environment {
	env := &Environment{underlying: xsync.NewMapOfPresized[string](len(m))}

	for k, v := range m {
		env.Set(k, v)
	}

	return env
}

// Split splits an environment variable (in the form "name=value") into the name
// and value substrings. If there is no '=', or the first '=' is at the start,
// it returns `"", "", false`.
func Split(l string) (name, value string, ok bool) {
	// Variable names should not contain '=' on any platform...and yet Windows
	// creates environment variables beginning with '=' in some circumstances.
	// See https://github.com/golang/go/issues/49886.
	// Dropping them matches the previous behaviour on Windows, which used SET
	// to obtain the state of environment variables.
	i := strings.IndexRune(l, '=')
	// Either there is no '=', or it is at the start of the string.
	// Both are disallowed.
	if i <= 0 {
		return "", "", false
	}
	return l[:i], l[i+1:], true
}

// FromSlice creates a new environment from a string slice of KEY=VALUE
func FromSlice(s []string) *Environment {
	env := NewWithLength(len(s))

	for _, l := range s {
		if k, v, ok := Split(l); ok {
			env.Set(k, v)
		}
	}

	return env
}

// Dump returns a copy of the environment with all keys normalized
func (e *Environment) Dump() map[string]string {
	d := make(map[string]string, e.underlying.Size())
	e.underlying.Range(func(k, v string) bool {
		d[normalizeKeyName(k)] = v
		return true
	})

	return d
}

// Get returns a key from the environment
func (e *Environment) Get(key string) (string, bool) {
	v, ok := e.underlying.Load(normalizeKeyName(key))
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
	_, ok := e.underlying.Load(normalizeKeyName(key))
	return ok
}

// Set sets a key in the environment
func (e *Environment) Set(key string, value string) string {
	e.underlying.Store(normalizeKeyName(key), value)
	return value
}

// Remove a key from the Environment and return its value
func (e *Environment) Remove(key string) string {
	value, ok := e.Get(key)
	if ok {
		e.underlying.Delete(normalizeKeyName(key))
	}
	return value
}

// Length returns the length of the environment
func (e *Environment) Length() int {
	return e.underlying.Size()
}

// Diff returns a new environment with the keys and values from this
// environment which are different in the other one.
func (e *Environment) Diff(other *Environment) Diff {
	diff := Diff{
		Added:   make(map[string]string),
		Changed: make(map[string]DiffPair),
		Removed: make(map[string]struct{}, 0),
	}

	if other == nil {
		e.underlying.Range(func(k, _ string) bool {
			diff.Removed[k] = struct{}{}
			return true
		})

		return diff
	}

	e.underlying.Range(func(k, v string) bool {
		other, ok := other.Get(k)
		if !ok {
			// This environment has added this key to other
			diff.Added[k] = v
			return true
		}

		if other != v {
			diff.Changed[k] = DiffPair{
				Old: other,
				New: v,
			}
		}

		return true
	})

	other.underlying.Range(func(k, _ string) bool {
		if _, ok := e.Get(k); !ok {
			diff.Removed[k] = struct{}{}
		}

		return true
	})

	return diff
}

// Merge merges another env into this one and returns the result
func (e *Environment) Merge(other *Environment) {
	if other == nil {
		return
	}

	other.underlying.Range(func(k, v string) bool {
		e.Set(k, v)
		return true
	})
}

func (e *Environment) Apply(diff Diff) {
	for k, v := range diff.Added {
		e.Set(k, v)
	}
	for k, v := range diff.Changed {
		e.Set(k, v.New)
	}
	for k := range diff.Removed {
		e.Remove(k)
	}
}

// Copy returns a copy of the env
func (e *Environment) Copy() *Environment {
	if e == nil {
		return New()
	}

	c := New()

	e.underlying.Range(func(k, v string) bool {
		c.Set(k, v)
		return true
	})

	return c
}

// ToSlice returns a sorted slice representation of the environment
func (e *Environment) ToSlice() []string {
	s := []string{}
	e.underlying.Range(func(k, v string) bool {
		s = append(s, k+"="+v)
		return true
	})

	// Ensure they are in a consistent order (helpful for tests)
	sort.Strings(s)

	return s
}

func (e *Environment) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.Dump())
}

func (e *Environment) UnmarshalJSON(data []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	e.underlying = xsync.NewMapOfPresized[string](len(raw))
	for k, v := range raw {
		e.Set(k, v)
	}

	return nil
}

// Environment variables on Windows are case-insensitive. When you run `SET`
// within a Windows command prompt, you'll see variables like this:
//
//	...
//	Path=C:\Program Files (x86)\Parallels\Parallels Tools\Applications;...
//	PROCESSOR_IDENTIFIER=Intel64 Family 6 Model 94 Stepping 3, GenuineIntel
//	SystemDrive=C:
//	SystemRoot=C:\Windows
//	...
//
// There's a mix of both CamelCase and UPPERCASE, but the can all be accessed
// regardless of the case you use. So PATH is the same as Path, PAth, pATH,
// and so on.
//
// os.Environ() in Golang returns key/values in the original casing, so it
// returns a slice like this:
//
//	{ "Path=...", "PROCESSOR_IDENTIFIER=...", "SystemRoot=..." }
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

type Diff struct {
	Added   map[string]string
	Changed map[string]DiffPair
	Removed map[string]struct{}
}

type DiffPair struct {
	Old string
	New string
}

func (diff *Diff) Remove(key string) {
	delete(diff.Added, key)
	delete(diff.Changed, key)
	delete(diff.Removed, key)
}

func (diff *Diff) Empty() bool {
	return len(diff.Added) == 0 && len(diff.Changed) == 0 && len(diff.Removed) == 0
}
