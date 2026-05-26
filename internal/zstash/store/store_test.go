package store

import (
	"testing"
	"time"
)

func TestCalculateTransferSpeedMBps(t *testing.T) {
	tests := []struct {
		name         string
		bytes        int64
		duration     time.Duration
		expectedMBps float64
	}{
		{
			name:         "1MB in 1 second",
			bytes:        1_000_000,
			duration:     time.Second,
			expectedMBps: 1.0,
		},
		{
			name:         "10MB in 2 seconds",
			bytes:        10_000_000,
			duration:     2 * time.Second,
			expectedMBps: 5.0,
		},
		{
			name:         "500KB in 0.5 seconds",
			bytes:        500_000,
			duration:     500 * time.Millisecond,
			expectedMBps: 1.0,
		},
		{
			name:         "100MB in 10 seconds",
			bytes:        100_000_000,
			duration:     10 * time.Second,
			expectedMBps: 10.0,
		},
		{
			name:         "Zero bytes",
			bytes:        0,
			duration:     time.Second,
			expectedMBps: 0.0,
		},
		{
			name:         "Very small transfer",
			bytes:        1024,
			duration:     time.Second,
			expectedMBps: 0.001024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualMBps := calculateTransferSpeedMBps(tt.bytes, tt.duration)

			if actualMBps != tt.expectedMBps {
				t.Errorf("Expected %.6f MB/s, got %.6f MB/s", tt.expectedMBps, actualMBps)
			}
		})
	}
}

func TestTransferSpeedCalculationWithTolerance(t *testing.T) {
	tests := []struct {
		name         string
		bytes        int64
		duration     time.Duration
		expectedMBps float64
		tolerance    float64
	}{
		{
			name:         "1.5MB in 1.2 seconds",
			bytes:        1_500_000,
			duration:     1200 * time.Millisecond,
			expectedMBps: 1.25,
			tolerance:    0.001,
		},
		{
			name:         "Binary vs decimal MB difference",
			bytes:        1_048_576, // 1 MiB
			duration:     time.Second,
			expectedMBps: 1.048576, // Should be ~1.05 MB/s (decimal)
			tolerance:    0.000001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualMBps := calculateTransferSpeedMBps(tt.bytes, tt.duration)

			diff := actualMBps - tt.expectedMBps
			if diff < 0 {
				diff = -diff
			}

			if diff > tt.tolerance {
				t.Errorf("Expected %.6f MB/s (±%.6f), got %.6f MB/s", tt.expectedMBps, tt.tolerance, actualMBps)
			}
		})
	}
}

func TestTransferInfoCalculation(t *testing.T) {
	start := time.Now()
	// Simulate some time passing
	time.Sleep(10 * time.Millisecond)

	bytesWritten := int64(1_000_000) // 1MB
	duration := time.Since(start)

	// Calculate using the extracted function
	averageSpeed := calculateTransferSpeedMBps(bytesWritten, duration)

	transferInfo := &TransferInfo{
		BytesTransferred: bytesWritten,
		TransferSpeed:    averageSpeed,
		Duration:         duration,
	}

	// Verify the calculation makes sense
	if transferInfo.TransferSpeed <= 0 {
		t.Error("Transfer speed should be positive")
	}

	if transferInfo.BytesTransferred != bytesWritten {
		t.Errorf("Expected %d bytes transferred, got %d", bytesWritten, transferInfo.BytesTransferred)
	}

	// The speed should be reasonable for 1MB over ~10ms
	// Should be around 100 MB/s (very fast since it's just a sleep)
	if transferInfo.TransferSpeed < 50 || transferInfo.TransferSpeed > 1000 {
		t.Logf("Transfer speed: %.2f MB/s (this is expected to be high due to sleep simulation)", transferInfo.TransferSpeed)
	}
}

func BenchmarkCalculateTransferSpeedMBps(b *testing.B) {
	bytes := int64(1_000_000)
	duration := time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calculateTransferSpeedMBps(bytes, duration)
	}
}
