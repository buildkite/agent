package agent

import (
	"context"
	"time"

	"github.com/buildkite/agent/v4/api"
)

// forwardOSCBatch is best effort and memory-only; evidence failure must not fail a job.
func (r *JobRunner) forwardOSCBatch(ctx context.Context, reports []api.OSCEvidence) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for attempt := range 3 {
		resp, err := r.apiClient.ForwardOSC(ctx, r.conf.Job.ID, reports)
		if err == nil {
			r.agentLogger.Infof("[OSC evidence POC] accepted batch reports=%d", len(reports))
			return
		}
		if (resp != nil && !api.IsRetryableStatus(resp)) || ctx.Err() != nil || attempt == 2 {
			r.agentLogger.Warnf("[OSC evidence POC] lost batch reports=%d: %v", len(reports), err)
			return
		}
		select {
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
}
