package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"
	"github.com/buildkite/agent/v3/api"
	agentedgev1 "github.com/buildkite/agent/v3/api/proto/gen"
	"github.com/buildkite/agent/v3/api/proto/gen/agentedgev1connect"
	"github.com/google/uuid"
)

// This file implements a flexible fake testing server for the agent, including
// registration through to pinging and running jobs, but not including many of
// the other things the agent REST API does.
//
// Why not fake the client? Because there's a lot of value in testing that
// real requests round-trip through a network connection, even if both endpoints
// are the same process.

const (
	JobStateScheduled = "scheduled"
	JobStateAssigned  = "assigned"
	JobStateAccepted  = "accepted"
	JobStateRunning   = "running"
	JobStateFinished  = "finished"
)

type FakeJob struct {
	Job   *api.Job
	Auth  string
	State string
	Logs  strings.Builder
}

type FakeAgent struct {
	Assigned           *FakeJob
	Paused             bool
	Stop               bool
	Pings              int
	Heartbeats         int
	IgnoreInDispatches bool

	PingHandler func(*http.Request) (api.Ping, error)

	// PingStream is a simple way of providing streaming responses concurrently.
	// It is used for the default handler.
	PingStream chan *agentedgev1.StreamPingsResponse

	// PingStreamHandler provides more flexibility in how the streaming request
	// is handled. Setting PingStreamHandler overrides the default handler.
	PingStreamHandler func(context.Context, *connect.Request[agentedgev1.StreamPingsRequest], *connect.ServerStream[agentedgev1.StreamPingsResponse]) error
}

// agentJob is just an agent/job tuple.
type agentJob struct {
	agent *FakeAgent
	job   *FakeJob
}

type fakeAPIServerOption = func(*FakeAPIServer, *http.ServeMux)

// FakeAPIServer implements a fake Agent REST API server for testing.
type FakeAPIServer struct {
	*httptest.Server

	agentsMu sync.Mutex
	agents   map[string]*FakeAgent // session token Auth header -> agent

	mu            sync.Mutex
	jobs          map[string]*FakeJob                   // uuid -> job
	agentJobs     map[string]agentJob                   // job token Auth header -> (agent, job)
	registrations map[string]*api.AgentRegisterResponse // reg token Auth header -> response
}

// NewFakeAPIServer constructs a new FakeAPIServer for testing.
func NewFakeAPIServer(opts ...fakeAPIServerOption) *FakeAPIServer {
	fs := &FakeAPIServer{
		agents:        make(map[string]*FakeAgent),
		jobs:          make(map[string]*FakeJob),
		agentJobs:     make(map[string]agentJob),
		registrations: make(map[string]*api.AgentRegisterResponse),
	}
	mux := http.NewServeMux()
	for _, opt := range opts {
		opt(fs, mux)
	}
	mux.HandleFunc("PUT /jobs/{job_uuid}/acquire", fs.handleJobAcquire)
	mux.HandleFunc("PUT /jobs/{job_uuid}/accept", fs.handleJobAccept)
	mux.HandleFunc("PUT /jobs/{job_uuid}/start", fs.handleJobStart)
	mux.HandleFunc("PUT /jobs/{job_uuid}/finish", fs.handleJobFinish)
	mux.HandleFunc("POST /jobs/{job_uuid}/chunks", fs.handleJobChunks)
	mux.HandleFunc("GET /ping", fs.handlePing)
	mux.HandleFunc("POST /heartbeat", fs.handleHeartbeat)
	mux.HandleFunc("POST /register", fs.handleRegister)
	fs.Server = httptest.NewServer(mux)
	return fs
}

// WithStreaming enables the ping streaming API for the fake server.
func WithStreaming(fs *FakeAPIServer, mux *http.ServeMux) {
	mux.Handle(agentedgev1connect.NewAgentEdgeServiceHandler(fs))
}

func (fs *FakeAPIServer) StreamPings(ctx context.Context, req *connect.Request[agentedgev1.StreamPingsRequest], resp *connect.ServerStream[agentedgev1.StreamPingsResponse]) error {
	auth := req.Header().Get("Authorization")
	agent := fs.agentForAuth(auth)
	if agent == nil {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("invalid Authorization header value %q", auth))
	}

	if agent.PingStreamHandler != nil {
		return agent.PingStreamHandler(ctx, req, resp)
	}

	for p := range agent.PingStream {
		if err := resp.Send(p); err != nil {
			return connect.NewError(connect.CodeUnknown, err)
		}
	}
	return nil
}

func (fs *FakeAPIServer) AddAgent(token string) *FakeAgent {
	fs.agentsMu.Lock()
	defer fs.agentsMu.Unlock()
	a := &FakeAgent{
		PingStream: make(chan *agentedgev1.StreamPingsResponse),
	}
	fs.agents["Token "+token] = a
	return a
}

func (fs *FakeAPIServer) DeleteAgent(token string) {
	fs.agentsMu.Lock()
	defer fs.agentsMu.Unlock()
	delete(fs.agents, "Token "+token)
}

func (fs *FakeAPIServer) agentForAuth(auth string) *FakeAgent {
	fs.agentsMu.Lock()
	defer fs.agentsMu.Unlock()
	return fs.agents[auth]
}

func (fs *FakeAPIServer) AddJob(env map[string]string) *FakeJob {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	jobToken := uuid.New().String()
	j := &FakeJob{
		State: JobStateScheduled,
		Job: &api.Job{
			ID:                 uuid.New().String(),
			Token:              jobToken,
			ChunksMaxSizeBytes: 1024,
			Env:                env,
		},
		Auth: "Token " + jobToken,
	}
	fs.jobs[j.Job.ID] = j
	return j
}

func (fs *FakeAPIServer) Assign(agent *FakeAgent, job *FakeJob) {
	fs.agentsMu.Lock()
	fs.mu.Lock()
	defer fs.mu.Unlock()
	defer fs.agentsMu.Unlock()
	fs.assignNoMutex(agent, job)
}

func (fs *FakeAPIServer) assignNoMutex(agent *FakeAgent, job *FakeJob) {
	agent.Assigned = job
	job.State = JobStateAssigned
	fs.agentJobs[job.Auth] = agentJob{
		agent: agent,
		job:   job,
	}
}

func (fs *FakeAPIServer) AddRegistration(token string, reg *api.AgentRegisterResponse) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.registrations["Token "+token] = reg
}

func (fs *FakeAPIServer) handleJobAcquire(rw http.ResponseWriter, req *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// The agent doesn't know the job token yet, so it must use the session
	// token.
	auth := req.Header.Get("Authorization")
	agent := fs.agentForAuth(auth)
	if agent == nil {
		http.Error(rw, encodeMsgf("invalid Authorization header value %q", auth), http.StatusUnauthorized)
		return
	}

	jobUUID := req.PathValue("job_uuid")
	job := fs.jobs[jobUUID]
	if job == nil {
		http.Error(rw, encodeMsgf("job UUID %q not found", jobUUID), http.StatusNotFound)
		return
	}

	if got, want := job.State, JobStateScheduled; got != want {
		http.Error(rw, encodeMsgf("job in invalid state for acquire [%q != %q]", got, want), http.StatusUnprocessableEntity)
		return
	}

	if req.Header.Get("X-Buildkite-Lock-Acquire-Job") != "1" {
		http.Error(rw, "Expected X-Buildkite-Lock-Acquire-Job to be set to 1", http.StatusUnprocessableEntity)
		return
	}

	// job is assigned to this agent, accepted, and is now accessible using a
	// job token.
	fs.assignNoMutex(agent, job) // we're within the mutex, see above
	job.State = JobStateAccepted

	out, err := json.Marshal(job.Job)
	if err != nil {
		http.Error(rw, encodeMsgf("json.Marshal(%v) = %v", job.Job, err), http.StatusInternalServerError)
		return
	}
	rw.Write(out) //nolint:errcheck // Test should fail on incomplete response.
}

func (fs *FakeAPIServer) handleJobAccept(rw http.ResponseWriter, req *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	// The agent has the job info from the ping, but accepts as itself.
	auth := req.Header.Get("Authorization")
	agent := fs.agentForAuth(auth)
	if agent == nil {
		http.Error(rw, encodeMsgf("invalid Authorization header value %q", auth), http.StatusUnauthorized)
		return
	}

	jobUUID := req.PathValue("job_uuid")
	job := fs.jobs[jobUUID]
	if job == nil {
		http.Error(rw, encodeMsgf("job UUID %q not found", jobUUID), http.StatusNotFound)
		return
	}

	if got, want := job.State, JobStateAssigned; got != want {
		http.Error(rw, encodeMsgf("job in invalid state for accept [%q != %q]", got, want), http.StatusUnprocessableEntity)
		return
	}

	job.State = JobStateAccepted

	out, err := json.Marshal(job.Job)
	if err != nil {
		http.Error(rw, encodeMsgf("json.Marshal(%v) = %v", job.Job, err), http.StatusInternalServerError)
		return
	}
	rw.Write(out) //nolint:errcheck // Test should fail on incomplete response.
}

func (fs *FakeAPIServer) handleJobStart(rw http.ResponseWriter, req *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	auth := req.Header.Get("Authorization")
	aj, found := fs.agentJobs[req.Header.Get("Authorization")]
	if !found {
		http.Error(rw, encodeMsgf("invalid Authorization header value %q", auth), http.StatusUnauthorized)
		return
	}

	job := aj.job

	if got, want := job.Job.ID, req.PathValue("job_uuid"); got != want {
		http.Error(rw, encodeMsgf("job UUID mismatch [%q != %q]", got, want), http.StatusNotFound)
		return
	}

	if got, want := job.State, JobStateAccepted; got != want {
		http.Error(rw, encodeMsgf("job in invalid state for start [%q != %q]", got, want), http.StatusUnprocessableEntity)
		return
	}

	job.State = JobStateRunning

	rw.Write([]byte("{}")) //nolint:errcheck // Test should fail on incomplete response.
}

func (fs *FakeAPIServer) handleJobFinish(rw http.ResponseWriter, req *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	auth := req.Header.Get("Authorization")
	aj, found := fs.agentJobs[req.Header.Get("Authorization")]
	if !found {
		http.Error(rw, encodeMsgf("invalid Authorization header value %q", auth), http.StatusUnauthorized)
		return
	}

	agent := aj.agent
	job := aj.job

	if got, want := job.Job.ID, req.PathValue("job_uuid"); got != want {
		http.Error(rw, encodeMsgf("job UUID mismatch [%q != %q]", got, want), http.StatusNotFound)
		return
	}

	if got, want := job.State, JobStateRunning; got != want {
		http.Error(rw, encodeMsgf("job in invalid state for finish [%q != %q]", got, want), http.StatusUnprocessableEntity)
		return
	}

	var finishReq api.JobFinishRequest
	if err := json.NewDecoder(req.Body).Decode(&finishReq); err != nil {
		http.Error(rw, encodeMsgf("couldn't decde request body: %v", err), http.StatusBadRequest)
		return
	}

	job.State = JobStateFinished
	agent.Assigned = nil

	if ignore := finishReq.IgnoreAgentInDispatches; ignore != nil {
		agent.IgnoreInDispatches = *ignore
	}

	rw.Write([]byte("{}")) //nolint:errcheck // Test should fail on incomplete response.
}

func (fs *FakeAPIServer) handleJobChunks(rw http.ResponseWriter, req *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	auth := req.Header.Get("Authorization")
	aj, found := fs.agentJobs[req.Header.Get("Authorization")]
	if !found {
		http.Error(rw, encodeMsgf("invalid Authorization header value %q", auth), http.StatusUnauthorized)
		return
	}

	job := aj.job

	if got, want := job.Job.ID, req.PathValue("job_uuid"); got != want {
		http.Error(rw, encodeMsgf("job UUID mismatch [%q != %q]", got, want), http.StatusNotFound)
		return
	}

	if got, want := job.State, JobStateRunning; got != want {
		http.Error(rw, encodeMsgf("job in invalid state for chunks [%q != %q]", got, want), http.StatusUnprocessableEntity)
		return
	}

	// TODO: do the right thing for out of order chunks
	if _, err := io.Copy(&job.Logs, req.Body); err != nil {
		http.Error(rw, encodeMsgf("incomplete stream: %v", err), http.StatusBadRequest)
		return
	}

	rw.WriteHeader(http.StatusOK)
}

func (fs *FakeAPIServer) handlePing(rw http.ResponseWriter, req *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var ping api.Ping

	auth := req.Header.Get("Authorization")
	agent := fs.agentForAuth(auth)
	if agent == nil {
		http.Error(rw, encodeMsgf("invalid Authorization header value %q", auth), http.StatusUnauthorized)
		return
	}

	switch {
	case agent.PingHandler != nil:
		resp, err := agent.PingHandler(req)
		if err != nil {
			http.Error(rw, encodeMsg(err), http.StatusUnprocessableEntity)
			return
		}
		ping = resp

	case agent.Assigned != nil:
		ping = api.Ping{
			Job: agent.Assigned.Job,
		}

	case agent.Paused:
		ping = api.Ping{
			Action: "pause",
		}

	case agent.Stop:
		ping = api.Ping{
			Action: "disconnect",
		}
	}
	agent.Pings++

	out, err := json.Marshal(ping)
	if err != nil {
		http.Error(rw, encodeMsgf("json.Marshal(%v) = %v", ping, err), http.StatusInternalServerError)
		return
	}
	rw.Write(out) //nolint:errcheck // Test should fail on incomplete response.
}

func (fs *FakeAPIServer) handleHeartbeat(rw http.ResponseWriter, req *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	auth := req.Header.Get("Authorization")
	agent := fs.agentForAuth(auth)
	if agent == nil {
		http.Error(rw, encodeMsgf("invalid Authorization header value %q", auth), http.StatusUnauthorized)
		return
	}

	agent.Heartbeats++

	var hb api.Heartbeat
	if err := json.NewDecoder(req.Body).Decode(&hb); err != nil {
		http.Error(rw, encodeMsg(err), http.StatusBadRequest)
		return
	}
	hb.ReceivedAt = time.Now().Format(time.RFC3339)

	out, err := json.Marshal(hb)
	if err != nil {
		http.Error(rw, encodeMsgf("json.Marshal(%v) = %v", hb, err), http.StatusInternalServerError)
		return
	}
	rw.Write(out) //nolint:errcheck // Test should fail on incomplete response.
}

func (fs *FakeAPIServer) handleRegister(rw http.ResponseWriter, req *http.Request) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	reg := fs.registrations[req.Header.Get("Authorization")]
	if reg == nil {
		http.Error(rw, encodeMsg("unauthorized"), http.StatusUnauthorized)
	}

	out, err := json.Marshal(reg)
	if err != nil {
		http.Error(rw, encodeMsgf("json.Marshal(%v) = %v", reg, err), http.StatusInternalServerError)
		return
	}
	rw.Write(out) //nolint:errcheck // Test should fail on incomplete response.
}

func encodeMsg(msg any) string {
	input := map[string]string{"message": fmt.Sprint(msg)}
	b, err := json.Marshal(input)
	if err != nil {
		panic(fmt.Sprintf("json.Marshal(%v) = %v", input, err))
	}
	return string(b)
}

func encodeMsgf(f string, v ...any) string {
	return encodeMsg(fmt.Sprintf(f, v...))
}
