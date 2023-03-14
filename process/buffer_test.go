package process

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestBuffer(t *testing.T) {
	var b Buffer

	// New buffer should be empty.
	if got, want := b.ReadAndTruncate(), []byte(nil); !cmp.Equal(got, want) {
		t.Errorf("b.ReadAndTruncate() = %v, want %v", got, want)
	}

	text := []byte("Kronk! Pull the lever!")
	got, err := b.Write(text)
	if err != nil {
		t.Errorf("b.Write(%q) error = %v", text, err)
	}
	if want := 22; got != want {
		t.Errorf("b.Write(%q) = %d, want %d", text, got, want)
	}

	// ReadAndTruncate should return all current contents.
	if diff := cmp.Diff(b.ReadAndTruncate(), text); diff != "" {
		t.Errorf("b.ReadAndTruncate() diff (-got +want):\n%s", diff)
	}

	// Buffer should now be empty
	if got, want := b.ReadAndTruncate(), []byte(nil); !cmp.Equal(got, want) {
		t.Errorf("b.ReadAndTruncate() = %v, want %v", got, want)
	}
}
