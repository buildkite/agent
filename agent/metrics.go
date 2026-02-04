package agent

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "buildkite_agent"
)

var (
	agentWorkersStarted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "workers",
		Name:      "started_total",
		Help:      "Count of agent workers started",
	})
	agentWorkersEnded = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "workers",
		Name:      "ended_total",
		Help:      "Count of agent workers ended",
	})
	// Currently running = started - ended.

	pingsSent = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "pings",
		Name:      "sent_total",
		Help:      "Count of pings sent",
	})
	pingErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "pings",
		Name:      "errors_total",
		Help:      "Count of pings that failed",
	})
	pingActions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "pings",
		Name:      "actions_total",
		Help:      "Count of successful pings by subsequent action",
	}, []string{"action"})
	pingDurations = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: "pings",
		Name:      "duration_seconds_total",
		Help:      "Time spent pinging (including errors, not including subsequent actions)",
		Buckets:   prometheus.ExponentialBuckets(0.015625, 2, 12),
	})
	pingWaitDurations = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: "pings",
		Name:      "wait_duration_seconds_total",
		Help:      "Time spent waiting between pings (ping ticker + jitter)",
		Buckets:   prometheus.LinearBuckets(1, 1, 20),
	})

	jobsStarted = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "jobs",
		Name:      "started_total",
		Help:      "Count of jobs started",
	})
	jobsEnded = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "jobs",
		Name:      "ended_total",
		Help:      "Count of jobs ended (any outcome)",
	})

	logChunksUploaded = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "logs",
		Name:      "chunks_uploaded_total",
		Help:      "Count of log chunks uploaded",
	})
	logBytesUploaded = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "logs",
		Name:      "bytes_uploaded_total",
		Help:      "Count of log bytes uploaded",
	})
	logChunkUploadErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "logs",
		Name:      "chunk_uploads_errored_total",
		Help:      "Count of log chunks not uploaded due to error",
	})
	logBytesUploadErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "logs",
		Name:      "bytes_uploads_errored_total",
		Help:      "Count of log bytes not uploaded due to error",
	})
	logUploadDurations = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: "logs",
		Name:      "upload_duration_seconds_total",
		Help:      "Time spent uploading log chunks",
		// Log chunk upload can be retried for a while.
		Buckets: prometheus.ExponentialBuckets(0.015625, 2, 16),
	})
)
