package store

import (
	"time"
)

const (
	// local s3 store type
	LocalS3Store = "local_s3"
	// local hosted agents store type
	LocalHostedAgents = "local_hosted_agents"
	// local file store type
	LocalFileStore = "local_file"
)

type TransferInfo struct {
	BytesTransferred int64
	TransferSpeed    float64 // in MB/s
	RequestID        string
	Duration         time.Duration
	PartCount        int // number of parts used in multipart transfer (0 if not multipart)
	Concurrency      int // number of concurrent uploads/downloads used
}

func IsValidStore(storeType string) bool {
	switch storeType {
	case LocalS3Store, LocalHostedAgents, LocalFileStore:
		return true
	default:
		return false
	}
}

// calculateTransferSpeedMBps calculates transfer speed in MB/s (decimal megabytes)
// using the formula: bytes / duration_in_seconds / 1,000,000
func calculateTransferSpeedMBps(bytes int64, duration time.Duration) float64 {
	return float64(bytes) / duration.Seconds() / 1000 / 1000
}
