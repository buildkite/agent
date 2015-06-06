package agent

import (
	"regexp"
	"strconv"
	"sync"
	"time"
)

var HeaderRegex = regexp.MustCompile("^(?:---|\\+\\+\\+|~~~)\\s(.+)?$")

type HeaderTimesStreamer struct {
	// The callback that will be called when a header time is ready for
	// upload
	Callback func(int, int, map[string]string)

	// The times that have found while scanning lines
	times []string

	// Every time we get a new time, we increment the wait group, and
	// decrement it after it has been uploaded.
	waitGroup sync.WaitGroup

	// We store the last index we uploaded to, so we don't have to keep
	// uploading the same times
	cursor int

	// A boolean to keep track if a save has been scheduled
	scheduled bool

	// When calculating the delta between the previous upload, and the
	// current upload, we lock this mutex so multiple routines don't try
	// and calculate the deltas at the same time (which could lead to weird
	// things happening).
	mutex sync.Mutex
}

func (h *HeaderTimesStreamer) Scan(line string) {
	// To avoid running the regex over every single line, we'll first do a
	// length check. Hopefully there are no heeaders over 500 characters!
	if len(line) < 500 && HeaderRegex.MatchString(line) {
		go h.Now(line)
	}
}

func (h *HeaderTimesStreamer) Now(line string) {
	// logger.Debug("Found header \"%s\", capturing current time", line)

	// Add the current time to our times slice
	h.times = append(h.times, time.Now().UTC().Format(time.RFC3339Nano))

	// Add the time to the wait group
	h.waitGroup.Add(1)

	// Wait for a second to see if any more header times come in, and then
	// save. This is super hacky way of implementing a throttler.
	if !h.scheduled {
		h.scheduled = true

		time.AfterFunc(time.Second*1, func() {
			h.scheduled = false
			go h.Upload()
		})
	}
}

func (h *HeaderTimesStreamer) Upload() {
	// Store the current cursor value
	c := h.cursor

	// Lock the mutex while we figure out what to upload so another routine
	// doesn't try to do it at the same time
	h.mutex.Lock()

	// Grab only the times that we haven't uploaded yet
	length := len(h.times)
	times := h.times[h.cursor:length]

	// Construct the payload to send to the server
	payload := map[string]string{}
	for index, time := range times {
		payload[strconv.Itoa(h.cursor+index)] = time
	}

	// Save the new cursor length
	h.cursor = length

	// Unlock the mutex so another routine can kick off a save
	h.mutex.Unlock()

	// How many times are we uploading this time
	timesToUpload := len(times)

	// Do we even have some times to upload
	if timesToUpload > 0 {
		// Call our callback with the times for upload
		h.Callback(c, length, payload)

		// Decrement the wait group for every time we've uploaded
		for _, _ = range times {
			h.waitGroup.Done()
		}
	}
}

func (h *HeaderTimesStreamer) Wait() {
	h.waitGroup.Wait()
}
