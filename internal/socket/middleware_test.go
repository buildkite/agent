package socket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func testHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}")) //nolint:errcheck // test handler
}

func shouldCall(t *testing.T) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}
}

func shouldNotCall(t *testing.T) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("next.ServeHTTP should not be called")
		})
	}
}

func TestAuthMiddleware(t *testing.T) {
	t.Parallel()

	token := "llamas"
	cases := []struct {
		title    string
		auth     string
		wantCode int
		wantBody map[string]string
		next     func(http.Handler) http.Handler
	}{
		{
			title:    "valid token",
			auth:     fmt.Sprintf("Bearer %s", token),
			wantCode: http.StatusOK,
			wantBody: map[string]string{},
			next:     shouldCall(t),
		},
		{
			title:    "invalid token",
			auth:     "Bearer alpacas",
			wantCode: http.StatusUnauthorized,
			wantBody: map[string]string{"error": "invalid authorization token"},
			next:     shouldNotCall(t),
		},
		{
			title:    "non-bearer auth",
			auth:     fmt.Sprintf("Basic %s", token),
			wantCode: http.StatusUnauthorized,
			wantBody: map[string]string{"error": "invalid authorization header: type must be Bearer"},
			next:     shouldNotCall(t),
		},
		{
			title:    "no auth",
			auth:     "",
			wantCode: http.StatusUnauthorized,
			wantBody: map[string]string{"error": "authorization header is required"},
			next:     shouldNotCall(t),
		},
	}

	for _, c := range cases {
		t.Run(c.title, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Add("Authorization", c.auth)

			w := httptest.NewRecorder()

			mdlw := AuthMiddleware(token, t.Errorf)
			wrapped := mdlw(c.next(http.HandlerFunc(testHandler)))
			wrapped.ServeHTTP(w, req)

			gotCode := w.Result().StatusCode
			if c.wantCode != gotCode {
				t.Errorf("w.Result().StatusCode = %d (wanted %d)", gotCode, c.wantCode)
			}

			var gotBody map[string]string
			if err := json.NewDecoder(w.Body).Decode(&gotBody); err != nil {
				t.Errorf("json.NewDecoder(w.Body).Decode(&gotBody) = %v", err)
			}

			if diff := cmp.Diff(c.wantBody, gotBody); diff != "" {
				t.Errorf("cmp.Diff(c.wantBody, gotBody) = %s (-want +got)", diff)
			}
		})
	}
}

func TestHeadersMiddleware(t *testing.T) {
	t.Parallel()

	mdlw := HeadersMiddleware(http.Header{"Content-Type": []string{"application/json"}})
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	wrapped := mdlw(http.HandlerFunc(testHandler))
	wrapped.ServeHTTP(w, req)

	gotHeader := w.Header().Get("Content-Type")
	if gotHeader != "application/json" {
		t.Errorf("w.Header().Get(\"Content-Type\") = %s (wanted %s)", gotHeader, "application/json")
	}
}
