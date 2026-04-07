package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

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

func newBenchmarkHarness(ctx context.Context, cfg config) (_ *benchmarkHarness, err error) {
	rootDir, err := filepath.Abs(cfg.workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create workdir: %w", err)
	}

	h := &benchmarkHarness{cfg: cfg, rootDir: rootDir}
	defer func() {
		if err != nil {
			h.cleanup()
		}
	}()

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
	scenario, err := scenarioDefinition(scenarioName)
	if err != nil {
		return nil, err
	}

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

		for _, variantName := range h.cfg.variants {
			variant, ok := stateByVariant[variantName]
			if !ok || scenario.cold {
				variant, err = h.newVariant(ctx, scenarioDir, variantName, iteration)
				if err != nil {
					return nil, err
				}
				stateByVariant[variantName] = variant

				if !scenario.cold {
					primeCommit := currentCommit
					if scenario.newCommitPerRound {
						primeCommit = previousCommit
					}
					if err := h.primeVariant(ctx, variant, primeCommit); err != nil {
						fmt.Print("!")
						return nil, fmt.Errorf("prime %s: %w", variantName, err)
					}
					fmt.Print("p")
				}
			}
		}

		if scenario.newCommitPerRound {
			currentCommit, err = h.createAndPushCommit(ctx, iteration)
			if err != nil {
				return nil, err
			}
			round.CommitCreated = true
		}

		for _, variantName := range h.cfg.variants {
			variant := stateByVariant[variantName]
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

	rep.Summaries = summariseScenario(rep.Rounds)
	return rep, nil
}

func (h *benchmarkHarness) newVariant(_ context.Context, scenarioDir, variantName string, iteration int) (*variantEnv, error) {
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
	return err
}

func (h *benchmarkHarness) runVariantRound(ctx context.Context, variant *variantEnv, scenarioName string, iteration int, commit string) (variantRoundReport, error) {
	workerCount := 1
	scenario, err := scenarioDefinition(scenarioName)
	if err != nil {
		return variantRoundReport{Name: variant.name}, err
	}
	if scenario.concurrent {
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
			sample, err := h.runBootstrapWorker(ctx, variant, iteration, worker, commit, isPrime)
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

func (h *benchmarkHarness) runBootstrapWorker(ctx context.Context, variant *variantEnv, iteration, worker int, commit string, isPrime bool) (sampleReport, error) {
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
		"BUILDKITE_AGENT_LOG_LEVEL=error",
	)

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

	markerPathRel, err := filepath.Rel(h.writerDir, markerPath)
	if err != nil {
		return "", fmt.Errorf("resolve benchmark marker path: %w", err)
	}
	if _, err := h.commandOutput(ctx, h.writerDir, nil, "git", "add", "--", markerPathRel); err != nil {
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

func (h *benchmarkHarness) outputPathWithinRootDir() bool {
	if h.rootDir == "" || h.cfg.outputPath == "" {
		return false
	}
	return pathContains(h.rootDir, h.cfg.outputPath)
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
