package clicommand

import (
	"testing"
)

func TestClampOIDCTokenLifetimeSeconds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		requested   int
		maxLifetime int
		wantEff     int
		wantClamp   bool
	}{
		{"no cap", 600, 0, 600, false},
		{"no cap negative max", 600, -1, 600, false},
		{"api default unchanged", 0, 300, 0, false},
		{"under cap", 100, 300, 100, false},
		{"at cap", 300, 300, 300, false},
		{"over cap", 600, 300, 300, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, clamped := clampOIDCTokenLifetimeSeconds(tt.requested, tt.maxLifetime)
			if got != tt.wantEff || clamped != tt.wantClamp {
				t.Errorf("clampOIDCTokenLifetimeSeconds(%d, %d) = (%d, %v), want (%d, %v)",
					tt.requested, tt.maxLifetime, got, clamped, tt.wantEff, tt.wantClamp)
			}
		})
	}
}
