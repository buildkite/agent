package cliconfig

import "strconv"

// OptionalString can be used for a bool-like flag with optional value.
// It can capture three kinds of state:
//   - Not present, or present with a falsy value
//     (e.g.: "", "--foo=false", "--foo=0")
//   - Present, but either with no value, or with a truthy value
//     (e.g.: "--foo", "--foo=true", "--foo=1")
//   - Present with a value that is neither truthy nor falsy
//     (e.g.: "--foo=bar", "--foo=llamas")
type OptionalString struct {
	Trueish bool // True if the flag was passed, and not falsy
	Value   string
}

// IsBoolFlag returns true. (*OptionalString can be used like a bool flag.)
// IsBoolFlag is used to communicate to the flag parser that this flag can be
// passed without a value. See https://pkg.go.dev/flag#Value
func (o *OptionalString) IsBoolFlag() bool {
	return true
}

// Set sets the stored value, and determines truthiness with strconv.ParseBool.
func (o *OptionalString) Set(v string) error {
	b, err := strconv.ParseBool(v)
	o.Trueish = b || err != nil // Is only false if v parsed to false
	o.Value = v
	return nil
}

func (o *OptionalString) String() string {
	return o.Value
}
