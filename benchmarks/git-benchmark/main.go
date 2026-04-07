package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type config struct {
	agentBinary string
	sourceRepo  string
	workDir     string
	outputPath  string
	iterations  int
	concurrency int
	variants    []string
	scenarios   []string
	toxiproxy   toxiproxyConfig
	keepWorkDir bool
}

type toxiproxyConfig struct {
	enabled               bool
	image                 string
	latencyMS             int
	downstreamKBPerSecond int
}

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

type benchmarkVariant struct {
	mirrorMode string
	cloneFlags string
	fetchFlags string
}

type benchmarkHarness struct {
	cfg          config
	rootDir      string
	upstreamDir  string
	writerDir    string
	repoName     string
	branch       string
	upstreamURL  string
	agentVersion string
	gitVersion   string
	upstreamLog  string
	upstreamCmd  *exec.Cmd
	upstreamTap  *countingProxy
	tproxy       *toxiproxyRuntime
}

type variantEnv struct {
	name      string
	rootDir   string
	mirrorDir string
}

type toxiproxyRuntime struct {
	mode      string
	adminURL  string
	proxyName string
	proxyPort int
	host      string
	container string
	cmd       *exec.Cmd
}

type toxiproxyCreateProxyRequest struct {
	Name     string `json:"name"`
	Listen   string `json:"listen"`
	Upstream string `json:"upstream"`
}

type toxiproxyCreateToxicRequest struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Stream     string         `json:"stream"`
	Toxicity   float64        `json:"toxicity"`
	Attributes map[string]any `json:"attributes"`
}

var benchmarkVariants = map[string]benchmarkVariant{
	"direct": {
		cloneFlags: "-v",
		fetchFlags: "-v --prune",
	},
	"direct-shallow": {
		cloneFlags: "-v --depth=1",
		fetchFlags: "-v --prune --depth=1",
	},
	"direct-blobless": {
		cloneFlags: "-v --filter=blob:none",
		fetchFlags: "-v --prune --filter=blob:none",
	},
	"mirror-reference": {
		mirrorMode: "reference",
		cloneFlags: "-v",
		fetchFlags: "-v --prune",
	},
	"mirror-dissociate": {
		mirrorMode: "dissociate",
		cloneFlags: "-v",
		fetchFlags: "-v --prune",
	},
}

var validScenarios = map[string]struct{}{
	"cold-single":                {},
	"warm-single":                {},
	"warm-concurrent":            {},
	"warm-concurrent-new-commit": {},
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := parseConfig()
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fatalf("parse config: %v", err)
	}

	h, err := newBenchmarkHarness(ctx, cfg)
	if err != nil {
		fatalf("initialise harness: %v", err)
	}
	defer h.cleanup()

	report, err := h.run(ctx)
	if err != nil {
		fatalf("run benchmark: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.outputPath), 0o755); err != nil {
		fatalf("create output directory: %v", err)
	}
	buf, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatalf("marshal report: %v", err)
	}
	if err := os.WriteFile(cfg.outputPath, append(buf, '\n'), 0o644); err != nil {
		fatalf("write report: %v", err)
	}

	printSummary(report)
	fmt.Printf("\nReport written to %s\n", cfg.outputPath)
}

func parseConfig() (config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return config{}, fmt.Errorf("get working directory: %w", err)
	}
	return parseConfigFromArgs(os.Args[1:], cwd)
}

func parseConfigFromArgs(args []string, cwd string) (config, error) {
	var cfg config
	var variants string
	var scenarios string
	fs := flag.NewFlagSet("git-benchmark", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)

	fs.StringVar(&cfg.agentBinary, "agent-binary", "", "Path to a buildkite-agent binary")
	fs.StringVar(&cfg.sourceRepo, "source-repo", "", "Repository URL to benchmark")
	fs.StringVar(&cfg.workDir, "workdir", "", "Benchmark work directory (defaults to a temp dir)")
	fs.StringVar(&cfg.outputPath, "output", "", "Path to write the JSON report")
	fs.IntVar(&cfg.iterations, "iterations", 3, "Number of rounds to run per scenario")
	fs.IntVar(&cfg.concurrency, "concurrency", 8, "Number of concurrent bootstraps for concurrent scenarios")
	fs.StringVar(&variants, "variants", "direct,direct-shallow,direct-blobless,mirror-reference,mirror-dissociate", "Comma-separated variant list")
	fs.StringVar(&scenarios, "scenarios", "cold-single,warm-single,warm-concurrent,warm-concurrent-new-commit", "Comma-separated scenario list")
	fs.BoolVar(&cfg.toxiproxy.enabled, "toxiproxy", false, "Put the upstream git daemon behind Toxiproxy")
	fs.StringVar(&cfg.toxiproxy.image, "toxiproxy-image", "ghcr.io/shopify/toxiproxy:2.12.0", "Docker image to use when Toxiproxy is started via Docker")
	fs.IntVar(&cfg.toxiproxy.latencyMS, "toxiproxy-latency-ms", 30, "Injected downstream latency in milliseconds when Toxiproxy is enabled")
	fs.IntVar(&cfg.toxiproxy.downstreamKBPerSecond, "toxiproxy-downstream-kb-per-second", 10240, "Injected downstream bandwidth cap in kilobytes per second when Toxiproxy is enabled")
	fs.BoolVar(&cfg.keepWorkDir, "keep-workdir", false, "Keep the benchmark work directory after completion")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.iterations <= 0 {
		return cfg, errors.New("--iterations must be greater than zero")
	}
	if cfg.concurrency <= 0 {
		return cfg, errors.New("--concurrency must be greater than zero")
	}
	if cfg.toxiproxy.enabled {
		if cfg.toxiproxy.latencyMS < 0 {
			return cfg, errors.New("--toxiproxy-latency-ms must be zero or greater")
		}
		if cfg.toxiproxy.downstreamKBPerSecond < 0 {
			return cfg, errors.New("--toxiproxy-downstream-kb-per-second must be zero or greater")
		}
	}

	var err error
	cfg.variants, err = splitCSV(variants)
	if err != nil {
		return cfg, fmt.Errorf("parse variants: %w", err)
	}
	if err := validateVariantNames(cfg.variants); err != nil {
		return cfg, fmt.Errorf("parse variants: %w", err)
	}
	cfg.scenarios, err = splitCSV(scenarios)
	if err != nil {
		return cfg, fmt.Errorf("parse scenarios: %w", err)
	}
	if err := validateAllowedValues(cfg.scenarios, validScenarios, "scenario"); err != nil {
		return cfg, fmt.Errorf("parse scenarios: %w", err)
	}

	if cfg.workDir == "" {
		cfg.workDir, err = os.MkdirTemp("/tmp", "gcb-")
		if err != nil {
			return cfg, fmt.Errorf("create temp workdir: %w", err)
		}
	}
	if cfg.sourceRepo == "" {
		cfg.sourceRepo = cwd
	}
	if cfg.agentBinary == "" {
		return cfg, errors.New("--agent-binary is required")
	}
	if cfg.outputPath == "" {
		cfg.outputPath = filepath.Join(cfg.workDir, "report.json")
	}

	return cfg, nil
}

func splitCSV(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		items = append(items, trimmed)
	}
	if len(items) == 0 {
		return nil, errors.New("empty list")
	}
	return items, nil
}

func validateVariantNames(values []string) error {
	for _, value := range values {
		if _, err := variantDefinition(value); err != nil {
			return err
		}
	}
	return nil
}

func variantDefinition(name string) (benchmarkVariant, error) {
	variant, ok := benchmarkVariants[name]
	if !ok {
		return benchmarkVariant{}, fmt.Errorf("unknown variant %q", name)
	}
	return variant, nil
}

func validateAllowedValues(values []string, allowed map[string]struct{}, kind string) error {
	for _, value := range values {
		if _, ok := allowed[value]; ok {
			continue
		}
		return fmt.Errorf("unknown %s %q", kind, value)
	}
	return nil
}

func newBenchmarkHarness(ctx context.Context, cfg config) (*benchmarkHarness, error) {
	rootDir, err := filepath.Abs(cfg.workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	h := &benchmarkHarness{cfg: cfg, rootDir: rootDir}

	h.agentVersion, err = h.commandOutput(ctx, "", nil, cfg.agentBinary, "--version")
	if err != nil {
		return nil, fmt.Errorf("get agent version: %w", err)
	}
	h.agentVersion = strings.TrimSpace(h.agentVersion)

	h.gitVersion, err = h.commandOutput(ctx, "", nil, "git", "--version")
	if err != nil {
		return nil, fmt.Errorf("get git version: %w", err)
	}
	h.gitVersion = strings.TrimSpace(h.gitVersion)

	h.repoName = strings.TrimSuffix(filepath.Base(strings.TrimSuffix(cfg.sourceRepo, "/")), ".git")
	h.upstreamDir = filepath.Join(rootDir, "upstream")
	if err := os.MkdirAll(h.upstreamDir, 0o755); err != nil {
		return nil, fmt.Errorf("create upstream dir: %w", err)
	}

	barePath := filepath.Join(h.upstreamDir, h.repoName+".git")
	fmt.Printf("Cloning %s into local upstream cache...\n", cfg.sourceRepo)
	if _, err := h.commandOutput(ctx, "", nil, "git", "clone", "--mirror", "--", cfg.sourceRepo, barePath); err != nil {
		return nil, fmt.Errorf("clone upstream mirror: %w", err)
	}

	h.writerDir = filepath.Join(rootDir, "writer")
	if _, err := h.commandOutput(ctx, "", nil, "git", "clone", "--", barePath, h.writerDir); err != nil {
		return nil, fmt.Errorf("clone writer repo: %w", err)
	}
	if _, err := h.commandOutput(ctx, h.writerDir, nil, "git", "config", "user.name", "Person Example"); err != nil {
		return nil, fmt.Errorf("configure writer user.name: %w", err)
	}
	if _, err := h.commandOutput(ctx, h.writerDir, nil, "git", "config", "user.email", "person@example.com"); err != nil {
		return nil, fmt.Errorf("configure writer user.email: %w", err)
	}

	h.branch, err = h.commandOutput(ctx, h.writerDir, nil, "git", "branch", "--show-current")
	if err != nil {
		return nil, fmt.Errorf("detect branch: %w", err)
	}
	h.branch = strings.TrimSpace(h.branch)
	if h.branch == "" {
		return nil, errors.New("detected empty branch name")
	}

	if err := h.startUpstream(ctx); err != nil {
		return nil, err
	}

	return h, nil
}

func (h *benchmarkHarness) run(ctx context.Context) (*report, error) {
	rep := &report{
		GeneratedAt:  time.Now(),
		HostOS:       runtime.GOOS,
		HostArch:     runtime.GOARCH,
		SourceRepo:   h.cfg.sourceRepo,
		RepoName:     h.repoName,
		Branch:       h.branch,
		AgentBinary:  h.cfg.agentBinary,
		AgentVersion: h.agentVersion,
		GitVersion:   h.gitVersion,
		WorkDir:      h.rootDir,
		UpstreamURL:  h.upstreamURL,
		Network:      h.networkProfile(),
		Iterations:   h.cfg.iterations,
		Concurrency:  h.cfg.concurrency,
		Variants:     append([]string(nil), h.cfg.variants...),
	}

	for _, scenarioName := range h.cfg.scenarios {
		scenario, err := h.runScenario(ctx, scenarioName)
		if err != nil {
			return nil, err
		}
		rep.Scenarios = append(rep.Scenarios, *scenario)
	}

	if !h.cfg.keepWorkDir {
		note := "benchmark work directory removed after run"
		if h.outputPathWithinRootDir() {
			note = "benchmark work directory cleaned after run, preserving the report file"
		}
		rep.Notes = append(rep.Notes, note)
	}
	rep.Notes = append(rep.Notes, "upstream traffic is measured by a counted TCP proxy in front of the benchmark git daemon")

	return rep, nil
}

func (h *benchmarkHarness) runScenario(ctx context.Context, scenarioName string) (*scenarioReport, error) {
	fmt.Printf("\n=== Scenario: %s ===\n", scenarioName)
	fmt.Print("Progress: ")
	defer fmt.Println()

	scenarioDir := filepath.Join(h.rootDir, scenarioName)
	if err := os.MkdirAll(scenarioDir, 0o755); err != nil {
		return nil, fmt.Errorf("create scenario dir: %w", err)
	}

	rep := &scenarioReport{Name: scenarioName}

	stateByVariant := make(map[string]*variantEnv, len(h.cfg.variants))
	currentCommit, err := h.currentCommit(ctx)
	if err != nil {
		return nil, err
	}

	for iteration := 1; iteration <= h.cfg.iterations; iteration++ {
		round := roundReport{
			Iteration: iteration,
			StartedAt: time.Now(),
		}
		previousCommit := currentCommit
		createdNewCommitForRound := false

		switch scenarioName {
		case "cold-single", "warm-single", "warm-concurrent", "warm-concurrent-new-commit":
		default:
			return nil, fmt.Errorf("unknown scenario %q", scenarioName)
		}

		if scenarioName == "warm-concurrent-new-commit" && iteration > 1 {
			currentCommit, err = h.createAndPushCommit(ctx, iteration)
			if err != nil {
				return nil, err
			}
			round.CommitCreated = true
			createdNewCommitForRound = true
		}

		for _, variantName := range h.cfg.variants {
			variant, ok := stateByVariant[variantName]
			if !ok || scenarioName == "cold-single" {
				if ok {
					variant.close()
				}
				variant, err = h.newVariant(ctx, scenarioDir, variantName, iteration)
				if err != nil {
					return nil, err
				}
				stateByVariant[variantName] = variant

				if scenarioName != "cold-single" {
					primeCommit := currentCommit
					if scenarioName == "warm-concurrent-new-commit" {
						primeCommit = previousCommit
					}
					if err := h.primeVariant(ctx, variant, primeCommit); err != nil {
						fmt.Print("!")
						return nil, fmt.Errorf("prime %s: %w", variantName, err)
					}
					fmt.Print("p")
				}
			}

			if scenarioName == "warm-concurrent-new-commit" && !createdNewCommitForRound {
				currentCommit, err = h.createAndPushCommit(ctx, iteration)
				if err != nil {
					return nil, err
				}
				round.CommitCreated = true
				createdNewCommitForRound = true
			}

			report, err := h.runVariantRound(ctx, variant, scenarioName, iteration, currentCommit)
			if err != nil {
				report.Error = err.Error()
				fmt.Print("!")
			} else {
				fmt.Print(".")
			}
			round.Variants = append(round.Variants, report)
		}

		round.Commit = currentCommit
		rep.Rounds = append(rep.Rounds, round)
	}

	for _, variant := range stateByVariant {
		variant.close()
	}

	rep.Summaries = summariseScenario(rep.Rounds)
	return rep, nil
}

func (h *benchmarkHarness) newVariant(ctx context.Context, scenarioDir, variantName string, iteration int) (*variantEnv, error) {
	variantDir := filepath.Join(scenarioDir, fmt.Sprintf("%s-%02d", variantName, iteration))
	if err := os.MkdirAll(variantDir, 0o755); err != nil {
		return nil, fmt.Errorf("create variant dir: %w", err)
	}
	variantDef, err := variantDefinition(variantName)
	if err != nil {
		return nil, err
	}

	v := &variantEnv{name: variantName, rootDir: variantDir}
	if variantDef.mirrorMode != "" {
		v.mirrorDir = filepath.Join(variantDir, "mirrors")
		if err := os.MkdirAll(v.mirrorDir, 0o755); err != nil {
			return nil, fmt.Errorf("create mirror dir: %w", err)
		}
	}
	return v, nil
}

func (h *benchmarkHarness) primeVariant(ctx context.Context, variant *variantEnv, commit string) error {
	_, err := h.runBootstrapWorkers(ctx, variant, 1, 0, commit, true)
	if err != nil {
		return err
	}
	return nil
}

func (h *benchmarkHarness) runVariantRound(ctx context.Context, variant *variantEnv, scenarioName string, iteration int, commit string) (variantRoundReport, error) {
	workerCount := 1
	if strings.Contains(scenarioName, "concurrent") {
		workerCount = h.cfg.concurrency
	}

	if h.upstreamTap == nil {
		return variantRoundReport{Name: variant.name}, errors.New("upstream traffic proxy is not running")
	}
	upstreamBefore := h.upstreamTap.Snapshot()

	started := time.Now()
	samples, err := h.runBootstrapWorkers(ctx, variant, workerCount, iteration, commit, false)
	roundDuration := time.Since(started)
	upstreamDelta := h.upstreamTap.Snapshot().Delta(upstreamBefore)

	report := variantRoundReport{
		Name:                    variant.name,
		RoundDurationMS:         durationMS(roundDuration),
		UpstreamRequests:        int(upstreamDelta.Connections),
		UpstreamConnections:     upstreamDelta.Connections,
		UpstreamBytesToServer:   upstreamDelta.BytesToUpstream,
		UpstreamBytesFromServer: upstreamDelta.BytesFromUpstream,
		UpstreamBytesTotal:      upstreamDelta.TotalBytes(),
		Samples:                 samples,
	}

	report.CacheSizeBytes, _ = directorySize(filepath.Join(variant.rootDir, "mirrors"))
	if err != nil {
		return report, err
	}
	return report, nil
}

func (h *benchmarkHarness) runBootstrapWorkers(ctx context.Context, variant *variantEnv, workerCount, iteration int, commit string, isPrime bool) ([]sampleReport, error) {
	results := make([]sampleReport, workerCount)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			sample, err := h.runBootstrapWorker(ctx, variant, workerCount, iteration, worker, commit, isPrime)
			results[worker] = sample
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}(worker)
	}

	wg.Wait()
	return results, firstErr
}

func (h *benchmarkHarness) runBootstrapWorker(ctx context.Context, variant *variantEnv, workerCount, iteration, worker int, commit string, isPrime bool) (sampleReport, error) {
	variantDef, err := variantDefinition(variant.name)
	if err != nil {
		return sampleReport{}, err
	}

	workerDir := filepath.Join(variant.rootDir, fmt.Sprintf("run-%02d-worker-%02d", iteration, worker))
	if isPrime {
		workerDir = filepath.Join(variant.rootDir, fmt.Sprintf("prime-worker-%02d", worker))
	}
	if err := os.RemoveAll(workerDir); err != nil {
		return sampleReport{}, fmt.Errorf("remove worker dir: %w", err)
	}
	if err := os.MkdirAll(workerDir, 0o755); err != nil {
		return sampleReport{}, fmt.Errorf("create worker dir: %w", err)
	}

	homeDir := filepath.Join(workerDir, "home")
	hooksDir := filepath.Join(workerDir, "hooks")
	pluginsDir := filepath.Join(workerDir, "plugins")
	checkoutPath := filepath.Join(workerDir, "checkout")
	tracePath := filepath.Join(workerDir, "trace2.json")
	stdoutPath := filepath.Join(workerDir, "stdout.log")
	stderrPath := filepath.Join(workerDir, "stderr.log")

	for _, dir := range []string{homeDir, hooksDir, pluginsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return sampleReport{}, fmt.Errorf("create worker support dir: %w", err)
		}
	}

	stdoutFile, err := os.Create(stdoutPath)
	if err != nil {
		return sampleReport{}, fmt.Errorf("create stdout log: %w", err)
	}
	defer func() { _ = stdoutFile.Close() }()
	stderrFile, err := os.Create(stderrPath)
	if err != nil {
		_ = stdoutFile.Close()
		return sampleReport{}, fmt.Errorf("create stderr log: %w", err)
	}
	defer func() { _ = stderrFile.Close() }()

	env := append(os.Environ(),
		"HOME="+homeDir,
		"PATH="+os.Getenv("PATH"),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_TRACE2_EVENT="+tracePath,
		"BUILDKITE_BOOTSTRAP_PHASES=checkout",
		"BUILDKITE_AGENT_NO_JOB_API=true",
		"BUILDKITE_AGENT_NO_COLOR=true",
		"BUILDKITE_HOOKS_PATH="+hooksDir,
		"BUILDKITE_PLUGINS_PATH="+pluginsDir,
		"BUILDKITE_BUILD_CHECKOUT_PATH="+checkoutPath,
		"BUILDKITE_REPO="+h.upstreamURL,
		"BUILDKITE_AGENT_NAME=benchmark-agent",
		"BUILDKITE_ORGANIZATION_SLUG=benchmark-org",
		"BUILDKITE_PIPELINE_SLUG=benchmark-pipeline",
		"BUILDKITE_PIPELINE_PROVIDER=git",
		"BUILDKITE_COMMIT="+commit,
		"BUILDKITE_COMMIT_RESOLVED=true",
		"BUILDKITE_BRANCH="+h.branch,
		fmt.Sprintf("BUILDKITE_JOB_ID=benchmark-%s-%02d-%02d", variant.name, iteration, worker),
		"BUILDKITE_GIT_CLONE_FLAGS="+variantDef.cloneFlags,
		"BUILDKITE_GIT_FETCH_FLAGS="+variantDef.fetchFlags,
		"BUILDKITE_GIT_CLONE_MIRROR_FLAGS=-v",
		"BUILDKITE_GIT_CLEAN_FLAGS=-ffxdq",
		"BUILDKITE_GIT_SUBMODULES=false",
	)
	env = append(env, "BUILDKITE_AGENT_LOG_LEVEL=error")

	if variantDef.mirrorMode != "" {
		env = append(env,
			"BUILDKITE_GIT_MIRRORS_PATH="+variant.mirrorDir,
			"BUILDKITE_GIT_MIRROR_CHECKOUT_MODE="+variantDef.mirrorMode,
		)
	}

	cmd := exec.CommandContext(ctx, h.cfg.agentBinary, "bootstrap")
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = env

	started := time.Now()
	err = cmd.Run()
	duration := time.Since(started)
	_ = stdoutFile.Close()
	_ = stderrFile.Close()

	sample := sampleReport{
		Worker:       worker,
		DurationMS:   durationMS(duration),
		CheckoutPath: checkoutPath,
		StdoutPath:   stdoutPath,
		StderrPath:   stderrPath,
		TracePath:    tracePath,
	}
	if cmd.ProcessState != nil {
		sample.UserMS = durationMS(cmd.ProcessState.UserTime())
		sample.SystemMS = durationMS(cmd.ProcessState.SystemTime())
	}
	if timings, timingErr := parseGitTraceTimings(tracePath); timingErr == nil {
		sample.GitCloneMS = timings.CloneMS
		sample.GitFetchMS = timings.FetchMS
		sample.GitCheckoutMS = timings.CheckoutMS
		sample.GitCleanMS = timings.CleanMS
	}
	if err != nil {
		sample.Error = err.Error()
		return sample, fmt.Errorf("variant %s worker %d failed: %w", variant.name, worker, err)
	}

	return sample, nil
}

func (h *benchmarkHarness) createAndPushCommit(ctx context.Context, iteration int) (string, error) {
	markerDir := filepath.Join(h.writerDir, ".git-benchmark")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		return "", fmt.Errorf("create benchmark marker dir: %w", err)
	}
	markerPath := filepath.Join(markerDir, "rounds.txt")
	f, err := os.OpenFile(markerPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("open benchmark marker: %w", err)
	}
	if _, err := fmt.Fprintf(f, "round %d %s\n", iteration, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("write benchmark marker: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close benchmark marker: %w", err)
	}

	if _, err := h.commandOutput(ctx, h.writerDir, nil, "git", "add", "--", markerPath); err != nil {
		return "", fmt.Errorf("stage benchmark marker: %w", err)
	}
	message := fmt.Sprintf("benchmark round %d", iteration)
	if _, err := h.commandOutput(ctx, h.writerDir, nil, "git", "commit", "-m", message); err != nil {
		return "", fmt.Errorf("commit benchmark marker: %w", err)
	}
	if _, err := h.commandOutput(ctx, h.writerDir, nil, "git", "push", "origin", h.branch); err != nil {
		return "", fmt.Errorf("push benchmark commit: %w", err)
	}
	return h.currentCommit(ctx)
}

func (h *benchmarkHarness) currentCommit(ctx context.Context) (string, error) {
	out, err := h.commandOutput(ctx, h.writerDir, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve current commit: %w", err)
	}
	return strings.TrimSpace(out), nil
}

func (h *benchmarkHarness) startUpstream(ctx context.Context) error {
	port, err := freePort()
	if err != nil {
		return fmt.Errorf("allocate upstream port: %w", err)
	}

	h.upstreamLog = filepath.Join(h.rootDir, "git-daemon.log")
	logFile, err := os.Create(h.upstreamLog)
	if err != nil {
		return fmt.Errorf("create upstream log: %w", err)
	}
	daemonHome := filepath.Join(h.rootDir, "git-daemon-home")
	if err := os.MkdirAll(daemonHome, 0o755); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("create git daemon home: %w", err)
	}
	if err := os.WriteFile(filepath.Join(daemonHome, ".gitconfig"), []byte("[uploadpack]\n\tallowFilter = true\n\tallowAnySHA1InWant = true\n"), 0o644); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("write git daemon config: %w", err)
	}

	cmd := exec.CommandContext(ctx,
		"git",
		"daemon",
		"--export-all",
		"--reuseaddr",
		"--base-path="+h.upstreamDir,
		"--listen=127.0.0.1",
		fmt.Sprintf("--port=%d", port),
		"--verbose",
		"--informative-errors",
		h.upstreamDir,
	)
	cmd.Env = append(os.Environ(), "HOME="+daemonHome)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("start git daemon: %w", err)
	}
	if err := logFile.Close(); err != nil {
		return fmt.Errorf("close upstream log: %w", err)
	}
	h.upstreamCmd = cmd
	h.upstreamURL = fmt.Sprintf("git://127.0.0.1:%d/%s.git", port, h.repoName)

	deadline := time.Now().Add(10 * time.Second)
	for {
		_, err := h.commandOutput(ctx, "", nil, "git", "ls-remote", h.upstreamURL, "HEAD")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for git daemon readiness: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	tap, err := newCountingProxy(fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("start upstream traffic proxy: %w", err)
	}
	h.upstreamTap = tap
	h.upstreamURL = fmt.Sprintf("git://127.0.0.1:%d/%s.git", tap.Port(), h.repoName)

	if !h.cfg.toxiproxy.enabled {
		return nil
	}

	tproxy, err := h.startToxiproxy(ctx, tap.Port())
	if err != nil {
		return err
	}
	h.tproxy = tproxy
	h.upstreamURL = fmt.Sprintf("git://127.0.0.1:%d/%s.git", tproxy.proxyPort, h.repoName)
	return nil
}

func (h *benchmarkHarness) startToxiproxy(ctx context.Context, upstreamPort int) (*toxiproxyRuntime, error) {
	if path, err := exec.LookPath("toxiproxy-server"); err == nil {
		return h.startLocalToxiproxy(ctx, path, upstreamPort)
	}
	return h.startDockerToxiproxy(ctx, upstreamPort)
}

func (h *benchmarkHarness) startLocalToxiproxy(ctx context.Context, binary string, upstreamPort int) (*toxiproxyRuntime, error) {
	adminPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocate toxiproxy admin port: %w", err)
	}
	proxyPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocate toxiproxy proxy port: %w", err)
	}

	logPath := filepath.Join(h.rootDir, "toxiproxy.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create toxiproxy log: %w", err)
	}

	cmd := exec.CommandContext(ctx, binary, "-host", "127.0.0.1", "-port", fmt.Sprintf("%d", adminPort))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start local toxiproxy-server: %w", err)
	}
	if err := logFile.Close(); err != nil {
		return nil, fmt.Errorf("close toxiproxy log: %w", err)
	}

	tproxy := &toxiproxyRuntime{
		mode:      "local-binary",
		adminURL:  fmt.Sprintf("http://127.0.0.1:%d", adminPort),
		proxyName: "git-upstream",
		proxyPort: proxyPort,
		host:      "127.0.0.1",
		cmd:       cmd,
	}

	if err := h.waitForToxiproxy(ctx, tproxy.adminURL); err != nil {
		tproxy.close()
		return nil, err
	}
	if err := h.configureToxiproxy(ctx, tproxy, fmt.Sprintf("127.0.0.1:%d", upstreamPort), false); err != nil {
		tproxy.close()
		return nil, err
	}
	return tproxy, nil
}

func (h *benchmarkHarness) startDockerToxiproxy(ctx context.Context, upstreamPort int) (*toxiproxyRuntime, error) {
	adminPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocate toxiproxy admin port: %w", err)
	}
	proxyPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocate toxiproxy proxy port: %w", err)
	}

	containerName := fmt.Sprintf("git-benchmark-toxiproxy-%d", time.Now().UnixNano())
	args := []string{
		"run", "-d", "--rm",
		"--name", containerName,
		"-p", fmt.Sprintf("%d:8474", adminPort),
		"-p", fmt.Sprintf("%d:%d", proxyPort, proxyPort),
	}
	if runtime.GOOS == "linux" {
		args = append(args, "--add-host=host.docker.internal:host-gateway")
	}
	args = append(args, h.cfg.toxiproxy.image)

	if _, err := h.commandOutput(ctx, "", nil, "docker", args...); err != nil {
		return nil, fmt.Errorf("start docker toxiproxy: %w", err)
	}

	tproxy := &toxiproxyRuntime{
		mode:      "docker",
		adminURL:  fmt.Sprintf("http://127.0.0.1:%d", adminPort),
		proxyName: "git-upstream",
		proxyPort: proxyPort,
		host:      "host.docker.internal",
		container: containerName,
	}

	if err := h.waitForToxiproxy(ctx, tproxy.adminURL); err != nil {
		tproxy.close()
		return nil, err
	}
	if err := h.configureToxiproxy(ctx, tproxy, fmt.Sprintf("%s:%d", tproxy.host, upstreamPort), true); err != nil {
		tproxy.close()
		return nil, err
	}
	return tproxy, nil
}

func (h *benchmarkHarness) waitForToxiproxy(ctx context.Context, adminURL string) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, adminURL+"/version", nil)
		if err == nil {
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode/100 == 2 {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait for toxiproxy readiness at %s", adminURL)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *benchmarkHarness) configureToxiproxy(ctx context.Context, tproxy *toxiproxyRuntime, upstream string, dockerMode bool) error {
	listenHost := "127.0.0.1"
	if dockerMode {
		listenHost = "0.0.0.0"
	}

	if err := h.toxiproxyRequest(ctx, http.MethodPost, tproxy.adminURL+"/proxies", toxiproxyCreateProxyRequest{
		Name:     tproxy.proxyName,
		Listen:   fmt.Sprintf("%s:%d", listenHost, tproxy.proxyPort),
		Upstream: upstream,
	}, nil); err != nil {
		return fmt.Errorf("create toxiproxy proxy: %w", err)
	}

	if h.cfg.toxiproxy.latencyMS > 0 {
		if err := h.toxiproxyRequest(ctx, http.MethodPost, fmt.Sprintf("%s/proxies/%s/toxics", tproxy.adminURL, tproxy.proxyName), toxiproxyCreateToxicRequest{
			Name:     "latency-downstream",
			Type:     "latency",
			Stream:   "downstream",
			Toxicity: 1,
			Attributes: map[string]any{
				"latency": h.cfg.toxiproxy.latencyMS,
				"jitter":  0,
			},
		}, nil); err != nil {
			return fmt.Errorf("create latency toxic: %w", err)
		}
	}

	if h.cfg.toxiproxy.downstreamKBPerSecond > 0 {
		if err := h.toxiproxyRequest(ctx, http.MethodPost, fmt.Sprintf("%s/proxies/%s/toxics", tproxy.adminURL, tproxy.proxyName), toxiproxyCreateToxicRequest{
			Name:     "bandwidth-downstream",
			Type:     "bandwidth",
			Stream:   "downstream",
			Toxicity: 1,
			Attributes: map[string]any{
				"rate": h.cfg.toxiproxy.downstreamKBPerSecond,
			},
		}, nil); err != nil {
			return fmt.Errorf("create bandwidth toxic: %w", err)
		}
	}

	return nil
}

func (h *benchmarkHarness) toxiproxyRequest(ctx context.Context, method, url string, reqBody, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal toxiproxy request: %w", err)
		}
		body = strings.NewReader(string(buf))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return fmt.Errorf("create toxiproxy request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("perform toxiproxy request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("toxiproxy API status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if respBody == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode toxiproxy response: %w", err)
	}
	return nil
}

func (h *benchmarkHarness) networkProfile() networkProfile {
	if !h.cfg.toxiproxy.enabled {
		return networkProfile{Mode: "loopback-direct"}
	}
	return networkProfile{
		Mode:                  "toxiproxy",
		LatencyMS:             h.cfg.toxiproxy.latencyMS,
		DownstreamKBPerSecond: h.cfg.toxiproxy.downstreamKBPerSecond,
	}
}

func (h *benchmarkHarness) commandOutput(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed == "" {
			return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, trimmed)
	}
	return string(out), nil
}

func (h *benchmarkHarness) cleanup() {
	if h.tproxy != nil {
		h.tproxy.close()
	}
	if h.upstreamTap != nil {
		_ = h.upstreamTap.Close()
	}
	if h.upstreamCmd != nil && h.upstreamCmd.Process != nil {
		_ = h.upstreamCmd.Process.Signal(syscall.SIGTERM)
		_, _ = h.upstreamCmd.Process.Wait()
	}
	if h.cfg.keepWorkDir {
		return
	}
	if h.outputPathWithinRootDir() {
		_ = removeAllExcept(h.rootDir, h.cfg.outputPath)
		return
	}
	_ = os.RemoveAll(h.rootDir)
}

func (v *variantEnv) close() {
}

func (h *benchmarkHarness) outputPathWithinRootDir() bool {
	if h.rootDir == "" || h.cfg.outputPath == "" {
		return false
	}
	return pathContains(h.rootDir, h.cfg.outputPath)
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

func (t *toxiproxyRuntime) close() {
	if t == nil {
		return
	}
	if t.container != "" {
		cmd := exec.Command("docker", "rm", "-f", t.container)
		_, _ = cmd.CombinedOutput()
		return
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Signal(syscall.SIGTERM)
		_, _ = t.cmd.Process.Wait()
	}
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

func removeAllExcept(root, keep string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		if pathContains(path, keep) {
			if entry.IsDir() {
				if err := removeAllExcept(path, keep); err != nil {
					return err
				}
			}
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}

	return nil
}

func pathContains(root, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func directorySize(paths ...string) (int64, error) {
	var total int64
	for _, root := range paths {
		if root == "" {
			continue
		}
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return 0, err
		}

		if err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				total += info.Size()
			}
			return nil
		}); err != nil {
			return 0, err
		}
	}
	return total, nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("listener addr was not TCP")
	}
	return addr.Port, nil
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
		if summary.Name == "direct" {
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

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
