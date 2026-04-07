package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

type report struct {
	GeneratedAt  time.Time        `json:"generated_at"`
	HostOS       string           `json:"host_os"`
	HostArch     string           `json:"host_arch"`
	SourceRepo   string           `json:"source_repo"`
	RepoName     string           `json:"repo_name"`
	Branch       string           `json:"branch"`
	AgentBinary  string           `json:"agent_binary"`
	AgentVersion string           `json:"agent_version"`
	GitVersion   string           `json:"git_version"`
	WorkDir      string           `json:"work_dir"`
	UpstreamURL  string           `json:"upstream_url"`
	Network      networkProfile   `json:"network"`
	Iterations   int              `json:"iterations"`
	Concurrency  int              `json:"concurrency"`
	Variants     []string         `json:"variants"`
	Scenarios    []scenarioReport `json:"scenarios"`
	Notes        []string         `json:"notes,omitempty"`
}

type networkProfile struct {
	Mode                  string `json:"mode"`
	LatencyMS             int    `json:"latency_ms,omitempty"`
	DownstreamKBPerSecond int    `json:"downstream_kb_per_second,omitempty"`
}

type scenarioReport struct {
	Name      string           `json:"name"`
	Rounds    []roundReport    `json:"rounds"`
	Summaries []variantSummary `json:"summaries"`
	Notes     []string         `json:"notes,omitempty"`
}

type roundReport struct {
	Iteration     int                  `json:"iteration"`
	Commit        string               `json:"commit"`
	StartedAt     time.Time            `json:"started_at"`
	Variants      []variantRoundReport `json:"variants"`
	CommitCreated bool                 `json:"commit_created,omitempty"`
}

type variantRoundReport struct {
	Name                    string         `json:"name"`
	RoundDurationMS         float64        `json:"round_duration_ms"`
	UpstreamRequests        int            `json:"upstream_requests"`
	UpstreamConnections     int64          `json:"upstream_connections"`
	UpstreamBytesToServer   int64          `json:"upstream_bytes_to_server"`
	UpstreamBytesFromServer int64          `json:"upstream_bytes_from_server"`
	UpstreamBytesTotal      int64          `json:"upstream_bytes_total"`
	Samples                 []sampleReport `json:"samples"`
	CacheSizeBytes          int64          `json:"cache_size_bytes"`
	Error                   string         `json:"error,omitempty"`
}

type sampleReport struct {
	Worker        int     `json:"worker"`
	DurationMS    float64 `json:"duration_ms"`
	UserMS        float64 `json:"user_ms"`
	SystemMS      float64 `json:"system_ms"`
	GitCloneMS    float64 `json:"git_clone_ms,omitempty"`
	GitFetchMS    float64 `json:"git_fetch_ms,omitempty"`
	GitCheckoutMS float64 `json:"git_checkout_ms,omitempty"`
	GitCleanMS    float64 `json:"git_clean_ms,omitempty"`
	CheckoutPath  string  `json:"checkout_path"`
	StdoutPath    string  `json:"stdout_path"`
	StderrPath    string  `json:"stderr_path"`
	TracePath     string  `json:"trace_path"`
	Error         string  `json:"error,omitempty"`
}

type variantSummary struct {
	Name                 string  `json:"name"`
	Samples              int     `json:"samples"`
	Rounds               int     `json:"rounds"`
	Failures             int     `json:"failures"`
	WorkerP50MS          float64 `json:"worker_p50_ms"`
	WorkerP95MS          float64 `json:"worker_p95_ms"`
	RoundP50MS           float64 `json:"round_p50_ms"`
	RoundP95MS           float64 `json:"round_p95_ms"`
	MeanUpstreamRequests float64 `json:"mean_upstream_requests"`
	MeanUpstreamBytes    float64 `json:"mean_upstream_bytes_total"`
	MeanBytesFromServer  float64 `json:"mean_upstream_bytes_from_server"`
	MeanUserMS           float64 `json:"mean_user_ms"`
	MeanSystemMS         float64 `json:"mean_system_ms"`
	MeanCloneMS          float64 `json:"mean_clone_ms"`
	MeanFetchMS          float64 `json:"mean_fetch_ms"`
	MeanCheckoutMS       float64 `json:"mean_checkout_ms"`
	MeanCleanMS          float64 `json:"mean_clean_ms"`
	FinalCacheSizeBytes  int64   `json:"final_cache_size_bytes"`
}

type gitTraceTimings struct {
	CloneMS    float64
	FetchMS    float64
	CheckoutMS float64
	CleanMS    float64
}

type trace2Event struct {
	Event string  `json:"event"`
	SID   string  `json:"sid"`
	Name  string  `json:"name"`
	TAbs  float64 `json:"t_abs"`
}

type scenarioSummaryRow struct {
	summary          variantSummary
	tailRatio        float64
	latencyDeltaPct  float64
	bytesDeltaPct    float64
	hasDirectControl bool
}

type summaryPrinter struct {
	w         io.Writer
	useColour bool
}

func parseGitTraceTimings(path string) (gitTraceTimings, error) {
	file, err := os.Open(path)
	if err != nil {
		return gitTraceTimings{}, err
	}
	defer func() { _ = file.Close() }()

	cmdNames := make(map[string]string)
	var timings gitTraceTimings

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event trace2Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return gitTraceTimings{}, fmt.Errorf("decode trace2 event: %w", err)
		}
		if strings.Contains(event.SID, "/") {
			continue
		}

		switch event.Event {
		case "cmd_name":
			cmdNames[event.SID] = event.Name
		case "exit":
			switch cmdNames[event.SID] {
			case "clone":
				timings.CloneMS += event.TAbs * float64(time.Second/time.Millisecond)
			case "fetch":
				timings.FetchMS += event.TAbs * float64(time.Second/time.Millisecond)
			case "checkout":
				timings.CheckoutMS += event.TAbs * float64(time.Second/time.Millisecond)
			case "clean":
				timings.CleanMS += event.TAbs * float64(time.Second/time.Millisecond)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return gitTraceTimings{}, fmt.Errorf("scan trace2 events: %w", err)
	}

	return timings, nil
}

func summariseScenario(rounds []roundReport) []variantSummary {
	byVariant := make(map[string][]variantRoundReport)
	for _, round := range rounds {
		for _, variant := range round.Variants {
			byVariant[variant.Name] = append(byVariant[variant.Name], variant)
		}
	}

	names := make([]string, 0, len(byVariant))
	for name := range byVariant {
		names = append(names, name)
	}
	sort.Strings(names)

	summaries := make([]variantSummary, 0, len(names))
	for _, name := range names {
		variants := byVariant[name]
		var workerDurations []float64
		var roundDurations []float64
		var requestCounts []float64
		var byteTotals []float64
		var bytesFromServer []float64
		var totalUserMS float64
		var totalSystemMS float64
		var totalCloneMS float64
		var totalFetchMS float64
		var totalCheckoutMS float64
		var totalCleanMS float64
		var totalSamples int
		var failures int
		var finalCacheSize int64

		for _, variant := range variants {
			roundDurations = append(roundDurations, variant.RoundDurationMS)
			requestCounts = append(requestCounts, float64(variant.UpstreamRequests))
			byteTotals = append(byteTotals, float64(variant.UpstreamBytesTotal))
			bytesFromServer = append(bytesFromServer, float64(variant.UpstreamBytesFromServer))
			finalCacheSize = variant.CacheSizeBytes
			for _, sample := range variant.Samples {
				totalSamples++
				workerDurations = append(workerDurations, sample.DurationMS)
				totalUserMS += sample.UserMS
				totalSystemMS += sample.SystemMS
				totalCloneMS += sample.GitCloneMS
				totalFetchMS += sample.GitFetchMS
				totalCheckoutMS += sample.GitCheckoutMS
				totalCleanMS += sample.GitCleanMS
				if sample.Error != "" {
					failures++
				}
			}
			if variant.Error != "" && len(variant.Samples) == 0 {
				failures++
			}
		}

		summaries = append(summaries, variantSummary{
			Name:                 name,
			Samples:              totalSamples,
			Rounds:               len(variants),
			Failures:             failures,
			WorkerP50MS:          percentile(workerDurations, 50),
			WorkerP95MS:          percentile(workerDurations, 95),
			RoundP50MS:           percentile(roundDurations, 50),
			RoundP95MS:           percentile(roundDurations, 95),
			MeanUpstreamRequests: mean(requestCounts),
			MeanUpstreamBytes:    mean(byteTotals),
			MeanBytesFromServer:  mean(bytesFromServer),
			MeanUserMS:           safeDiv(totalUserMS, float64(totalSamples)),
			MeanSystemMS:         safeDiv(totalSystemMS, float64(totalSamples)),
			MeanCloneMS:          safeDiv(totalCloneMS, float64(totalSamples)),
			MeanFetchMS:          safeDiv(totalFetchMS, float64(totalSamples)),
			MeanCheckoutMS:       safeDiv(totalCheckoutMS, float64(totalSamples)),
			MeanCleanMS:          safeDiv(totalCleanMS, float64(totalSamples)),
			FinalCacheSizeBytes:  finalCacheSize,
		})
	}

	return summaries
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(((p / 100) * float64(len(sorted)-1)) + 0.5)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func durationMS(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func printSummary(rep *report) {
	printSummaryTo(os.Stdout, rep, shouldUseColour(os.Stdout))
}

func printSummaryTo(w io.Writer, rep *report, useColour bool) {
	p := summaryPrinter{w: w, useColour: useColour}
	p.printf("\nGit checkout benchmark summary for %s (%s)\n", rep.RepoName, rep.Branch)
	p.printf("Agent:       %s\n", rep.AgentVersion)
	p.printf("Git:         %s\n", rep.GitVersion)
	p.printf("Source repo: %s\n", rep.SourceRepo)
	p.printf("Upstream:    %s\n", rep.UpstreamURL)
	p.printf("Iterations:  %d\n", rep.Iterations)
	p.printf("Concurrency: %d\n", rep.Concurrency)
	p.printf("Network:     %s\n", formatNetworkProfile(rep.Network))
	p.printf("Variants:    %s\n", strings.Join(rep.Variants, ", "))
	p.printf("Scenarios:   %s\n", strings.Join(scenarioNames(rep.Scenarios), ", "))

	for _, scenario := range rep.Scenarios {
		rows, direct := scenarioRows(scenario.Summaries)
		bestLatency, lowestUpstream, worstTail := scenarioHighlights(rows)

		p.printf("\nScenario: %s\n", scenario.Name)
		if len(rows) > 0 {
			tailHeadline := fmt.Sprintf("Worst tail: %s (%s)", rows[worstTail].summary.Name, describeTail(rows[worstTail]))
			if tailRatiosAreUniform(rows) {
				tailHeadline = fmt.Sprintf("Tail spread: all variants ~%s", describeTail(rows[worstTail]))
			}
			p.printf("Best latency: %s (%s); Lowest upstream: %s (%s); %s\n",
				rows[bestLatency].summary.Name,
				describeLatencyDelta(rows[bestLatency], direct),
				rows[lowestUpstream].summary.Name,
				describeBytesDelta(rows[lowestUpstream], direct),
				tailHeadline,
			)
		}
		p.printf("%-22s %6s %10s %9s %10s %7s %10s %10s %9s\n", "Variant", "fails", "round p50", "Δlat", "round p95", "tail", "req/round", "MiB/round", "ΔMiB")
		for i, row := range rows {
			p.printf("%s %s %10.1f %s %10.1f %s %10.2f %10.2f %s\n",
				p.formatVariantName(row.summary.Name, i == bestLatency, i == len(rows)-1 && len(rows) > 1),
				p.formatFailuresCell(row.summary.Failures),
				row.summary.RoundP50MS,
				p.formatDeltaCell(row.latencyDeltaPct, row.hasDirectControl),
				row.summary.RoundP95MS,
				p.formatTailCell(row.tailRatio),
				row.summary.MeanUpstreamRequests,
				row.summary.MeanUpstreamBytes/float64(1<<20),
				p.formatBytesDeltaCell(row.bytesDeltaPct, row.hasDirectControl),
			)
		}
	}
}

func shouldUseColour(stdout *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (p summaryPrinter) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(p.w, format, args...)
}

func (p summaryPrinter) style(text string, codes ...string) string {
	if !p.useColour || len(codes) == 0 {
		return text
	}
	return strings.Join(codes, "") + text + ansiReset
}

func (p summaryPrinter) formatVariantName(name string, best, slowest bool) string {
	text := fmt.Sprintf("%-22s", name)
	if best {
		return p.style(text, ansiBold, ansiGreen)
	}
	if slowest {
		return p.style(text, ansiDim, ansiRed)
	}
	return text
}

func (p summaryPrinter) formatFailuresCell(failures int) string {
	text := fmt.Sprintf("%6d", failures)
	if failures == 0 {
		return text
	}
	return p.style(text, ansiBold, ansiRed)
}

func (p summaryPrinter) formatDeltaCell(delta float64, hasBaseline bool) string {
	if !hasBaseline {
		return fmt.Sprintf("%9s", "—")
	}
	return p.styleByDelta(fmt.Sprintf("%9s", fmt.Sprintf("%+.1f%%", delta)), delta)
}

func (p summaryPrinter) formatBytesDeltaCell(delta float64, hasBaseline bool) string {
	if !hasBaseline {
		return fmt.Sprintf("%9s", "—")
	}
	return p.styleByDelta(fmt.Sprintf("%9s", fmt.Sprintf("%+.1f%%", delta)), delta)
}

func (p summaryPrinter) styleByDelta(text string, delta float64) string {
	switch {
	case delta <= -50:
		return p.style(text, ansiBold, ansiGreen)
	case delta <= -10:
		return p.style(text, ansiGreen)
	case delta >= 50:
		return p.style(text, ansiBold, ansiRed)
	case delta >= 10:
		return p.style(text, ansiYellow)
	default:
		return text
	}
}

func (p summaryPrinter) formatTailCell(tail float64) string {
	text := fmt.Sprintf("%7s", fmt.Sprintf("%.2fx", tail))
	switch {
	case tail >= 3:
		return p.style(text, ansiBold, ansiRed)
	case tail >= 2:
		return p.style(text, ansiRed)
	case tail >= 1.5:
		return p.style(text, ansiYellow)
	case tail < 1.2:
		return p.style(text, ansiGreen)
	default:
		return text
	}
}

func scenarioRows(summaries []variantSummary) ([]scenarioSummaryRow, *variantSummary) {
	baseline, hasBaseline := directControlSummary(summaries)
	rows := make([]scenarioSummaryRow, 0, len(summaries))
	for _, summary := range summaries {
		row := scenarioSummaryRow{
			summary:          summary,
			tailRatio:        safeDiv(summary.RoundP95MS, summary.RoundP50MS),
			hasDirectControl: hasBaseline,
		}
		if hasBaseline {
			row.latencyDeltaPct = percentDelta(summary.RoundP50MS, baseline.RoundP50MS)
			row.bytesDeltaPct = percentDelta(summary.MeanUpstreamBytes, baseline.MeanUpstreamBytes)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i].summary, rows[j].summary
		if left.Failures != right.Failures {
			return left.Failures < right.Failures
		}
		if left.RoundP50MS != right.RoundP50MS {
			return left.RoundP50MS < right.RoundP50MS
		}
		if left.RoundP95MS != right.RoundP95MS {
			return left.RoundP95MS < right.RoundP95MS
		}
		if left.MeanUpstreamBytes != right.MeanUpstreamBytes {
			return left.MeanUpstreamBytes < right.MeanUpstreamBytes
		}
		return left.Name < right.Name
	})
	if !hasBaseline {
		return rows, nil
	}
	return rows, &baseline
}

func directControlSummary(summaries []variantSummary) (variantSummary, bool) {
	for _, summary := range summaries {
		if summary.Name == "direct" && summary.Failures == 0 {
			return summary, true
		}
	}
	return variantSummary{}, false
}

func scenarioHighlights(rows []scenarioSummaryRow) (bestLatency, lowestUpstream, worstTail int) {
	if len(rows) == 0 {
		return 0, 0, 0
	}
	bestLatency, lowestUpstream, worstTail = 0, 0, 0
	minFailures := rows[0].summary.Failures
	for _, row := range rows[1:] {
		if row.summary.Failures < minFailures {
			minFailures = row.summary.Failures
		}
	}
	for i, row := range rows {
		if row.summary.Failures != minFailures {
			continue
		}
		if rows[bestLatency].summary.Failures != minFailures || row.summary.RoundP50MS < rows[bestLatency].summary.RoundP50MS {
			bestLatency = i
		}
		if rows[lowestUpstream].summary.Failures != minFailures || row.summary.MeanUpstreamBytes < rows[lowestUpstream].summary.MeanUpstreamBytes {
			lowestUpstream = i
		}
		if rows[worstTail].summary.Failures != minFailures || row.tailRatio > rows[worstTail].tailRatio {
			worstTail = i
		}
	}
	return bestLatency, lowestUpstream, worstTail
}

func percentDelta(value, baseline float64) float64 {
	if baseline == 0 {
		return 0
	}
	return ((value - baseline) / baseline) * 100
}

func describeLatencyDelta(row scenarioSummaryRow, baseline *variantSummary) string {
	if baseline == nil {
		return fmt.Sprintf("%.1fms", row.summary.RoundP50MS)
	}
	if row.summary.Name == baseline.Name {
		return "baseline"
	}
	if row.latencyDeltaPct < 0 {
		return fmt.Sprintf("%.1f%% faster than direct", -row.latencyDeltaPct)
	}
	if row.latencyDeltaPct > 0 {
		return fmt.Sprintf("%.1f%% slower than direct", row.latencyDeltaPct)
	}
	return "same as direct"
}

func describeBytesDelta(row scenarioSummaryRow, baseline *variantSummary) string {
	if baseline == nil {
		return fmt.Sprintf("%.2f MiB/round", row.summary.MeanUpstreamBytes/float64(1<<20))
	}
	if row.summary.Name == baseline.Name {
		return "baseline"
	}
	if row.bytesDeltaPct < 0 {
		return fmt.Sprintf("%.1f%% less MiB than direct", -row.bytesDeltaPct)
	}
	if row.bytesDeltaPct > 0 {
		return fmt.Sprintf("%.1f%% more MiB than direct", row.bytesDeltaPct)
	}
	return "same MiB as direct"
}

func describeTail(row scenarioSummaryRow) string {
	return fmt.Sprintf("%.2fx p95/p50", row.tailRatio)
}

func tailRatiosAreUniform(rows []scenarioSummaryRow) bool {
	if len(rows) < 2 {
		return true
	}
	minTail := rows[0].tailRatio
	maxTail := rows[0].tailRatio
	for _, row := range rows[1:] {
		if row.tailRatio < minTail {
			minTail = row.tailRatio
		}
		if row.tailRatio > maxTail {
			maxTail = row.tailRatio
		}
	}
	return maxTail-minTail < 0.05
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

func formatNetworkProfile(network networkProfile) string {
	if network.Mode != "toxiproxy" {
		return network.Mode
	}

	return fmt.Sprintf("%s (latency=%dms downstream=%dKB/s)", network.Mode, network.LatencyMS, network.DownstreamKBPerSecond)
}

func scenarioNames(scenarios []scenarioReport) []string {
	names := make([]string, 0, len(scenarios))
	for _, scenario := range scenarios {
		names = append(names, scenario.Name)
	}
	return names
}
