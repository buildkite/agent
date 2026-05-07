package agent

import (
	"strconv"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestJobCountersAreLabelled(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		job          *api.Job
		wantPriority string
		wantQueue    string
	}{
		{
			name: "default priority and explicit queue",
			job: &api.Job{
				Priority: 0,
				Env:      map[string]string{"BUILDKITE_AGENT_META_DATA_QUEUE": "default"},
			},
			wantPriority: "0",
			wantQueue:    "default",
		},
		{
			name: "explicit priority",
			job: &api.Job{
				Priority: 42,
				Env:      map[string]string{"BUILDKITE_AGENT_META_DATA_QUEUE": "high-priority"},
			},
			wantPriority: "42",
			wantQueue:    "high-priority",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			priority := strconv.Itoa(test.job.Priority)
			queue := test.job.Env["BUILDKITE_AGENT_META_DATA_QUEUE"]

			if priority != test.wantPriority {
				t.Errorf("priority label = %q, want %q", priority, test.wantPriority)
			}
			if queue != test.wantQueue {
				t.Errorf("queue label = %q, want %q", queue, test.wantQueue)
			}

			namedLabels := prometheus.Labels{"priority": priority, "queue": queue}

			startsBeforePos := testutil.ToFloat64(jobsStartedWithLabels.WithLabelValues(priority, queue))
			endsBeforePos := testutil.ToFloat64(jobsEndedWithLabels.WithLabelValues(priority, queue))
			startsBeforeName := testutil.ToFloat64(jobsStartedWithLabels.With(namedLabels))
			endsBeforeName := testutil.ToFloat64(jobsEndedWithLabels.With(namedLabels))

			// Increment the same way RunJob does.
			jobsStartedWithLabels.WithLabelValues(priority, queue).Inc()
			jobsEndedWithLabels.WithLabelValues(priority, queue).Inc()

			// The increment is observed via the positional API.
			if got := testutil.ToFloat64(jobsStartedWithLabels.WithLabelValues(priority, queue)) - startsBeforePos; got != 1 {
				t.Errorf("jobsStartedWithLabels{priority=%q,queue=%q} positional delta = %v, want 1", priority, queue, got)
			}
			if got := testutil.ToFloat64(jobsEndedWithLabels.WithLabelValues(priority, queue)) - endsBeforePos; got != 1 {
				t.Errorf("jobsEndedWithLabels{priority=%q,queue=%q} positional delta = %v, want 1", priority, queue, got)
			}

			// The same increment is observed via the name-keyed API.
			// If metrics.go registered the labels in a different order than
			// agent_worker.go passes them to WithLabelValues, this read
			// would land on a different counter than the one we incremented
			// and the delta would be 0.
			if got := testutil.ToFloat64(jobsStartedWithLabels.With(namedLabels)) - startsBeforeName; got != 1 {
				t.Errorf("jobsStartedWithLabels{priority=%q,queue=%q} name-keyed delta = %v, want 1 — label registration order in metrics.go may not match WithLabelValues argument order in agent_worker.go", priority, queue, got)
			}
			if got := testutil.ToFloat64(jobsEndedWithLabels.With(namedLabels)) - endsBeforeName; got != 1 {
				t.Errorf("jobsEndedWithLabels{priority=%q,queue=%q} name-keyed delta = %v, want 1 — label registration order in metrics.go may not match WithLabelValues argument order in agent_worker.go", priority, queue, got)
			}
		})
	}

	// Incrementing one label combination must not affect another.
	// This catches a class of regressions where someone might accidentally
	// register a Counter (no labels) instead of CounterVec, in which case all
	// "label combinations" would alias to the same single counter.
	t.Run("label combinations are independent", func(t *testing.T) {
		t.Parallel()

		a := testutil.ToFloat64(jobsStartedWithLabels.WithLabelValues("999", "queue-a"))
		b := testutil.ToFloat64(jobsStartedWithLabels.WithLabelValues("999", "queue-b"))

		jobsStartedWithLabels.WithLabelValues("999", "queue-a").Inc()

		if got := testutil.ToFloat64(jobsStartedWithLabels.WithLabelValues("999", "queue-a")) - a; got != 1 {
			t.Errorf("queue-a delta = %v, want 1", got)
		}
		if got := testutil.ToFloat64(jobsStartedWithLabels.WithLabelValues("999", "queue-b")) - b; got != 0 {
			t.Errorf("queue-b should be unaffected by queue-a increment, delta = %v, want 0", got)
		}
	})
}

func TestJobRunningGaugeIsLabelled(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		job          *api.Job
		wantPriority string
		wantQueue    string
	}{
		{
			name: "default priority and explicit queue",
			job: &api.Job{
				Priority: 0,
				Env:      map[string]string{"BUILDKITE_AGENT_META_DATA_QUEUE": "default"},
			},
			wantPriority: "0",
			wantQueue:    "default",
		},
		{
			name: "explicit priority",
			job: &api.Job{
				Priority: 42,
				Env:      map[string]string{"BUILDKITE_AGENT_META_DATA_QUEUE": "high-priority"},
			},
			wantPriority: "42",
			wantQueue:    "high-priority",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			priority := strconv.Itoa(test.job.Priority)
			queue := test.job.Env["BUILDKITE_AGENT_META_DATA_QUEUE"]

			if priority != test.wantPriority {
				t.Errorf("priority label = %q, want %q", priority, test.wantPriority)
			}
			if queue != test.wantQueue {
				t.Errorf("queue label = %q, want %q", queue, test.wantQueue)
			}

			namedLabels := prometheus.Labels{"priority": priority, "queue": queue}

			beforePos := testutil.ToFloat64(jobsRunning.WithLabelValues(priority, queue))
			beforeName := testutil.ToFloat64(jobsRunning.With(namedLabels))

			// Mirror the start-of-job pattern in RunJob: cache the labelled
			// gauge once, Inc on entry, Dec on exit.
			running := jobsRunning.WithLabelValues(priority, queue)
			running.Inc()

			// During the "running" window the gauge should be +1.
			if got := testutil.ToFloat64(jobsRunning.WithLabelValues(priority, queue)) - beforePos; got != 1 {
				t.Errorf("jobsRunning{priority=%q,queue=%q} during-Inc positional delta = %v, want 1", priority, queue, got)
			}
			if got := testutil.ToFloat64(jobsRunning.With(namedLabels)) - beforeName; got != 1 {
				t.Errorf("jobsRunning{priority=%q,queue=%q} during-Inc name-keyed delta = %v, want 1 — label registration order in metrics.go may not match WithLabelValues argument order in agent_worker.go", priority, queue, got)
			}

			// On exit the deferred Dec returns the gauge to its prior value.
			running.Dec()

			if got := testutil.ToFloat64(jobsRunning.WithLabelValues(priority, queue)) - beforePos; got != 0 {
				t.Errorf("jobsRunning{priority=%q,queue=%q} after Inc/Dec positional delta = %v, want 0", priority, queue, got)
			}
			if got := testutil.ToFloat64(jobsRunning.With(namedLabels)) - beforeName; got != 0 {
				t.Errorf("jobsRunning{priority=%q,queue=%q} after Inc/Dec name-keyed delta = %v, want 0", priority, queue, got)
			}
		})
	}
}
