package clicommand

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"

	"github.com/buildkite/agent/v3/logger"
)

type profilerMode string

const (
	cpuMode          profilerMode = `cpu`
	memMode          profilerMode = `mem`
	mutexMode        profilerMode = `mutex`
	blockMode        profilerMode = `block`
	traceMode        profilerMode = `trace`
	threadCreateMode profilerMode = `thread`
)

type profiler struct {
	logger logger.Logger
	mode   profilerMode
	closer func()
}

// Profile starts a profiling session
func Profile(l logger.Logger, mode string) func() {
	p := profiler{logger: l}

	switch mode {
	case `cpu`:
		p.mode = cpuMode
	case `mem`, `memory`:
		p.mode = memMode
	case `mutex`:
		p.mode = mutexMode
	case `block`:
		p.mode = blockMode
	case `thread`:
		p.mode = threadCreateMode
	case `trace`:
		p.mode = traceMode
	default:
		p.logger.Fatal("Unknown profile mode %q", mode)
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
	path, err := ioutil.TempDir("", "profile")
	if err != nil {
		p.logger.Fatal("Could not create initial output directory: %v", err)
	}

	// create a pprof file for the mode
	fn := filepath.Join(path, string(p.mode)+".pprof")
	f, err := os.Create(fn)
	if err != nil {
		p.logger.Fatal("Could not create %s profile %q: %v", p.mode, fn, err)
	}

	// called after mode specific closers
	closer := func() {
		if err := f.Close(); err != nil {
			p.logger.Fatal("Failed to close %s: %v", fn, err)
		}
		p.logger.Info("Finished %s profiling finished, %s", p.mode, fn)
	}

	must := func(err error) {
		if err != nil {
			p.logger.Fatal("Profiler mode %s failed: %v", p.mode, err)
		}
	}

	switch p.mode {
	case cpuMode:
		p.logger.Info("CPU profiling enabled, %s", fn)
		must(pprof.StartCPUProfile(f))
		p.closer = func() {
			pprof.StopCPUProfile()
			closer()
		}

	case memMode:
		p.logger.Info("Memory profiling enabled, %s", fn)
		p.closer = func() {
			must(pprof.WriteHeapProfile(f))
			closer()
		}

	case mutexMode:
		runtime.SetMutexProfileFraction(1)
		p.logger.Info("Mutex profiling enabled, %s", fn)
		p.closer = func() {
			if mp := pprof.Lookup("mutex"); mp != nil {
				must(mp.WriteTo(f, 0))
			}
			runtime.SetMutexProfileFraction(0)
			closer()
		}

	case blockMode:
		runtime.SetBlockProfileRate(1)
		p.logger.Info("Block profiling enabled, %s", fn)
		p.closer = func() {
			must(pprof.Lookup("block").WriteTo(f, 0))
			runtime.SetBlockProfileRate(0)
			closer()
		}

	case threadCreateMode:
		p.logger.Info("Thread creation profiling enabled, %s", fn)
		p.closer = func() {
			if mp := pprof.Lookup("threadcreate"); mp != nil {
				must(mp.WriteTo(f, 0))
			}
			closer()
		}

	case traceMode:
		if err := trace.Start(f); err != nil {
			p.logger.Fatal("Could not start profiling trace: %v", err)
		}
		p.logger.Info("Trace enabled, %s", fn)
		p.closer = func() {
			trace.Stop()
			closer()
		}
	}
}
