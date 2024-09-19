package job

import (
	"context"

	"github.com/buildkite/agent/v3/tracetools"
)

func (e *Executor) restoreCachePhase(ctx context.Context) error {
	if e.CachePaths == "" {
		return nil
	}

	spanName := e.implementationSpecificSpanName("cache", "cache restore")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	err = e.restoreCache(ctx)
	if err != nil {
		return err
	}

	return nil
}

// Run the cache restore command
func (e *Executor) restoreCache(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "cache-restore", e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	e.shell.Headerf("Restoring cache")
	args := []string{"restore", e.CachePaths}

	// If blank, the upload destination is buildkite
	if e.CacheLocalPath != "" {
		args = append(args, "--local-cache-path", e.CacheLocalPath)
	}

	if err = e.shell.Run(ctx, "zstash", args...); err != nil {
		return err
	}

	return nil
}

func (e *Executor) saveCachePhase(ctx context.Context) error {
	if e.CachePaths == "" {
		return nil
	}

	spanName := e.implementationSpecificSpanName("cache", "cache restore")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	err = e.saveCache(ctx)
	if err != nil {
		return err
	}

	return nil
}

// Run the cache restore command
func (e *Executor) saveCache(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "cache-restore", e.ExecutorConfig.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	e.shell.Headerf("Saving cache")
	args := []string{"save", e.CachePaths}

	// If blank, the upload destination is buildkite
	if e.CacheLocalPath != "" {
		args = append(args, "--local-cache-path", e.CacheLocalPath)
	}

	if err = e.shell.Run(ctx, "zstash", args...); err != nil {
		return err
	}

	return nil
}
