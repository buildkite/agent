package api

import "testing"

func TestRailsPathEscape(t *testing.T) {
	input := "path.segment/containing#various?characters%lol"
	got := railsPathEscape(input)
	want := "path%2Esegment%2Fcontaining%23various%3Fcharacters%25lol"
	if got != want {
		t.Errorf("railsPathEscape(%q) = %q, want %q", input, got, want)
	}
}
