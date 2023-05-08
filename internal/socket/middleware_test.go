package socket

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func testHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
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
