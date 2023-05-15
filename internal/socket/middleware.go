package socket

import (
	"net/http"
	"strings"
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

// AuthMiddleware is a middleware that checks the Authorization header of an
// incoming request for a Bearer token and checks that that token is the
// correct one.
func AuthMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				WriteError(w, "authorization header is required", http.StatusUnauthorized)
				return
			}

			authType, reqToken, found := strings.Cut(auth, " ")
			if !found {
				WriteError(w, "invalid authorization header: must be in the form `Bearer <token>`", http.StatusUnauthorized)
				return
			}

			if authType != "Bearer" {
				WriteError(w, "invalid authorization header: type must be Bearer", http.StatusUnauthorized)
				return
			}

			if reqToken != token {
				WriteError(w, "invalid authorization token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
