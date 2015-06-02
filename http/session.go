package http

import (
	stdhttp "net/http"
)

type Session struct {
	Client *stdhttp.Client

	Endpoint  string
	UserAgent string
	Headers   []Header
}

func (s *Session) NewRequest(method string, path string) Request {
	request := Request{
		Method:  method,
		Path:    path,
		Session: s,
	}

	return request
}
