package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/buildkite/agent/api"
)

var currentAgentHealth = &agentHealth{}

type lastJobStatus struct {
	ID                string `json:"id"`
	State             string `json:"state"`
	ExitStatus        string `json:"exit_status,omitempty"`
	StartedAt         string `json:"started_at,omitempty"`
	FinishedAt        string `json:"finished_at,omitempty"`
	ChunksFailedCount int    `json:"chunks_failed_count"`
}

func (lj *lastJobStatus) Update(job api.Job) {
	lj.ID = job.ID
	lj.State = job.State
	lj.ExitStatus = job.ExitStatus
	lj.StartedAt = job.StartedAt
	lj.FinishedAt = job.FinishedAt
	lj.ChunksFailedCount = job.ChunksFailedCount
}

type lastAPICall struct {
	Status    string `json:"status,omitempty"`
	TimeTaken string `json:"timeTaken,omitempty"`
}

func (la *lastAPICall) Update(status string, timeTaken time.Duration) {
	la.Status = status
	la.TimeTaken = fmt.Sprintf("%s", timeTaken)
}

type agentHealth struct {
	LastJobStatus *lastJobStatus `json:"lastJobStatus,omitempty"`
	LastAPICall   *lastAPICall   `json:"lastApiCall,omitempty"`
}

type healthResponse struct {
	Response agentHealth `json:"data,omitempty"` // wrapped response
}

// InitHealthCheck setup the health check and ensure it is bound
func InitHealthCheck(addr string) error {

	http.HandleFunc("/status", checkHandler)

	return http.ListenAndServe(addr, nil)
}

func UpdateJobStatus(job api.Job) {
	if currentAgentHealth.LastJobStatus == nil {
		currentAgentHealth.LastJobStatus = &lastJobStatus{}
	}
	currentAgentHealth.LastJobStatus.Update(job)
}

func UpdateAPIStatus(status string, timeTaken time.Duration) {
	if currentAgentHealth.LastAPICall == nil {
		currentAgentHealth.LastAPICall = &lastAPICall{}
	}
	currentAgentHealth.LastAPICall.Update(status, timeTaken)
}

func checkHandler(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(currentAgentHealth)
}
