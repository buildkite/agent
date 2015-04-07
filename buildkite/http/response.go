package http

type Response struct {
	Request    *Request
	StatusCode int
	Headers    []Header
	Body       string
}
