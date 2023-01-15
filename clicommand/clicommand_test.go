package clicommand_test

type Test[T any] struct {
	name           string
	env            map[string]string
	args           []string
	expectedConfig T
}
