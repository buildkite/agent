package vmsandbox

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestConnRoundTrip(t *testing.T) {
	t.Parallel()

	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	host, guest := NewConn(a), NewConn(b)

	sent := []Message{
		{
			Type:        TypeStart,
			Env:         []string{"SECRET=line one\nline two\n", "EMPTY="},
			EnvFile:     "SECRET=\"line one\\nline two\\n\"\n",
			EnvJSONFile: `{"SECRET":"line one\nline two\n"}` + "\n",
		},
		{Type: TypeInterrupt, TimedOut: true},
		{Type: TypePowerOff},
	}
	go func() {
		for _, m := range sent {
			if err := host.Send(m); err != nil {
				t.Errorf("Send(%v) error = %v", m.Type, err)
				return
			}
		}
		_ = a.Close()
	}()

	var got []Message
	for {
		m, err := guest.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv error = %v", err)
		}
		got = append(got, m)
	}
	if diff := cmp.Diff(sent, got); diff != "" {
		t.Errorf("messages diff (-sent +got):\n%s", diff)
	}
}

// Output chunks can contain anything, including newlines and bytes that
// aren't valid UTF-8; the framing must keep them intact and separate.
func TestOutputWriterFraming(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := outputWriter{NewConn(&buf)}
	chunks := [][]byte{
		[]byte("hello\n"),
		{0xff, 0xfe, '\n', '\n', 0x00},
		[]byte("\"quoted\" and }braces{"),
	}
	for _, c := range chunks {
		n, err := w.Write(c)
		if err != nil || n != len(c) {
			t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(c))
		}
	}

	r := NewConn(&buf)
	for i, want := range chunks {
		m, err := r.Recv()
		if err != nil {
			t.Fatalf("Recv #%d error = %v", i, err)
		}
		if m.Type != TypeOutput || !bytes.Equal(m.Data, want) {
			t.Errorf("Recv #%d = %q %q, want %q %q", i, m.Type, m.Data, TypeOutput, want)
		}
	}
	if _, err := r.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("trailing Recv error = %v, want io.EOF", err)
	}
}

func TestConnRecv_TruncatedIsNotEOF(t *testing.T) {
	t.Parallel()

	// A connection that dies mid-message must not be mistaken for a clean
	// end of stream.
	r := NewConn(bytes.NewBufferString(`{"t":"x","s":`))
	_, err := r.Recv()
	if err == nil || !strings.Contains(err.Error(), "truncated") {
		t.Errorf("Recv error = %v, want a truncation error", err)
	}
}
