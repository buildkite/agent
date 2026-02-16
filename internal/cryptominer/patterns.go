// Package cryptominer provides detection of cryptocurrency mining processes.
package cryptominer

import (
	"fmt"
	"regexp"
	"strings"
)

// KnownMinerBinaries contains names of known cryptomining executables.
var KnownMinerBinaries = []string{
	"xmrig",
	"nbminer",
	"ethminer",
	"ccminer",
	"t-rex",
	"trex",
	"phoenixminer",
	"lolminer",
	"gminer",
	"teamredminer",
	"bzminer",
	"claymore",
	"excavator",
	"minerd",
	"cgminer",
	"bfgminer",
	"cpuminer",
	"cpuminer-multi",
	"cpuminer-opt",
	"xmr-stak",
	"xmr-stak-cpu",
	"xmr-stak-rx",
	"randomx",
	"kawpowminer",
	"nanominer",
	"srbminer",
	"wildrig",
	"nheqminer",
}

// SuspiciousCmdlinePatterns contains regex patterns that indicate mining activity
// in command-line arguments.
var SuspiciousCmdlinePatterns = []*regexp.Regexp{
	// Mining pool protocols
	regexp.MustCompile(`(?i)stratum\+tcp://`),
	regexp.MustCompile(`(?i)stratum\+ssl://`),
	regexp.MustCompile(`(?i)stratum\+tls://`),
	regexp.MustCompile(`(?i)stratum2\+`),

	// Common mining algorithm flags
	regexp.MustCompile(`(?i)--algo[= ](kawpow|randomx|ethash|etchash|autolykos|sha256|scrypt|equihash|cryptonight|argon2)`),
	regexp.MustCompile(`(?i)-a[= ]?(kawpow|randomx|ethash|etchash|autolykos|sha256|scrypt|equihash|cryptonight|argon2)`),

	// Mining pool connection flags (common across miners)
	regexp.MustCompile(`(?i)-o[= ]?[a-z0-9.-]+\.(pool\.|mining\.)`),
	regexp.MustCompile(`(?i)--url[= ][a-z0-9.-]+\.(pool\.|mining\.)`),

	// Worker/wallet specification (common pattern)
	regexp.MustCompile(`(?i)-u[= ]?[a-zA-Z0-9]{30,}`), // wallet address as username
	regexp.MustCompile(`(?i)--user[= ][a-zA-Z0-9]{30,}`),
	regexp.MustCompile(`(?i)--wallet[= ]`),

	// Known mining pool domains
	regexp.MustCompile(`(?i)(nicehash|2miners|ethermine|f2pool|nanopool|hiveon|flexpool|poolin|antpool|viabtc|slushpool|luxor|foundry|miningpoolhub|prohashing|zpool|zergpool|unmineable|moneroocean|supportxmr|minexmr|hashvault|herominers)\.`),

	// Cryptocurrency mining-specific flags
	regexp.MustCompile(`(?i)--coin[= ](eth|etc|rvn|kas|ergo|xmr|btc|ltc|zec|beam)`),
	regexp.MustCompile(`(?i)--donate-level[= ]`),
	regexp.MustCompile(`(?i)--rig-id[= ]`),
}

// MatchResult contains information about a detected cryptominer.
type MatchResult struct {
	PID         int
	ProcessName string
	Cmdline     string
	MatchType   string // "binary" or "pattern"
	MatchDetail string // which binary or pattern matched
}

// IsSuspiciousBinary checks if a process name matches known miner binaries.
func IsSuspiciousBinary(processName string) (bool, string) {
	lower := strings.ToLower(processName)
	for _, miner := range KnownMinerBinaries {
		if lower == miner || strings.HasPrefix(lower, miner+".") || strings.HasSuffix(lower, "/"+miner) {
			return true, miner
		}
	}
	return false, ""
}

// IsSuspiciousCmdline checks if a command line contains suspicious mining patterns.
func IsSuspiciousCmdline(cmdline string) (bool, string) {
	for _, pattern := range SuspiciousCmdlinePatterns {
		if pattern.MatchString(cmdline) {
			return true, pattern.String()
		}
	}
	return false, ""
}

// FormatDetectionMessage creates a human-readable message about detected miners.
func FormatDetectionMessage(result ScanResult) string {
	if !result.Found || len(result.Matches) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintln(&b, "🚨 Cryptominer detected in job process tree:")
	for _, match := range result.Matches {
		fmt.Fprintf(&b, "  • PID %d: %s (matched %s: %s)\n",
			match.PID, match.ProcessName, match.MatchType, match.MatchDetail)
		if match.Cmdline != "" {
			cmdline := match.Cmdline
			if len(cmdline) > 200 {
				cmdline = cmdline[:200] + "..."
			}
			fmt.Fprintf(&b, "    Command: %s\n", cmdline)
		}
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Using the Buildkite agent to mine cryptocurrency violates the terms of service. Continued violations will result in the termination of your account without notice.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "If you believe this detection was a false positive, please contact support@buildkite.com")
	return b.String()
}
