package ptr

// Go Proverbs: "A little copying is better than a little dependency."
//
// This file is basically the one function from k8s.io/utils/ptr.

// To returns a pointer to a variable containing the value.
func To[T any](t T) *T { return &t }
