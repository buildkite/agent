package process

import (
	"io"
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

func TestBufferClose(t *testing.T) {
	var b Buffer

	if err := b.Close(); err != nil {
		t.Errorf("initial b.Close() = %v", err)
	}

	gotN, gotErr := b.Write([]byte("This shouldn't work"))
	wantN, wantErr := 0, io.ErrClosedPipe
	if gotN != wantN || gotErr != wantErr {
		t.Errorf("after b.Close(): b.Write() = (%d, %v), want (%d, %v)", gotN, gotErr, wantN, wantErr)
	}

	if err := b.Close(); err != ErrAlreadyClosed {
		t.Errorf("double b.Close() = %v, want %v", err, ErrAlreadyClosed)
	}
}
