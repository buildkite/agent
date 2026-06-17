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

	// Original unlabelled counters — kept for backwards compatibility with
	// existing scrape consumers. New code should prefer the labelled
	// jobsStartedWithLabels / jobsEndedWithLabels counters below.
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

	// Labelled counters added for A-1100. These coexist with the unlabelled
	// jobsStarted / jobsEnded above; both are incremented at the same call
	// site so the unlabelled total always equals sum() over the labelled.
	jobsStartedWithLabels = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "jobs",
		Name:      "started_with_labels_total",
		Help:      "Count of jobs started, labelled by job priority and agent queue",
	}, []string{"priority", "queue"})
	jobsEndedWithLabels = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "jobs",
		Name:      "ended_with_labels_total",
		Help:      "Count of jobs ended (any outcome), labelled by job priority and agent queue",
	}, []string{"priority", "queue"})

	jobsRunning = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Subsystem: "jobs",
		Name:      "running",
		Help:      "Number of jobs currently running on this agent, labelled by job priority and agent queue",
	}, []string{"priority", "queue"})

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
	logCompressedBytesUploaded = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "logs",
		Name:      "compressed_bytes_uploaded_total",
		Help:      "Count of compressed log bytes sent over the wire",
	})
	logChunkSizeBytes = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNamespace,
		Subsystem: "logs",
		Name:      "chunk_size_bytes",
		Help:      "Distribution of log chunk sizes in bytes at upload time",
		Buckets:   prometheus.ExponentialBuckets(256, 4, 10), // 256B to ~64MB
	})
)
