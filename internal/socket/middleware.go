package socket

import (
	"maps"
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
			maps.Copy(h, headers)
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware is a middleware that checks the Authorization header of an
// incoming request for a Bearer token and checks that that token is the
// correct one. If there is an error while responding with an auth failure,
// it is logged with errorLogf.
func AuthMiddleware(token string, errorLogf func(f string, v ...any)) func(http.Handler) http.Handler {
	if errorLogf == nil {
		errorLogf = func(string, ...any) {}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				if err := WriteError(w, "authorization header is required", http.StatusUnauthorized); err != nil {
					errorLogf("AuthMiddleware: couldn't write error response: %v", err)
				}
				return
			}

			authType, reqToken, found := strings.Cut(auth, " ")
			if !found {
				if err := WriteError(w, "invalid authorization header: must be in the form `Bearer <token>`", http.StatusUnauthorized); err != nil {
					errorLogf("AuthMiddleware: couldn't write error response: %v", err)
				}
				return
			}

			if authType != "Bearer" {
				if err := WriteError(w, "invalid authorization header: type must be Bearer", http.StatusUnauthorized); err != nil {
					errorLogf("AuthMiddleware: couldn't write error response: %v", err)
				}
				return
			}

			if reqToken != token {
				if err := WriteError(w, "invalid authorization token", http.StatusUnauthorized); err != nil {
					errorLogf("AuthMiddleware: couldn't write error response: %v", err)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
