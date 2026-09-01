package clicommand

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
)

type profilerMode string

const (
	cpuMode          profilerMode = "cpu"
	memMode          profilerMode = "mem"
	mutexMode        profilerMode = "mutex"
	blockMode        profilerMode = "block"
	traceMode        profilerMode = "trace"
	threadCreateMode profilerMode = "thread"
)

type profiler struct {
	logger *slog.Logger
	mode   profilerMode
	closer func()
}

// Profile starts a profiling session
func Profile(l *slog.Logger, mode string) func() {
	p := profiler{logger: l}

	switch mode {
	case "cpu":
		p.mode = cpuMode
	case "mem", "memory":
		p.mode = memMode
	case "mutex":
		p.mode = mutexMode
	case "block":
		p.mode = blockMode
	case "thread":
		p.mode = threadCreateMode
	case "trace":
		p.mode = traceMode
	default:
		p.logger.Error("Unknown profile mode", "mode", mode)
		os.Exit(1)
	}

	p.Start()
	return p.Stop
}

// Stop stops the profile and flushes any unwritten data.
func (p *profiler) Stop() {
	p.closer()
}

// Start starts a new profiling session.
func (p *profiler) Start() {
	path, err := os.MkdirTemp("", "profile")
	if err != nil {
		p.logger.Error("Could not create initial output directory", "error", err)
		os.Exit(1)
	}

	// create a pprof file for the mode
	fn := filepath.Join(path, string(p.mode)+".pprof")
	f, err := os.Create(fn)
	if err != nil {
		p.logger.Error("Could not create profile", "mode", p.mode, "path", fn, "error", err)
		os.Exit(1)
	}

	// called after mode specific closers
	closer := func() {
		if err := f.Close(); err != nil {
			p.logger.Error("Failed to close profile", "path", fn, "error", err)
			os.Exit(1)
		}
		p.logger.Info("Finished profiling", "mode", p.mode, "path", fn)
	}

	must := func(err error) {
		if err != nil {
			p.logger.Error("Profiler failed", "mode", p.mode, "error", err)
			os.Exit(1)
		}
	}

	switch p.mode {
	case cpuMode:
		p.logger.Info("CPU profiling enabled", "path", fn)
		must(pprof.StartCPUProfile(f))
		p.closer = func() {
			pprof.StopCPUProfile()
			closer()
		}

	case memMode:
		p.logger.Info("Memory profiling enabled", "path", fn)
		p.closer = func() {
			must(pprof.WriteHeapProfile(f))
			closer()
		}

	case mutexMode:
		runtime.SetMutexProfileFraction(1)
		p.logger.Info("Mutex profiling enabled", "path", fn)
		p.closer = func() {
			if mp := pprof.Lookup("mutex"); mp != nil {
				must(mp.WriteTo(f, 0))
			}
			runtime.SetMutexProfileFraction(0)
			closer()
		}

	case blockMode:
		runtime.SetBlockProfileRate(1)
		p.logger.Info("Block profiling enabled", "path", fn)
		p.closer = func() {
			must(pprof.Lookup("block").WriteTo(f, 0))
			runtime.SetBlockProfileRate(0)
			closer()
		}

	case threadCreateMode:
		p.logger.Info("Thread creation profiling enabled", "path", fn)
		p.closer = func() {
			if mp := pprof.Lookup("threadcreate"); mp != nil {
				must(mp.WriteTo(f, 0))
			}
			closer()
		}

	case traceMode:
		if err := trace.Start(f); err != nil {
			p.logger.Error("Could not start profiling trace", "error", err)
			os.Exit(1)
		}
		p.logger.Info("Trace enabled", "path", fn)
		p.closer = func() {
			trace.Stop()
			closer()
		}
	}
}
