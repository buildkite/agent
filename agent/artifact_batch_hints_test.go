package agent

import (
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/stretchr/testify/require"
)

func TestApplyArtifactBatchHintsToJobSetsEnv(t *testing.T) {
	t.Parallel()

	worker := &AgentWorker{}
	worker.setArtifactBatchHintsFromPing(&api.Ping{
		ArtifactCreateBatchSize:    60,
		ArtifactUpdateBatchSizeMax: 240,
	})

	job := &api.Job{}
	worker.applyArtifactBatchHintsToJob(job)

	require.Equal(t, "60", job.Env[artifactCreateBatchSizeEnv])
	require.Equal(t, "240", job.Env[artifactUpdateBatchSizeMaxEnv])
}

func TestSetArtifactBatchHintsFromPingIgnoresZeroValues(t *testing.T) {
	t.Parallel()

	worker := &AgentWorker{}
	worker.setArtifactBatchHintsFromPing(&api.Ping{
		ArtifactCreateBatchSize:    45,
		ArtifactUpdateBatchSizeMax: 180,
	})
	worker.setArtifactBatchHintsFromPing(&api.Ping{})

	createBatchSize, updateBatchSizeMax := worker.artifactBatchHints()
	require.Equal(t, 45, createBatchSize)
	require.Equal(t, 180, updateBatchSizeMax)
}
