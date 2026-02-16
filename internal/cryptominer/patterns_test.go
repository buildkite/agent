package cryptominer

import (
	"testing"
)

func TestIsSuspiciousBinary(t *testing.T) {
	tests := []struct {
		name        string
		processName string
		want        bool
		wantMatch   string
	}{
		{"xmrig exact", "xmrig", true, "xmrig"},
		{"xmrig with extension", "xmrig.exe", true, "xmrig"},
		{"nbminer", "nbminer", true, "nbminer"},
		{"ethminer", "ethminer", true, "ethminer"},
		{"t-rex", "t-rex", true, "t-rex"},
		{"phoenixminer", "phoenixminer", true, "phoenixminer"},
		{"case insensitive", "XMRIG", true, "xmrig"},
		{"case insensitive mixed", "XmRig", true, "xmrig"},
		{"bash not suspicious", "bash", false, ""},
		{"python not suspicious", "python3", false, ""},
		{"node not suspicious", "node", false, ""},
		{"go not suspicious", "go", false, ""},
		{"partial match should not match", "myxmrig", false, ""}, // not a prefix/exact match
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotMatch := IsSuspiciousBinary(tt.processName)
			if got != tt.want {
				t.Errorf("IsSuspiciousBinary(%q) = %v, want %v", tt.processName, got, tt.want)
			}
			if got && gotMatch != tt.wantMatch {
				t.Errorf("IsSuspiciousBinary(%q) matched %q, want %q", tt.processName, gotMatch, tt.wantMatch)
			}
		})
	}
}

func TestIsSuspiciousCmdline(t *testing.T) {
	tests := []struct {
		name    string
		cmdline string
		want    bool
	}{
		{
			"stratum tcp",
			"./miner -o stratum+tcp://pool.example.com:3333",
			true,
		},
		{
			"stratum ssl",
			"/usr/bin/miner --url stratum+ssl://eth.pool.com:443",
			true,
		},
		{
			"algorithm kawpow",
			"./miner --algo kawpow -o pool.com",
			true,
		},
		{
			"algorithm randomx",
			"./xmrig -a randomx",
			true,
		},
		{
			"nicehash domain",
			"./miner -o stratum.nicehash.com:3333",
			true,
		},
		{
			"ethermine domain",
			"./miner -o us1.ethermine.org:4444",
			true,
		},
		{
			"moneroocean domain",
			"./xmrig -o gulf.moneroocean.stream:10001",
			true,
		},
		{
			"wallet flag",
			"./miner --wallet 0x1234567890abcdef1234567890abcdef12345678",
			true,
		},
		{
			"donate level flag",
			"./xmrig --donate-level 1",
			true,
		},
		{
			"normal go build",
			"go build -o myapp ./cmd/myapp",
			false,
		},
		{
			"normal npm install",
			"npm install --save lodash",
			false,
		},
		{
			"normal python script",
			"python3 train_model.py --algo gradient_descent",
			false,
		},
		{
			"docker run",
			"docker run -it ubuntu:latest /bin/bash",
			false,
		},
		{
			"git clone",
			"git clone https://github.com/example/repo.git",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := IsSuspiciousCmdline(tt.cmdline)
			if got != tt.want {
				t.Errorf("IsSuspiciousCmdline(%q) = %v, want %v", tt.cmdline, got, tt.want)
			}
		})
	}
}

func TestScanProcCmdline(t *testing.T) {
	lines := []string{
		"1234 5001 /bin/bash /path/to/script.sh",
		"1234 5002 /usr/bin/xmrig -o stratum+tcp://pool.com:3333",
		"1234 5003 sleep 60",
		"9999 6001 /usr/bin/node server.js", // different PGID
	}

	result, err := ScanProcCmdline(lines, 1234)
	if err != nil {
		t.Fatalf("ScanProcCmdline failed: %v", err)
	}

	if !result.Found {
		t.Error("Expected to find a miner, but Found is false")
	}

	if len(result.Matches) != 1 {
		t.Errorf("Expected 1 match, got %d", len(result.Matches))
	}

	if len(result.Matches) > 0 {
		match := result.Matches[0]
		if match.PID != 5002 {
			t.Errorf("Expected PID 5002, got %d", match.PID)
		}
		if match.ProcessName != "xmrig" {
			t.Errorf("Expected process name 'xmrig', got %q", match.ProcessName)
		}
	}
}

func TestScanProcCmdlineNoMatch(t *testing.T) {
	lines := []string{
		"1234 5001 /bin/bash /path/to/script.sh",
		"1234 5002 go test ./...",
		"1234 5003 npm run build",
	}

	result, err := ScanProcCmdline(lines, 1234)
	if err != nil {
		t.Fatalf("ScanProcCmdline failed: %v", err)
	}

	if result.Found {
		t.Error("Expected no miner found, but Found is true")
	}
}
