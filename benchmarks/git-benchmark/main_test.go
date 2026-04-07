package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type stubBenchmarkRunner struct {
	report  *report
	err     error
	cleaned bool
}

func (r *stubBenchmarkRunner) run(context.Context) (*report, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.report, nil
}

func (r *stubBenchmarkRunner) cleanup() {
	r.cleaned = true
}

func TestNewBenchmarkHarnessCleansUpWhenStartUpstreamFails(t *testing.T) {
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("exec.LookPath(go) error = %v", err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("exec.LookPath(git) error = %v", err)
	}

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(binDir) error = %v", err)
	}
	writeGitWrapper(t, binDir, gitPath)
	t.Setenv("PATH", binDir)

	sourceRepo := createBenchmarkSourceRepo(t, gitPath)
	workDir := filepath.Join(t.TempDir(), "workdir")

	_, err = newBenchmarkHarness(context.Background(), config{
		agentBinary: goPath,
		sourceRepo:  sourceRepo,
		workDir:     workDir,
		toxiproxy: toxiproxyConfig{
			enabled: true,
		},
	})
	if err == nil {
		t.Fatal("newBenchmarkHarness() error = nil, want error")
	}
	if _, statErr := os.Stat(workDir); !os.IsNotExist(statErr) {
		t.Fatalf("os.Stat(workDir) error = %v, want workdir removed after partial startup failure", statErr)
	}
}

func createBenchmarkSourceRepo(t *testing.T, gitPath string) string {
	t.Helper()

	repoDir := t.TempDir()
	runCommand(t, gitPath, "init", repoDir)
	runCommand(t, gitPath, "-C", repoDir, "config", "user.name", "Person Example")
	runCommand(t, gitPath, "-C", repoDir, "config", "user.email", "person@example.com")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("benchmark repo\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	runCommand(t, gitPath, "-C", repoDir, "add", "README.md")
	runCommand(t, gitPath, "-C", repoDir, "commit", "-m", "initial commit")

	return repoDir
}

func writeGitWrapper(t *testing.T, binDir, gitPath string) {
	t.Helper()

	var (
		wrapperPath string
		content     string
	)
	if runtime.GOOS == "windows" {
		wrapperPath = filepath.Join(binDir, "git.cmd")
		content = "@echo off\r\n\"" + gitPath + "\" %*\r\n"
	} else {
		wrapperPath = filepath.Join(binDir, "git")
		content = "#!/bin/sh\nexec " + strconv.Quote(gitPath) + " \"$@\"\n"
	}
	if err := os.WriteFile(wrapperPath, []byte(content), 0o755); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", wrapperPath, err)
	}
}

func runCommand(t *testing.T, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s error = %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func TestRunBenchmarkCleansUpWhenWriteReportFails(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	blockingPath := filepath.Join(rootDir, "reports")
	if err := os.WriteFile(blockingPath, []byte("not a directory\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	runner := &stubBenchmarkRunner{
		report: &report{
			RepoName: "agent",
			Branch:   "main",
		},
	}

	err := runBenchmark(context.Background(), config{outputPath: filepath.Join(blockingPath, "report.json")}, runner, io.Discard, false)
	if err == nil {
		t.Fatal("runBenchmark() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "create output directory") {
		t.Fatalf("runBenchmark() error = %q, want create output directory error", err)
	}
	if !runner.cleaned {
		t.Fatal("runBenchmark() did not clean up after write failure")
	}
}

func TestRunBenchmarkCleansUpWhenRunFails(t *testing.T) {
	t.Parallel()

	runner := &stubBenchmarkRunner{err: errors.New("boom")}

	err := runBenchmark(context.Background(), config{outputPath: filepath.Join(t.TempDir(), "report.json")}, runner, io.Discard, false)
	if err == nil {
		t.Fatal("runBenchmark() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "run benchmark: boom") {
		t.Fatalf("runBenchmark() error = %q, want wrapped run error", err)
	}
	if !runner.cleaned {
		t.Fatal("runBenchmark() did not clean up after run failure")
	}
}

func TestCleanupPreservesReportInsideWorkdir(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	reportPath := filepath.Join(rootDir, "report.json")
	logPath := filepath.Join(rootDir, "scenario", "worker.log")

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(reportPath, []byte("report\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(reportPath) error = %v", err)
	}
	if err := os.WriteFile(logPath, []byte("log\n"), 0o644); err != nil {
		t.Fatalf("os.WriteFile(logPath) error = %v", err)
	}

	h := benchmarkHarness{
		cfg: config{
			outputPath: reportPath,
		},
		rootDir: rootDir,
	}

	h.cleanup()

	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("os.Stat(reportPath) error = %v, want report to remain", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("os.Stat(logPath) error = %v, want file removed", err)
	}
}

func TestParseConfigFromArgsDefaultsToCurrentRepo(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	workDir := filepath.Join(t.TempDir(), "benchmark-workdir")
	agentBinary := filepath.Join(t.TempDir(), "agent-bin")

	cfg, err := parseConfigFromArgs([]string{"--workdir", workDir, "--agent-binary", agentBinary}, cwd)
	if err != nil {
		t.Fatalf("parseConfigFromArgs() error = %v", err)
	}

	if cfg.sourceRepo != cwd {
		t.Fatalf("cfg.sourceRepo = %q, want %q", cfg.sourceRepo, cwd)
	}
	if cfg.agentBinary != agentBinary {
		t.Fatalf("cfg.agentBinary = %q, want %q", cfg.agentBinary, agentBinary)
	}
	if cfg.outputPath != filepath.Join(workDir, "report.json") {
		t.Fatalf("cfg.outputPath = %q, want %q", cfg.outputPath, filepath.Join(workDir, "report.json"))
	}
	if got, want := cfg.variants, []string{"direct", "direct-shallow", "direct-blobless", "mirror-reference", "mirror-dissociate"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cfg.variants = %#v, want %#v", got, want)
	}
}

func TestParseConfigFromArgsRequiresAgentBinary(t *testing.T) {
	t.Parallel()

	_, err := parseConfigFromArgs(nil, t.TempDir())
	if err == nil {
		t.Fatal("parseConfigFromArgs() error = nil, want error")
	}
	if got, want := err.Error(), "--agent-binary is required"; got != want {
		t.Fatalf("parseConfigFromArgs() error = %q, want %q", got, want)
	}
}

func TestParseConfigFromArgsPreservesExplicitValues(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	workDir := filepath.Join(t.TempDir(), "benchmark-workdir")
	agentBinary := filepath.Join(t.TempDir(), "agent-bin")
	outputPath := filepath.Join(t.TempDir(), "report.json")
	sourceRepo := "https://github.com/buildkite/agent.git"

	cfg, err := parseConfigFromArgs([]string{
		"--workdir", workDir,
		"--agent-binary", agentBinary,
		"--source-repo", sourceRepo,
		"--output", outputPath,
	}, cwd)
	if err != nil {
		t.Fatalf("parseConfigFromArgs() error = %v", err)
	}

	if cfg.sourceRepo != sourceRepo {
		t.Fatalf("cfg.sourceRepo = %q, want %q", cfg.sourceRepo, sourceRepo)
	}
	if cfg.agentBinary != agentBinary {
		t.Fatalf("cfg.agentBinary = %q, want %q", cfg.agentBinary, agentBinary)
	}
	if cfg.outputPath != outputPath {
		t.Fatalf("cfg.outputPath = %q, want %q", cfg.outputPath, outputPath)
	}
}

func TestParseConfigFromArgsNormalisesRelativeLocalPaths(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	repoDir := filepath.Join(cwd, "repo")
	workDir := filepath.Join(t.TempDir(), "benchmark-workdir")
	agentBinary := filepath.Join(t.TempDir(), "agent-bin")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll(repoDir) error = %v", err)
	}

	cfg, err := parseConfigFromArgs([]string{
		"--workdir", workDir,
		"--agent-binary", agentBinary,
		"--source-repo", "repo",
		"--output", "report.json",
	}, cwd)
	if err != nil {
		t.Fatalf("parseConfigFromArgs() error = %v", err)
	}

	if cfg.sourceRepo != repoDir {
		t.Fatalf("cfg.sourceRepo = %q, want %q", cfg.sourceRepo, repoDir)
	}
	if cfg.outputPath != filepath.Join(cwd, "report.json") {
		t.Fatalf("cfg.outputPath = %q, want %q", cfg.outputPath, filepath.Join(cwd, "report.json"))
	}
	if cfg.workDir != workDir {
		t.Fatalf("cfg.workDir = %q, want %q", cfg.workDir, workDir)
	}
}

func TestParseConfigFromArgsRejectsOverlappingLocalSourceRepoAndWorkDir(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	repoDir := filepath.Join(cwd, "repo")
	if err := os.MkdirAll(filepath.Join(repoDir, "bench-workdir"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	_, err := parseConfigFromArgs([]string{
		"--agent-binary", filepath.Join(t.TempDir(), "agent-bin"),
		"--source-repo", repoDir,
		"--workdir", filepath.Join(repoDir, "bench-workdir"),
	}, cwd)
	if err == nil {
		t.Fatal("parseConfigFromArgs() error = nil, want error")
	}
	if got, want := err.Error(), "source repo and workdir must not overlap"; got != want {
		t.Fatalf("parseConfigFromArgs() error = %q, want %q", got, want)
	}
}

func TestParseConfigFromArgsRemovesTempWorkdirWhenNormalisationFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink-loop normalisation failure is not reliable on Windows")
	}

	rootDir := t.TempDir()
	loopPath := filepath.Join(rootDir, "loop")
	if err := os.Symlink(loopPath, loopPath); err != nil {
		t.Skipf("os.Symlink() unavailable: %v", err)
	}

	cfg, err := parseConfigFromArgs([]string{
		"--agent-binary", filepath.Join(t.TempDir(), "agent-bin"),
		"--source-repo", loopPath,
	}, rootDir)
	if err == nil {
		t.Fatal("parseConfigFromArgs() error = nil, want error")
	}
	if cfg.workDir == "" {
		t.Fatal("parseConfigFromArgs() workDir = empty, want temp workdir for cleanup assertion")
	}
	if _, statErr := os.Stat(cfg.workDir); !os.IsNotExist(statErr) {
		t.Fatalf("os.Stat(cfg.workDir) error = %v, want temp workdir removed after failure", statErr)
	}
}

func TestParseConfigFromArgsRejectsUnknownVariant(t *testing.T) {
	t.Parallel()

	_, err := parseConfigFromArgs([]string{
		"--agent-binary", filepath.Join(t.TempDir(), "agent-bin"),
		"--variants", "direct,unknown",
	}, t.TempDir())
	if err == nil {
		t.Fatal("parseConfigFromArgs() error = nil, want error")
	}
	if got, want := err.Error(), "parse variants: unknown variant \"unknown\""; got != want {
		t.Fatalf("parseConfigFromArgs() error = %q, want %q", got, want)
	}
}

func TestParseConfigFromArgsRejectsUnknownScenario(t *testing.T) {
	t.Parallel()

	_, err := parseConfigFromArgs([]string{
		"--agent-binary", filepath.Join(t.TempDir(), "agent-bin"),
		"--scenarios", "cold-single,unknown",
	}, t.TempDir())
	if err == nil {
		t.Fatal("parseConfigFromArgs() error = nil, want error")
	}
	if got, want := err.Error(), "parse scenarios: unknown scenario \"unknown\""; got != want {
		t.Fatalf("parseConfigFromArgs() error = %q, want %q", got, want)
	}
}

func TestVariantDefinition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want benchmarkVariant
	}{
		{
			name: "direct-shallow",
			want: benchmarkVariant{
				cloneFlags: "-v --depth=1",
				fetchFlags: "-v --prune --depth=1",
			},
		},
		{
			name: "direct-blobless",
			want: benchmarkVariant{
				cloneFlags: "-v --filter=blob:none",
				fetchFlags: "-v --prune --filter=blob:none",
			},
		},
		{
			name: "mirror-dissociate",
			want: benchmarkVariant{
				mirrorMode: "dissociate",
				cloneFlags: "-v",
				fetchFlags: "-v --prune",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := variantDefinition(tt.name)
			if err != nil {
				t.Fatalf("variantDefinition() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("variantDefinition() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestPrintSummaryToSortsRowsAndShowsDeltas(t *testing.T) {
	t.Parallel()

	rep := &report{
		RepoName:     "agent",
		Branch:       "main",
		AgentVersion: "agent-version",
		GitVersion:   "git version",
		SourceRepo:   "https://example.com/repo.git",
		UpstreamURL:  "git://127.0.0.1:9418/repo.git",
		GeneratedAt:  time.Unix(0, 0),
		Scenarios: []scenarioReport{{
			Name: "warm-single",
			Summaries: []variantSummary{
				{Name: "direct", RoundP50MS: 1000, RoundP95MS: 1100, MeanUpstreamRequests: 2, MeanUpstreamBytes: 10 * (1 << 20)},
				{Name: "mirror-reference", RoundP50MS: 300, RoundP95MS: 330, MeanUpstreamRequests: 2, MeanUpstreamBytes: 1 * (1 << 20)},
				{Name: "direct-shallow", RoundP50MS: 500, RoundP95MS: 1000, MeanUpstreamRequests: 2, MeanUpstreamBytes: 4 * (1 << 20)},
			},
		}},
	}

	var out bytes.Buffer
	printSummaryTo(&out, rep, false)
	got := out.String()

	if !strings.Contains(got, "Best latency: mirror-reference (70.0% faster than direct)") {
		t.Fatalf("summary missing best latency insight:\n%s", got)
	}
	if !strings.Contains(got, "Lowest upstream: mirror-reference (90.0% less MiB than direct)") {
		t.Fatalf("summary missing lowest upstream insight:\n%s", got)
	}
	if !strings.Contains(got, "Worst tail: direct-shallow (2.00x p95/p50)") {
		t.Fatalf("summary missing worst tail insight:\n%s", got)
	}

	mirrorIdx := strings.Index(got, "mirror-reference")
	shallowIdx := strings.Index(got, "direct-shallow")
	directIdx := strings.LastIndex(got, "direct")
	if mirrorIdx == -1 || shallowIdx == -1 || directIdx == -1 {
		t.Fatalf("summary missing expected variant rows:\n%s", got)
	}
	if mirrorIdx >= shallowIdx || shallowIdx >= directIdx {
		t.Fatalf("summary rows not sorted by latency:\n%s", got)
	}
	if !strings.Contains(got, "-70.0%") || !strings.Contains(got, "-60.0%") {
		t.Fatalf("summary missing expected delta values:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("summary unexpectedly contains ANSI escapes when colour disabled:\n%q", got)
	}
}

func TestPrintSummaryToUsesANSIColourWhenEnabled(t *testing.T) {
	t.Parallel()

	rep := &report{
		RepoName: "agent",
		Branch:   "main",
		Scenarios: []scenarioReport{{
			Name: "warm-single",
			Summaries: []variantSummary{
				{Name: "direct", RoundP50MS: 1000, RoundP95MS: 1100, MeanUpstreamRequests: 2, MeanUpstreamBytes: 10 * (1 << 20)},
				{Name: "mirror-reference", RoundP50MS: 300, RoundP95MS: 330, MeanUpstreamRequests: 2, MeanUpstreamBytes: 1 * (1 << 20)},
				{Name: "broken", Failures: 1, RoundP50MS: 1200, RoundP95MS: 4000, MeanUpstreamRequests: 3, MeanUpstreamBytes: 12 * (1 << 20)},
			},
		}},
	}

	var out bytes.Buffer
	printSummaryTo(&out, rep, true)
	if !strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("summary missing ANSI colour output:\n%q", out.String())
	}
}

func TestPrintSummaryToSkipsBrokenDirectBaseline(t *testing.T) {
	t.Parallel()

	rep := &report{
		RepoName: "agent",
		Branch:   "main",
		Scenarios: []scenarioReport{{
			Name: "warm-single",
			Summaries: []variantSummary{
				{Name: "direct", Failures: 1, RoundP50MS: 1000, RoundP95MS: 1000, MeanUpstreamRequests: 2, MeanUpstreamBytes: 10 * (1 << 20)},
				{Name: "mirror-reference", RoundP50MS: 300, RoundP95MS: 450, MeanUpstreamRequests: 2, MeanUpstreamBytes: 1 * (1 << 20)},
			},
		}},
	}

	var out bytes.Buffer
	printSummaryTo(&out, rep, false)
	got := out.String()

	if strings.Contains(got, "faster than direct") || strings.Contains(got, "less MiB than direct") {
		t.Fatalf("summary should not compare against a failing direct baseline:\n%s", got)
	}
	if !strings.Contains(got, "Best latency: mirror-reference (300.0ms)") {
		t.Fatalf("summary missing absolute latency headline:\n%s", got)
	}
	if !strings.Contains(got, "         —") {
		t.Fatalf("summary missing em-dash delta cells when direct baseline is unavailable:\n%s", got)
	}
}

func TestParseGitTraceTimings(t *testing.T) {
	t.Parallel()

	tracePath := filepath.Join(t.TempDir(), "trace2.json")
	trace := "" +
		"{\"event\":\"cmd_name\",\"sid\":\"clone-sid\",\"name\":\"clone\"}\n" +
		"{\"event\":\"cmd_name\",\"sid\":\"fetch-sid\",\"name\":\"fetch\"}\n" +
		"{\"event\":\"cmd_name\",\"sid\":\"clean-one\",\"name\":\"clean\"}\n" +
		"{\"event\":\"cmd_name\",\"sid\":\"clean-two\",\"name\":\"clean\"}\n" +
		"{\"event\":\"cmd_name\",\"sid\":\"checkout-sid\",\"name\":\"checkout\"}\n" +
		"{\"event\":\"exit\",\"sid\":\"clone-sid\",\"t_abs\":1.5}\n" +
		"{\"event\":\"exit\",\"sid\":\"fetch-sid\",\"t_abs\":0.25}\n" +
		"{\"event\":\"exit\",\"sid\":\"clean-one\",\"t_abs\":0.01}\n" +
		"{\"event\":\"exit\",\"sid\":\"clean-two\",\"t_abs\":0.02}\n" +
		"{\"event\":\"exit\",\"sid\":\"checkout-sid\",\"t_abs\":0.125}\n" +
		"{\"event\":\"cmd_name\",\"sid\":\"clone-sid/child\",\"name\":\"index-pack\"}\n" +
		"{\"event\":\"exit\",\"sid\":\"clone-sid/child\",\"t_abs\":9}\n"
	if err := os.WriteFile(tracePath, []byte(trace), 0o644); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	timings, err := parseGitTraceTimings(tracePath)
	if err != nil {
		t.Fatalf("parseGitTraceTimings() error = %v", err)
	}

	if timings.CloneMS != 1500 {
		t.Fatalf("timings.CloneMS = %v, want %v", timings.CloneMS, 1500.0)
	}
	if timings.FetchMS != 250 {
		t.Fatalf("timings.FetchMS = %v, want %v", timings.FetchMS, 250.0)
	}
	if timings.CheckoutMS != 125 {
		t.Fatalf("timings.CheckoutMS = %v, want %v", timings.CheckoutMS, 125.0)
	}
	if timings.CleanMS != 30 {
		t.Fatalf("timings.CleanMS = %v, want %v", timings.CleanMS, 30.0)
	}
}

func TestWaitForToxiproxyReturnsPromptlyWhenContextCancelled(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	started := time.Now()
	err := (&benchmarkHarness{}).waitForToxiproxy(ctx, server.URL)
	elapsed := time.Since(started)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForToxiproxy() error = %v, want context deadline exceeded", err)
	}
	if elapsed > time.Second {
		t.Fatalf("waitForToxiproxy() took %v, want prompt return after context cancellation", elapsed)
	}
}
