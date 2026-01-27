package job

import "testing"

func TestExtractSSHVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		output    string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{
			name:      "OpenSSH 8.9",
			output:    "OpenSSH_8.9p1 Ubuntu-3ubuntu0.1, OpenSSL 3.0.2 15 Mar 2022",
			wantMajor: 8,
			wantMinor: 9,
			wantOK:    true,
		},
		{
			name:      "OpenSSH 7.6",
			output:    "OpenSSH_7.6p1 Ubuntu-4ubuntu0.7, OpenSSL 1.0.2n  7 Dec 2017",
			wantMajor: 7,
			wantMinor: 6,
			wantOK:    true,
		},
		{
			name:      "OpenSSH 7.5 (before accept-new)",
			output:    "OpenSSH_7.5p1, OpenSSL 1.0.2k  26 Jan 2017",
			wantMajor: 7,
			wantMinor: 5,
			wantOK:    true,
		},
		{
			name:      "OpenSSH 9.0",
			output:    "OpenSSH_9.0p1, LibreSSL 3.3.6",
			wantMajor: 9,
			wantMinor: 0,
			wantOK:    true,
		},
		{
			name:      "macOS format with space",
			output:    "OpenSSH 9.6p1, LibreSSL 3.3.6",
			wantMajor: 9,
			wantMinor: 6,
			wantOK:    true,
		},
		{
			name:      "invalid output",
			output:    "not ssh output",
			wantMajor: 0,
			wantMinor: 0,
			wantOK:    false,
		},
		{
			name:      "empty output",
			output:    "",
			wantMajor: 0,
			wantMinor: 0,
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			major, minor, ok := extractSSHVersion(tc.output)
			if ok != tc.wantOK {
				t.Errorf("extractSSHVersion(%q) ok = %v, want %v", tc.output, ok, tc.wantOK)
			}
			if major != tc.wantMajor {
				t.Errorf("extractSSHVersion(%q) major = %d, want %d", tc.output, major, tc.wantMajor)
			}
			if minor != tc.wantMinor {
				t.Errorf("extractSSHVersion(%q) minor = %d, want %d", tc.output, minor, tc.wantMinor)
			}
		})
	}
}

func TestSSHVersionSupportsAcceptNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{"7.5 - no", "OpenSSH_7.5p1", false},
		{"7.6 - yes", "OpenSSH_7.6p1", true},
		{"7.7 - yes", "OpenSSH_7.7p1", true},
		{"8.0 - yes", "OpenSSH_8.0p1", true},
		{"9.0 - yes", "OpenSSH_9.0p1", true},
		{"6.9 - no", "OpenSSH_6.9p1", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			major, minor, ok := extractSSHVersion(tc.output)
			if !ok {
				t.Fatalf("failed to parse version from %q", tc.output)
			}

			got := major > 7 || (major == 7 && minor >= 6)
			if got != tc.want {
				t.Errorf("version %d.%d supportsAcceptNew = %v, want %v", major, minor, got, tc.want)
			}
		})
	}
}
