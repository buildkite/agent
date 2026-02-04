package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/api"
)

// JobController provides simple functionality to a job runner - the ability to
// write job logs and exit with a status code.
type JobController struct {
	client *Client
	job    *api.Job

	// Log chunk sequence
	writeLogMu       sync.Mutex
	sequence, offset uint64
}

// NewJobController creates a new job controller for a job.
func NewJobController(client *Client, job *api.Job) *JobController {
	return &JobController{
		client: client,
		job:    job,
	}
}

// WriteLog writes some log content for this job back to Buildkite.
// Unlike the main agent, this immediately uploads the content without any
// buffering, does not tee the log to a local file, nor does it insert
// timestamp codes. However, it uses the same retry loop as the agent, which
// can attempt to upload logs for a very long time.
func (c *JobController) WriteLog(ctx context.Context, log string) error {
	c.writeLogMu.Lock()
	defer c.writeLogMu.Unlock()

	size := uint64(len(log))
	err := c.client.UploadChunk(ctx, c.job.ID, &api.Chunk{
		Data:     []byte(log),
		Sequence: c.sequence,
		Offset:   c.offset,
		Size:     size,
	})
	if err != nil {
		return err
	}
	c.sequence++
	c.offset += size
	return nil
}

// Start marks a job as started in Buildkite. If an error is returned, the job
// should not proceed.
func (c *JobController) Start(ctx context.Context) error {
	return c.client.StartJob(ctx, c.job, time.Now())
}

// Finish marks the job as finished in Buildkite.
func (c *JobController) Finish(ctx context.Context, exit ProcessExit, ignoreAgentInDispatches *bool) error {
	return c.client.FinishJob(ctx, c.job, time.Now(), exit, 0, ignoreAgentInDispatches)
}

// BKTimestamp formats a time as a Buildkite timestamp code.
func BKTimestamp(t time.Time) string {
	return fmt.Sprintf("\x1b_bk;t=%d\x07", t.UnixNano()/int64(time.Millisecond))
}
