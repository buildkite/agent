package jobapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/agent/v3/jobapi"
	"github.com/google/go-cmp/cmp"
)

func testHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

func TestAuthMdlw(t *testing.T) {
	t.Parallel()

	token := "llamas"
	cases := []struct {
		title    string
		auth     string
		wantCode int
		wantBody map[string]string
	}{
		{
			title:    "valid token",
			auth:     fmt.Sprintf("Bearer %s", token),
			wantCode: http.StatusOK,
			wantBody: map[string]string{},
		},
		{
			title:    "invalid token",
			auth:     "Bearer alpacas",
			wantCode: http.StatusUnauthorized,
			wantBody: map[string]string{"error": "invalid authorization token"},
		},
		{
			title:    "non-bearer auth",
			auth:     fmt.Sprintf("Basic %s", token),
			wantCode: http.StatusUnauthorized,
			wantBody: map[string]string{"error": "invalid authorization header: type must be Bearer"},
		},
		{
			title:    "no auth",
			auth:     "",
			wantCode: http.StatusUnauthorized,
			wantBody: map[string]string{"error": "authorization header is required"},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.title, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Add("Authorization", c.auth)

			w := httptest.NewRecorder()

			mdlw := jobapi.AuthMiddleware(token)
			wrapped := mdlw(http.HandlerFunc(testHandler))
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

func TestHeadersMdlw(t *testing.T) {
	t.Parallel()

	mdlw := jobapi.HeadersMiddleware()
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	wrapped := mdlw(http.HandlerFunc(testHandler))
	wrapped.ServeHTTP(w, req)

	gotHeader := w.Header().Get("Content-Type")
	if gotHeader != "application/json" {
		t.Errorf("w.Header().Get(\"Content-Type\") = %s (wanted %s)", gotHeader, "application/json")
	}
}
