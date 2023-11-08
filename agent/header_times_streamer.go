package agent

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/status"
)

// If you change header parsing here make sure to change it in the
// buildkite.com frontend logic, too

var (
	headerRE          = regexp.MustCompile(`^(---|\+\+\+|~~~)\s`)
	headerExpansionRE = regexp.MustCompile(`^\^\^\^\s\+\+\+`)
	ansiColourRE      = regexp.MustCompile(`\x1b\[([;\d]+)?[mK]`)
)

type headerTimesStreamer struct {
	// The logger instance to use
	logger logger.Logger

	// The callback that will be called when a header time is ready for
	// upload
	uploadCallback func(context.Context, int, int, map[string]string)

	// Closed when the upload loop has ended.
	streamingDoneCh chan struct{}

	// streamingMu guards timesCh and streaming bool.
	// It prevents concurrently sending and closing timesCh.
	streamingMu sync.Mutex
	streaming   bool // track if we're currently streaming header times

	// The header times that have been found while scanning lines.
	//  - Channel sends in Scan (jobRunner.Run goroutine)
	//  - Channel close in Stop (jobRunner.Run goroutine)
	//  - Channel receive in Upload (headerTimesStreamer.Run goroutine)
	timesCh chan string
}

func newHeaderTimesStreamer(l logger.Logger, upload func(context.Context, int, int, map[string]string)) *headerTimesStreamer {
	return &headerTimesStreamer{
		logger:         l,
		uploadCallback: upload,
		// Receive is blocked during uploadCallback, so timesCh is buffered.
		// Capacity of 1000 is a guess at the maximum number of header times a
		// reasonable job would generate during a typical uploadCallback
		// (~x00 milliseconds). If it generates headers more rapidly, or
		// uploadCallback takes a long time, Scan will block, delaying later
		// header times.
		timesCh:         make(chan string, 1000),
		streamingDoneCh: make(chan struct{}),
	}
}

// Run runs the header times streamer (specifically, the part that receives
// header times, batches them, and then uploads them to Buildkite).
// Do not call Run after calling Stop.
func (h *headerTimesStreamer) Run(ctx context.Context) {
	ctx, setStatus, done := status.AddSimpleItem(ctx, "Header Times Streamer")
	defer done()
	setStatus("🏃 Starting...")

	h.streamingMu.Lock()
	h.streaming = true
	h.streamingMu.Unlock()

	defer close(h.streamingDoneCh)

	h.logger.Debug("[HeaderTimesStreamer] Streamer has started...")

	nextIndex := 0
	chanOpen := true

	// Upload loop
	for chanOpen {
		setStatus("⌚️ Waiting 1 second for new header times to put into upload batch...")
		var times []string
		timeout := time.NewTimer(1 * time.Second)
		defer timeout.Stop()

	batchLoop:
		for {
			select {
			case t, open := <-h.timesCh:
				chanOpen = open
				if !open { // timesCh is closed
					break batchLoop
				}
				times = append(times, t)

			case <-timeout.C: // waited long enough for times
				break batchLoop

			case <-ctx.Done(): // pack it all up
				h.logger.Debug("[HeaderTimesStreamer] %v", ctx.Err())
				return
			}
		}

		// Do we even have some times to upload?
		if len(times) == 0 {
			continue
		}

		// Construct the payload to send to the server
		payload := map[string]string{}
		for i, t := range times {
			payload[strconv.Itoa(nextIndex+i)] = t
		}
		startIdx := nextIndex
		nextIndex += len(times)

		// Call our callback with the times for upload
		setStatus(fmt.Sprintf("📡 Uploading %d header times", len(times)))

		h.logger.Debug("[HeaderTimesStreamer] Uploading header times %d..%d", startIdx, nextIndex-1)
		h.uploadCallback(ctx, startIdx, nextIndex, payload)
		h.logger.Debug("[HeaderTimesStreamer] Finished uploading header times %d..%d", startIdx, nextIndex-1)
	}

	setStatus("👋 Finished!")
	h.logger.Debug("[HeaderTimesStreamer] Streamer has finished...")
}

// Scan takes a line of log output and tracks a time if it's a header.
// Returns true for header lines or header expansion lines.
func (h *headerTimesStreamer) Scan(line string) bool {
	// Make sure all ANSI colours are removed from the string before we
	// check to see if it's a header (sometimes a colour escape sequence may
	// be the first thing on the line, which will cause the regex to ignore it)
	line = ansiColourRE.ReplaceAllString(line, "")

	if !headerRE.MatchString(line) {
		// It's not a header, but could be a header expansion.
		return headerExpansionRE.MatchString(line)
	}

	h.logger.Debug("[HeaderTimesStreamer] Found header %q", line)

	// Use mutex to prevent concurrently sending and closing the channel.
	h.streamingMu.Lock()
	defer h.streamingMu.Unlock()

	if h.streaming {
		h.timesCh <- time.Now().UTC().Format(time.RFC3339Nano)
	}
	return true
}

// Stop stops the header time streamer. This should only be called after the
// logs have stopped being generated (the process has ended).
func (h *headerTimesStreamer) Stop() {
	h.streamingMu.Lock()
	if !h.streaming {
		// Already stopped, or never started.
		h.streamingMu.Unlock()
		return
	}
	close(h.timesCh)
	h.streamingMu.Unlock()

	h.logger.Debug("[HeaderTimesStreamer] Waiting for all the header times to be uploaded")
	<-h.streamingDoneCh
}
