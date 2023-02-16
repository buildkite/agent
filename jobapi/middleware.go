package jobapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/job/shell"
)

func LoggerMiddleware(l shell.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t := time.Now()
			defer l.Commentf("Job API:\t%s\t%s\t%s", r.Method, r.URL.Path, time.Since(t))
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware is a middleware that checks the Authorization header of an incoming request for a Bearer token
// and checks that that token is the correct one.
func AuthMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				writeError(w, errors.New("authorization header is required"), http.StatusUnauthorized)
				return
			}

			authType, reqToken, found := strings.Cut(auth, " ")
			if !found {
				writeError(w, errors.New("invalid authorization header: must be in the form `Bearer <token>`"), http.StatusUnauthorized)
				return
			}

			if authType != "Bearer" {
				writeError(w, errors.New("invalid authorization header: type must be Bearer"), http.StatusUnauthorized)
				return
			}

			if reqToken != token {
				writeError(w, errors.New("invalid authorization token"), http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// HeadersMiddleware is a middleware that sets the common headers for all responses. At the moment, this is just
// Content-Type: application/json.
func HeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer next.ServeHTTP(w, r)

		w.Header().Set("Content-Type", "application/json")
	})
}
