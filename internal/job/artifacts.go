package job

import (
	"context"

	"github.com/buildkite/agent/v3/internal/self"
	"github.com/buildkite/agent/v3/tracetools"
)

func (e *Executor) artifactPhase(ctx context.Context) error {
	if e.AutomaticArtifactUploadPaths == "" {
		return nil
	}

	spanName := e.implementationSpecificSpanName("artifacts", "artifact upload")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, e.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	err = e.preArtifactHooks(ctx)
	if err != nil {
		return err
	}

	err = e.uploadArtifacts(ctx)
	if err != nil {
		return err
	}

	err = e.postArtifactHooks(ctx)
	if err != nil {
		return err
	}

	return nil
}

// Run the pre-artifact hooks
func (e *Executor) preArtifactHooks(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-artifact", e.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = e.executeLocalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = e.executePluginHook(ctx, "pre-artifact", e.pluginCheckouts); err != nil {
		return err
	}

	return nil
}

// Run the artifact upload command
func (e *Executor) uploadArtifacts(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "artifact-upload", e.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	e.shell.Headerf("Uploading artifacts")
	args := []string{"artifact", "upload", e.AutomaticArtifactUploadPaths}

	// If blank, the upload destination is buildkite
	if e.ArtifactUploadDestination != "" {
		args = append(args, e.ArtifactUploadDestination)
	}

	if err = e.shell.Command(self.Path(ctx), args...).Run(ctx); err != nil {
		return err
	}

	return nil
}

// Run the post-artifact hooks
func (e *Executor) postArtifactHooks(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "post-artifact", e.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = e.executeGlobalHook(ctx, "post-artifact"); err != nil {
		return err
	}

	if err = e.executeLocalHook(ctx, "post-artifact"); err != nil {
		return err
	}

	if err = e.executePluginHook(ctx, "post-artifact", e.pluginCheckouts); err != nil {
		return err
	}

	return nil
}
