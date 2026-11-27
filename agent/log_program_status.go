package agent

import (
	"encoding/json"

	"github.com/buildkite/osc7501"
)

// programStatusEvents scans the exact bytes sent to log storage. The scanner
// survives chunks: a split report belongs to the chunk containing its end.
func (ls *LogStreamer) programStatusEvents(data []byte) []byte {
	type record struct {
		ID       string `json:"id"`
		State    string `json:"state"`
		App      string `json:"app,omitempty"`
		Kind     string `json:"kind,omitempty"`
		Progress *int   `json:"progress,omitempty"`
	}
	type event struct {
		Start  uint64 `json:"log_byte_offset"`
		End    uint64 `json:"log_byte_end"`
		Record record `json:"record"`
	}

	const prefix = `{"version":1,"events":[`
	var sidecar []byte
	count := 0
	ls.programStatus.OnReportWithOffsets = func(r osc7501.Report, start, end uint64) {
		// Keep a bounded prefix, not extra chunks or requests that delay logs.
		// Continue scanning after the limit to preserve framing and offsets.
		if count >= 128 {
			return
		}
		rec := record{ID: r.ID, State: r.State, App: r.App, Kind: r.Kind}
		if r.Progress >= 0 {
			rec.Progress = &r.Progress
		}
		encoded, err := json.Marshal(event{Start: start, End: end, Record: rec})
		if err != nil {
			return // Optional metadata must never fail a log upload.
		}
		if count == 0 {
			sidecar = append(sidecar, prefix...)
		}
		separator := 0
		if count > 0 {
			separator = 1
		}
		if len(sidecar)+separator+len(encoded)+len("]}") > 64*1024 {
			count = 128
			return
		}
		if separator != 0 {
			sidecar = append(sidecar, ',')
		}
		sidecar = append(sidecar, encoded...)
		count++
	}
	// io.Discard cannot fail. Incomplete reports remain in the scanner.
	_, _ = ls.programStatus.Write(data)
	ls.programStatus.OnReportWithOffsets = nil
	if len(sidecar) == 0 {
		return nil
	}
	return append(sidecar, ']', '}')
}
