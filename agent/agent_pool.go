package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/status"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// AgentPool manages multiple parallel AgentWorkers.
type AgentPool struct {
	workers []*AgentWorker
}

// NewAgentPool returns a new AgentPool.
func NewAgentPool(workers []*AgentWorker) *AgentPool {
	return &AgentPool{
		workers: workers,
	}
}

func (ap *AgentPool) StartStatusServer(ctx context.Context, l logger.Logger, addr string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", healthHandler(l))
	mux.Handle("/metrics", promhttp.Handler())
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

// Start kicks off the parallel AgentWorkers and waits for them to finish.
func (r *AgentPool) Start(ctx context.Context) error {
	ctx, setStat, done := status.AddSimpleItem(ctx, "Agent Pool")
	defer done()
	setStat("🏃 Spawning workers...")

	idleMon := newIdleMonitor(len(r.workers))

	errCh := make(chan error)

	// Spawn each worker "in parallel" (in its own goroutine)
	for _, worker := range r.workers {
		go func() {
			errCh <- runWorker(ctx, worker, idleMon)
		}()
	}

	setStat("✅ Workers spawned!")

	// Number of receives = number of sends
	errs := make([]error, 0, len(r.workers))
	for range r.workers {
		errs = append(errs, <-errCh)
	}
	return errors.Join(errs...) // nil if all errs are nil
}

func runWorker(ctx context.Context, worker *AgentWorker, idleMon *idleMonitor) error {
	agentWorkersStarted.Inc()
	defer agentWorkersEnded.Inc()
	defer idleMon.markDead(worker)

	// Connect the worker to the API
	if err := worker.Connect(ctx); err != nil {
		return err
	}
	// Ensure the worker is disconnected at the end of this function.
	defer worker.Disconnect(ctx) //nolint:errcheck // Error is logged within core/client

	// Starts the agent worker and wait for it to finish.
	return worker.Start(ctx, idleMon)
}

// StopGracefully stops all workers in the pool gracefully.
func (r *AgentPool) StopGracefully() {
	for _, worker := range r.workers {
		worker.StopGracefully()
	}
}

// StopUngracefully stops all workers in the pool ungracefully. It blocks until
// all workers have returned from stopping, which means waiting for job
// cancellation to finish.
func (r *AgentPool) StopUngracefully() {
	var wg sync.WaitGroup
	wg.Add(len(r.workers))
	for _, worker := range r.workers {
		// Because StopUngracefully calls the job runner's Cancel, which blocks,
		// concurrently stop all the workers.
		// The number of concurrent Stops is bounded by the spawn count, and
		// there already exists a handful of goroutines per worker.
		go func() {
			worker.StopUngracefully()
			wg.Done()
		}()
	}
	wg.Wait()
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
		l.Debug("agent_pool.go/healthHandler: %s %s", r.Method, r.URL.Path)
		if r.URL.Path != "/" {
			http.NotFound(w, r)
		} else {
			fmt.Fprintf(w, "OK: Buildkite agent is running") //nolint:errcheck // YOLO?
		}
	}
}
