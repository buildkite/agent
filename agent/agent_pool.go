package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/status"
)

// AgentPool manages multiple parallel AgentWorkers
type AgentPool struct {
	workers     []*AgentWorker
	idleMonitor *IdleMonitor
}

// NewAgentPool returns a new AgentPool
func NewAgentPool(workers []*AgentWorker) *AgentPool {
	return &AgentPool{
		workers:     workers,
		idleMonitor: NewIdleMonitor(len(workers)),
	}
}

func (ap *AgentPool) StartStatusServer(ctx context.Context, l logger.Logger, addr string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", healthHandler(l))
	mux.HandleFunc("/status", status.Handle)
	mux.HandleFunc("/status.json", ap.statusJSONHandler(l))

	for _, worker := range ap.workers {
		mux.HandleFunc("/agent/"+strconv.Itoa(worker.spawnIndex), worker.healthHandler())
	}

	go func() {
		_, setStatus, done := status.AddSimpleItem(ctx, "Health check server")
		defer done()
		setStatus("👂 Listening")

		l.Notice("Starting HTTP health check server on %v", addr)
		err := http.ListenAndServe(addr, mux)
		if err != nil {
			l.Error("Could not start health check server: %v", err)
		}
	}()
}

// Start kicks off the parallel AgentWorkers and waits for them to finish
func (r *AgentPool) Start(ctx context.Context) error {
	ctx, setStat, done := status.AddSimpleItem(ctx, "Agent Pool")
	defer done()
	setStat("🏃 Spawning workers...")

	var wg sync.WaitGroup
	var spawn int = len(r.workers)
	var errs = make(chan error, spawn)

	// Spawn goroutines for each parallel worker
	for _, worker := range r.workers {
		wg.Add(1)

		go func(worker *AgentWorker) {
			defer wg.Done()

			if err := r.runWorker(ctx, worker); err != nil {
				errs <- err
			}
		}(worker)
	}

	setStat("✅ Workers spawned!")

	go func() {
		wg.Wait()
		close(errs)
	}()

	return <-errs
}

func (r *AgentPool) runWorker(ctx context.Context, worker *AgentWorker) error {
	// Connect the worker to the API
	if err := worker.Connect(ctx); err != nil {
		return err
	}
	// Ensure the worker is disconnected at the end of this function.
	defer worker.Disconnect(ctx)

	// Starts the agent worker and wait for it to finish.
	return worker.Start(ctx, r.idleMonitor)
}

func (r *AgentPool) Stop(graceful bool) {
	for _, worker := range r.workers {
		worker.Stop(graceful)
	}
}

func (ap *AgentPool) statusJSONHandler(l logger.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		type agentWorkerStatus struct {
			Status       agentWorkerState `json:"status"`
			CurrentJobID string           `json:"current_job_id,omitempty"`
			ID           string           `json:"id"`
			SpawnIndex   int              `json:"spawn_index"`
		}

		aggregateState := agentWorkerStateIdle
		statuses := make([]agentWorkerStatus, 0, len(ap.workers))
		for _, worker := range ap.workers {
			// If any worker is busy, the aggregate state is busy
			workerState := worker.getState()
			if workerState == agentWorkerStateBusy {
				aggregateState = agentWorkerStateBusy
			}
			statuses = append(statuses, agentWorkerStatus{
				ID:           worker.agent.UUID,
				Status:       workerState,
				CurrentJobID: worker.getCurrentJobID(),
				SpawnIndex:   worker.spawnIndex,
			})
		}

		err := json.NewEncoder(w).Encode(struct {
			Health          string              `json:"health"`
			AggregateStatus agentWorkerState    `json:"aggregate_status"`
			Workers         []agentWorkerStatus `json:"workers"`
		}{
			Health:          "ok",
			AggregateStatus: aggregateState,
			Workers:         statuses,
		})

		if err != nil {
			l.Error("Could not encode status.json response: %v", err)
		}
	}
}

func healthHandler(l logger.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l.Info("%s %s", r.Method, r.URL.Path)
		if r.URL.Path != "/" {
			http.NotFound(w, r)
		} else {
			fmt.Fprintf(w, "OK: Buildkite agent is running")
		}
	}
}
