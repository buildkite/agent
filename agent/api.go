// Created by interfacer; DO NOT EDIT

package agent

import (
	"github.com/buildkite/agent/v3/api"
)

// APIClient is an interface generated for "github.com/buildkite/agent/v3/api.Client".
type APIClient interface {
	AcceptJob(*api.Job) (*api.Job, *api.Response, error)
	AcquireJob(string) (*api.Job, *api.Response, error)
	Annotate(string, *api.Annotation) (*api.Response, error)
	Config() api.Config
	Connect() (*api.Response, error)
	CreateArtifacts(string, *api.ArtifactBatch) (*api.ArtifactBatchCreateResponse, *api.Response, error)
	Disconnect() (*api.Response, error)
	ExistsMetaData(string, string) (*api.MetaDataExists, *api.Response, error)
	FinishJob(*api.Job) (*api.Response, error)
	FromAgentRegisterResponse(*api.AgentRegisterResponse) *api.Client
	FromPing(*api.Ping) *api.Client
	GetJobState(string) (*api.JobState, *api.Response, error)
	GetMetaData(string, string) (*api.MetaData, *api.Response, error)
	Heartbeat() (*api.Heartbeat, *api.Response, error)
	Ping() (*api.Ping, *api.Response, error)
	Register(*api.AgentRegisterRequest) (*api.AgentRegisterResponse, *api.Response, error)
	SaveHeaderTimes(string, *api.HeaderTimes) (*api.Response, error)
	SearchArtifacts(string, *api.ArtifactSearchOptions) ([]*api.Artifact, *api.Response, error)
	SetMetaData(string, *api.MetaData) (*api.Response, error)
	StartJob(*api.Job) (*api.Response, error)
	StepUpdate(string, *api.StepUpdate) (*api.Response, error)
	UpdateArtifacts(string, map[string]string) (*api.Response, error)
	UploadChunk(string, *api.Chunk) (*api.Response, error)
	UploadPipeline(string, *api.Pipeline) (*api.Response, error)
}
