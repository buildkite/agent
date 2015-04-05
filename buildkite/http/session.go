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
