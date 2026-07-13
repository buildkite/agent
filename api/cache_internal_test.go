package api

import "testing"

func TestIsJSONContent(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{"basic JSON", "application/json", true},
		{"JSON with charset", "application/json; charset=utf-8", true},
		{"JSON with charset uppercase", "APPLICATION/JSON; CHARSET=UTF-8", true},
		{"JSON with additional params", "application/json; charset=utf-8; boundary=something", true},
		{"JSON with spaces", "  application/json  ", true},
		{"JSON with spaces and charset", "  application/json; charset=utf-8  ", true},
		{"problem+json", "application/problem+json", true},
		{"text plain", "text/plain", false},
		{"HTML", "text/html", false},
		{"XML", "application/xml", false},
		{"empty", "", false},
		{"partial match", "json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isJSONContent(tt.contentType); got != tt.want {
				t.Errorf("isJSONContent(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}
