package artifact

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const metricsNamespace = "buildkite_agent"

var (
	artifactsUploaded = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "artifacts",
		Name:      "uploaded_total",
		Help:      "Count of artifacts uploaded",
	})
	artifactBytesUploaded = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "artifacts",
		Name:      "bytes_uploaded_total",
		Help:      "Count of artifact bytes uploaded",
	})
	artifactUploadFailures = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNamespace,
		Subsystem: "artifacts",
		Name:      "uploads_failed_total",
		Help:      "Count of artifact uploads that failed",
	})
)
