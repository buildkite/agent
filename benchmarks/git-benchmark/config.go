package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

type benchmarkVariant struct {
	mirrorMode string
	cloneFlags string
	fetchFlags string
}

type benchmarkScenario struct {
	cold              bool
	concurrent        bool
	newCommitPerRound bool
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

var benchmarkScenarios = map[string]benchmarkScenario{
	"cold-single": {
		cold: true,
	},
	"warm-single": {},
	"warm-concurrent": {
		concurrent: true,
	},
	"warm-concurrent-new-commit": {
		concurrent:        true,
		newCommitPerRound: true,
	},
}

func parseConfig() (config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return config{}, fmt.Errorf("get working directory: %w", err)
	}
	return parseConfigFromArgs(os.Args[1:], cwd)
}

func parseConfigFromArgs(args []string, cwd string) (_ config, err error) {
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
	for _, scenarioName := range cfg.scenarios {
		if _, err := scenarioDefinition(scenarioName); err != nil {
			return cfg, fmt.Errorf("parse scenarios: %w", err)
		}
	}
	if cfg.agentBinary == "" {
		return cfg, errors.New("--agent-binary is required")
	}

	if cfg.workDir == "" {
		cfg.workDir, err = os.MkdirTemp("", "gcb-")
		if err != nil {
			return cfg, fmt.Errorf("create temp workdir: %w", err)
		}
		defer func() {
			if err != nil {
				_ = os.RemoveAll(cfg.workDir)
			}
		}()
	}
	if cfg.sourceRepo == "" {
		cfg.sourceRepo = cwd
	}
	if cfg.outputPath == "" {
		cfg.outputPath = filepath.Join(cfg.workDir, "report.json")
	}
	if err := normalizeConfigPaths(&cfg, cwd); err != nil {
		return cfg, err
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

func scenarioDefinition(name string) (benchmarkScenario, error) {
	scenario, ok := benchmarkScenarios[name]
	if !ok {
		return benchmarkScenario{}, fmt.Errorf("unknown scenario %q", name)
	}
	return scenario, nil
}

func normalizeConfigPaths(cfg *config, cwd string) error {
	workDir, err := resolvePathRelativeToCWD(cfg.workDir, cwd)
	if err != nil {
		return fmt.Errorf("resolve workdir: %w", err)
	}
	cfg.workDir = workDir

	outputPath, err := resolvePathRelativeToCWD(cfg.outputPath, cwd)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	cfg.outputPath = outputPath

	localSourceRepo, ok, err := resolveExistingLocalPath(cfg.sourceRepo, cwd)
	if err != nil {
		return fmt.Errorf("resolve source repo: %w", err)
	}
	if ok {
		cfg.sourceRepo = localSourceRepo
		sourceRepoComparable, err := canonicalComparablePath(localSourceRepo)
		if err != nil {
			return fmt.Errorf("canonicalise source repo: %w", err)
		}
		workDirComparable, err := canonicalComparablePath(cfg.workDir)
		if err != nil {
			return fmt.Errorf("canonicalise workdir: %w", err)
		}
		if pathContains(sourceRepoComparable, workDirComparable) || pathContains(workDirComparable, sourceRepoComparable) {
			return errors.New("source repo and workdir must not overlap")
		}
	}

	return nil
}

func resolvePathRelativeToCWD(path, cwd string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}

func resolveExistingLocalPath(path, cwd string) (string, bool, error) {
	if looksLikeRemoteRepo(path) {
		return "", false, nil
	}

	resolved, err := resolvePathRelativeToCWD(path, cwd)
	if err != nil {
		return "", false, err
	}
	if _, err := os.Stat(resolved); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return resolved, true, nil
}

func looksLikeRemoteRepo(path string) bool {
	if strings.Contains(path, "://") {
		return true
	}

	if volume := filepath.VolumeName(path); volume != "" {
		return false
	}

	colon := strings.Index(path, ":")
	if colon <= 0 {
		return false
	}

	prefix := path[:colon]
	if strings.Contains(prefix, "/") || strings.Contains(prefix, `\`) {
		return false
	}

	return strings.Contains(prefix, "@") || strings.Contains(prefix, ".")
}

func canonicalComparablePath(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return canonical, nil
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
