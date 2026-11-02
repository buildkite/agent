package clicommand

import (
	"github.com/buildkite/agent/v4/internal/programstatus"
	"github.com/dustin/go-humanize/english"
)

// uploadStatus reports an upload command's progress on the root program
// status record, such as "Uploading 3 artifacts" then "Uploaded 3 artifacts".
// It does nothing if its Reporter is nil.
type uploadStatus struct {
	reporter *programstatus.Reporter
	noun     string // what's uploaded, such as "artifact"
	emptyMsg string // the done message when nothing was uploaded, if any
	errorMsg string

	uploaded int
	percent  int // the last progress reported
}

func newUploadStatus(r *programstatus.Reporter, noun, emptyMsg, errorMsg string) *uploadStatus {
	return &uploadStatus{reporter: r, noun: noun, emptyMsg: emptyMsg, errorMsg: errorMsg, percent: -1}
}

// start reports that an upload of unknown size is running.
func (s *uploadStatus) start(msg string) {
	s.reporter.Working("", -1, msg)
}

// progress records that uploaded of total items have been uploaded. It only
// reports when the whole percentage changes, so uploading thousands of items
// doesn't send thousands of reports. total must be positive.
func (s *uploadStatus) progress(uploaded, total int) {
	s.uploaded = uploaded
	if p := uploaded * 100 / total; p != s.percent {
		s.percent = p
		s.reporter.Working("", p, "Uploading "+english.Plural(total, s.noun, ""))
	}
}

// uploadedOne records that one more item has been uploaded.
func (s *uploadStatus) uploadedOne() {
	s.uploaded++
}

// finish reports how the upload ended.
func (s *uploadStatus) finish(err error) {
	doneMsg := "Uploaded " + english.Plural(s.uploaded, s.noun, "")
	if s.uploaded == 0 && s.emptyMsg != "" {
		doneMsg = s.emptyMsg
	}
	s.reporter.Finish("", err, doneMsg, s.errorMsg)
}
