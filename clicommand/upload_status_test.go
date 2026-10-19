package clicommand

import (
	"errors"
	"io"
	"testing"

	"github.com/buildkite/agent/v4/internal/programstatus"
	"github.com/google/go-cmp/cmp"
	"github.com/wolfeidau/osc7501"
)

func TestUploadStatus(t *testing.T) {
	t.Parallel()

	working := func(progress int, msg string) osc7501.Report {
		return osc7501.Report{State: "working", App: programstatus.App, Progress: progress, Msg: msg}
	}
	finished := func(state, msg string) osc7501.Report {
		return osc7501.Report{State: state, App: programstatus.App, Progress: -1, Msg: msg}
	}

	tests := []struct {
		name string
		noun string
		run  func(s *uploadStatus)
		want []osc7501.Report
	}{
		{
			name: "progress only reports whole percentage changes",
			noun: "artifact",
			run: func(s *uploadStatus) {
				for _, uploaded := range []int{0, 1, 10, 11, 1000} {
					s.progress(uploaded, 1000)
				}
				s.finish(nil)
			},
			want: []osc7501.Report{
				working(0, "Uploading 1,000 artifacts"),
				working(1, "Uploading 1,000 artifacts"),
				working(100, "Uploading 1,000 artifacts"),
				finished("done", "Uploaded 1,000 artifacts"),
			},
		},
		{
			name: "one artifact",
			noun: "artifact",
			run: func(s *uploadStatus) {
				s.progress(0, 1)
				s.progress(1, 1)
				s.finish(nil)
			},
			want: []osc7501.Report{
				working(0, "Uploading 1 artifact"),
				working(100, "Uploading 1 artifact"),
				finished("done", "Uploaded 1 artifact"),
			},
		},
		{
			name: "nothing uploaded",
			noun: "artifact",
			run:  func(s *uploadStatus) { s.finish(nil) },
			want: []osc7501.Report{finished("done", "Nothing to upload")},
		},
		{
			name: "uploads counted one at a time",
			noun: "pipeline",
			run: func(s *uploadStatus) {
				s.start("Uploading pipeline from a.yml")
				s.uploadedOne()
				s.start("Uploading pipeline from b.yml")
				s.uploadedOne()
				s.finish(nil)
			},
			want: []osc7501.Report{
				working(-1, "Uploading pipeline from a.yml"),
				working(-1, "Uploading pipeline from b.yml"),
				finished("done", "Uploaded 2 pipelines"),
			},
		},
		{
			name: "failed",
			noun: "pipeline",
			run: func(s *uploadStatus) {
				s.start("Uploading pipeline from a.yml")
				s.finish(errors.New("boom"))
			},
			want: []osc7501.Report{
				working(-1, "Uploading pipeline from a.yml"),
				finished("error", "Upload failed"),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var got []osc7501.Report
			scanner := &osc7501.Scanner{W: io.Discard, OnReport: func(r osc7501.Report) { got = append(got, r) }}
			test.run(newUploadStatus(programstatus.New(scanner), test.noun, "Nothing to upload", "Upload failed"))
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("reports diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUploadStatusWithoutReporter(t *testing.T) {
	t.Parallel()

	// Reporting is off when the Reporter is nil, so nothing should panic.
	s := newUploadStatus(nil, "artifact", "", "Upload failed")
	s.start("Uploading")
	s.progress(1, 2)
	s.uploadedOne()
	s.finish(nil)
}
