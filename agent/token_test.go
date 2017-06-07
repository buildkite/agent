package agent

import "testing"

func TestStringTokenErrorsWhenEmpty(t *testing.T) {
	tok, err := StringToken("").Read()

	if err != errEmptyToken {
		t.Fatalf("Read should fail when token empty")
	}

	if tok != "" {
		t.Fatalf("Read should return empty string when token empty")
	}
}
