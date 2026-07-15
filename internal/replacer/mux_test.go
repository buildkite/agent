package replacer

import (
	"io"
	"slices"
	"testing"
)

func passthrough(b []byte) []byte { return b }

func TestMuxNeedlesDeduplicates(t *testing.T) {
	t.Parallel()

	r1 := New(io.Discard, []string{"alpha", "bravo"}, passthrough)
	r2 := New(io.Discard, []string{"bravo", "charlie"}, passthrough)
	m := NewMux(r1, r2)

	got := m.Needles()
	slices.Sort(got)
	want := []string{"alpha", "bravo", "charlie"}
	if !slices.Equal(got, want) {
		t.Errorf("Mux.Needles() = %v, want %v (deduplicated across replacers)", got, want)
	}
}

func TestMuxNeedlesEmpty(t *testing.T) {
	t.Parallel()

	if got := NewMux().Needles(); len(got) != 0 {
		t.Errorf("empty Mux.Needles() = %v, want empty", got)
	}
}
