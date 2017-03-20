package agent

import (
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/buildkite/agent/logger"
)

var HeaderRegex = regexp.MustCompile("^(?:---|\\+\\+\\+|~~~)\\s(.+)?$")
var ANSIColorRegex = regexp.MustCompile(`\x1b\[([;\d]+)?[mK]`)

type HeaderTimesStreamer struct {
	// The callback that will be called when a header time is ready for
	// upload
	UploadCallback func(int, int, map[string]string)

	// The times that have found while scanning lines
	times      []string
	timesMutex sync.Mutex

	// Every time we get a new time, we increment the wait group, and
	// decrement it after it has been uploaded.
	uploadWaitGroup sync.WaitGroup

	// Every time we go to scan a line, we increment the wait group, then
	// decrement after it's finished scanning. That way when we stop the
	// streamer, we can wait for all the lines to finish scanning first.
	scanWaitGroup sync.WaitGroup

	// A boolean to keep track if we're currently streaming header times
	streaming      bool
	streamingMutex sync.Mutex

	// We store the last index we uploaded to, so we don't have to keep
	// uploading the same times
	cursor int
}

func (h *HeaderTimesStreamer) Start() error {
	h.streaming = true

	go func() {
		logger.Debug("[HeaderTimesStreamer] Streamer has started...")

		for true {
			// Break out of streaming if it's finished. We also
			// need to aquire a read lock on the flag because it
			// can be modified by other routines.
			h.streamingMutex.Lock()
			if !h.streaming {
				break
			}
			h.streamingMutex.Unlock()

			// Upload any pending header times
			h.Upload()

			// Sleep for a second and try upload some more later
			time.Sleep(1 * time.Second)
		}

		logger.Debug("[HeaderTimesStreamer] Streamer has finished...")
	}()

	return nil
}

func (h *HeaderTimesStreamer) Scan(line string) {
	// Keep track of how many line scans we need to do
	h.scanWaitGroup.Add(1)
	defer h.scanWaitGroup.Done()

	logger.Debug("[HeaderTimesStreamer] Found header %q", line)

	// Aquire a lock on the times and then add the current time to
	// our times slice.
	h.timesMutex.Lock()
	h.times = append(h.times, time.Now().UTC().Format(time.RFC3339Nano))
	h.timesMutex.Unlock()

	// Add the time to the wait group
	h.uploadWaitGroup.Add(1)
}

func (h *HeaderTimesStreamer) Upload() {
	// Store the current cursor value
	c := h.cursor

	// Grab only the times that we haven't uploaded yet. We need to aquire
	// a lock since other routines may be adding to it.
	h.timesMutex.Lock()
	length := len(h.times)
	times := h.times[h.cursor:length]
	h.timesMutex.Unlock()

	// Construct the payload to send to the server
	payload := map[string]string{}
	for index, time := range times {
		payload[strconv.Itoa(h.cursor+index)] = time
	}

	// Save the cursor we're up to
	h.cursor = length

	// How many times are we uploading this time
	timesToUpload := len(times)

	// Do we even have some times to upload
	if timesToUpload > 0 {
		// Call our callback with the times for upload
		logger.Debug("[HeaderTimesStreamer] Uploading header times %d..%d", c, length-1)
		h.UploadCallback(c, length, payload)
		logger.Debug("[HeaderTimesStreamer] Finished uploading header times %d..%d", c, length-1)

		// Decrement the wait group for every time we've uploaded.
		h.uploadWaitGroup.Add(timesToUpload * -1)
	}
}

func (h *HeaderTimesStreamer) Stop() {
	logger.Debug("[HeaderTimesStreamer] Waiting for all the lines to be scanned")
	h.scanWaitGroup.Wait()

	logger.Debug("[HeaderTimesStreamer] Waiting for all the header times to be uploaded")
	h.uploadWaitGroup.Wait()

	// Since we're modifying the waitGroup and the streaming flag, we need
	// to aquire a write lock.
	h.streamingMutex.Lock()
	h.streaming = false
	h.streamingMutex.Unlock()
}

func (h *HeaderTimesStreamer) LinePreProcessor(line string) string {
	// Make sure all ANSI colors are removed from the string before we
	// check to see if it's a header (sometimes a color escape sequence may
	// be the first thing on the line, which will cause the regex to ignore
	// it)
	return ANSIColorRegex.ReplaceAllString(line, "")
}

func (h *HeaderTimesStreamer) LineIsHeader(line string) bool {
	// To avoid running the regex over every single line, we'll first do a
	// length check. Hopefully there are no heeaders over 500 characters!
	return len(line) < 500 && HeaderRegex.MatchString(line)
}
