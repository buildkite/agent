package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

type benchmarkRunner interface {
	run(context.Context) (*report, error)
	cleanup()
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err := runApp(ctx, os.Stdout, shouldUseColour(os.Stdout))
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fatalf("%v", err)
	}
}

func runApp(ctx context.Context, stdout io.Writer, useColour bool) error {
	cfg, err := parseConfig()
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("parse config: %w", err)
	}

	h, err := newBenchmarkHarness(ctx, cfg)
	if err != nil {
		return fmt.Errorf("initialise harness: %w", err)
	}

	return runBenchmark(ctx, cfg, h, stdout, useColour)
}

func runBenchmark(ctx context.Context, cfg config, runner benchmarkRunner, stdout io.Writer, useColour bool) error {
	defer runner.cleanup()

	report, err := runner.run(ctx)
	if err != nil {
		return fmt.Errorf("run benchmark: %w", err)
	}
	if err := writeReport(cfg.outputPath, report); err != nil {
		return err
	}

	printSummaryTo(stdout, report, useColour)
	_, _ = fmt.Fprintf(stdout, "\nReport written to %s\n", cfg.outputPath)
	return nil
}

func writeReport(path string, rep *report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	buf, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
