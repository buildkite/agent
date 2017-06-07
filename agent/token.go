package agent

import "errors"

var errEmptyToken = errors.New("Invalid token, empty string supplied")

type TokenReader interface {
	Read() (string, error)
}

type StringToken string

func (s StringToken) Read() (string, error) {
	str := string(s)
	if str == "" {
		return str, errEmptyToken
	}
	return str, nil
}
