package buildkite

import (
	"github.com/buildkite/agent/logger"
	"strconv"
	"sync"
	"time"
)

type HeaderTimes struct {
	Job   *Job
	API   API
	Times []string

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

type HeaderTimesJSONPayload struct {
	Times map[string]string `json:"header_times"`
}

func (h *HeaderTimes) Now(line string) {
	// logger.Debug("Found header \"%s\", capturing current time", line)

	// Add the current time to our times slice
	h.Times = append(h.Times, time.Now().UTC().Format(time.RFC3339Nano))

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

func (h *HeaderTimes) Upload() {
	// Store the current cursor value
	c := h.cursor

	// Lock the mutex while we figure out what to upload so another routine
	// doesn't try to do it at the same time
	h.mutex.Lock()

	// Grab only the times that we haven't uploaded yet
	length := len(h.Times)
	times := h.Times[h.cursor:length]

	// Construct the payload to send to the server
	payload := HeaderTimesJSONPayload{Times: map[string]string{}}
	for index, time := range times {
		payload.Times[strconv.Itoa(h.cursor+index)] = time
	}

	// Save the new cursor length
	h.cursor = length

	// Unlock the mutex so another routine can kick off a save
	h.mutex.Unlock()

	// How many times are we uploading this time
	timesToUpload := len(times)

	// Do we even have some times to upload
	if timesToUpload > 0 {
		logger.Debug("[HeaderTimes] Uploading %d..%d (%d)", c+1, length, timesToUpload)

		// Send the timings to the API
		h.API.Post("jobs/"+h.Job.ID+"/header_times", &payload, payload)

		// Decrement the wait group for every time we've uploaded
		for _, _ = range times {
			h.waitGroup.Done()
		}
	}
}

func (h *HeaderTimes) Wait() {
	logger.Debug("[HeaderTimes] Waiting for all times to finish uploading")

	h.waitGroup.Wait()
}
