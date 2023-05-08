package socket

import (
	"net/http"
	"time"
)

// LoggerMiddleware logs all requests (method, URL, and handle time) with the
// formatted logging function (logf).
func LoggerMiddleware(prefix string, logf func(f string, v ...any)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t := time.Now()
			next.ServeHTTP(w, r)
			logf("%s:\t%s\t%s\t%s", prefix, r.Method, r.URL.Path, time.Since(t))
		})
	}
}

// HeadersMiddleware is a middleware that sets common headers for all
// responses.
func HeadersMiddleware(headers http.Header) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			for k, v := range headers {
				h[k] = v
			}
			next.ServeHTTP(w, r)
		})
	}
}
