package joberrorosc_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/joberrorosc"
	"github.com/google/go-cmp/cmp"
	"github.com/wolfeidau/osc7501"
)

func TestErrorRoundTrip(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		"Hook failed\n\nLast output:\n\tpermission denied\r\n",
		strings.Repeat("🦘", 999) + "!", // 1000 characters, well over OSC's byte limit
		strings.Repeat("\x01", 1000),   // JSON escaping expands this substantially
	} {
		want := api.JobCapturedError{
			Code: strings.Repeat("c", 255), Message: message,
			Timestamp:      time.Date(2026, 10, 22, 10, 11, 12, 345678901, time.FixedZone("AEST", 10*60*60)),
			IdempotencyKey: "a-delivery-key",
		}
		var wire bytes.Buffer
		if err := joberrorosc.Write(&wire, want); err != nil {
			t.Fatal(err)
		}
		for _, size := range []int{1, 17, 4096} {
			var output bytes.Buffer
			var got []api.JobCapturedError
			w := joberrorosc.NewWriter(&output, func(e api.JobCapturedError) { got = append(got, e) })
			input := append([]byte("before\n"), wire.Bytes()...)
			input = append(input, []byte("after\n")...)
			for remaining := input; len(remaining) > 0; {
				n := min(size, len(remaining))
				if _, err := w.Write(remaining[:n]); err != nil {
					t.Fatal(err)
				}
				remaining = remaining[n:]
			}
			if diff := cmp.Diff([]api.JobCapturedError{want}, got); diff != "" {
				t.Fatalf("write size %d (-want +got):\n%s", size, diff)
			}
			if !bytes.Equal(output.Bytes(), input) {
				t.Fatalf("write size %d changed the log", size)
			}
		}
	}
}

func TestOnlyCompleteErrorEnvelopesAreExtracted(t *testing.T) {
	t.Parallel()
	var wire bytes.Buffer
	e := api.JobCapturedError{Code: "plugin.failed", Message: strings.Repeat("界", 1000), Timestamp: time.Now(), IdempotencyKey: "event"}
	if err := joberrorosc.Write(&wire, e); err != nil {
		t.Fatal(err)
	}
	var parts []osc7501.Report
	s := osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) { parts = append(parts, r) }}
	if _, err := s.Write(wire.Bytes()); err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 {
		t.Fatal("expected a multipart error")
	}
	var got []api.JobCapturedError
	w := joberrorosc.NewWriter(io.Discard, func(e api.JobCapturedError) { got = append(got, e) })
	// Missing first/last parts, out-of-order parts, and malformed metadata must
	// not turn partial JSON into API calls. Ordinary status errors aren't job errors.
	bad := []osc7501.Report{
		parts[1], parts[0], parts[len(parts)-1],
		{State: "error", App: "buildkite-agent", Msg: "command failed"},
		{State: "error", App: "buildkite-job-error", ID: "job-error/v1/bad", Title: "1/999", Msg: "e30="},
		{State: "error", App: "buildkite-job-error", ID: "job-error/v2/future", Title: "1/1", Msg: "e30="},
		{State: "error", App: "buildkite-job-error", ID: "job-error/v1/bad", Title: "1/1", Msg: "not-json"},
	}
	for _, r := range bad {
		if _, err := w.Write(r.Encode()); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 0 {
		t.Fatalf("invalid envelopes emitted %d errors", len(got))
	}
	// An incomplete preceding error must not prevent the next complete one.
	if _, err := w.Write(wire.Bytes()); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]api.JobCapturedError{e}, got); diff != "" {
		t.Fatal(diff)
	}
}
