package bootstrap

import (
	"context"

	"github.com/buildkite/agent/v3/tracetools"
)

func (b *Bootstrap) artifactPhase(ctx context.Context) error {
	if b.AutomaticArtifactUploadPaths == "" {
		return nil
	}

	spanName := b.implementationSpecificSpanName("artifacts", "artifact upload")
	span, ctx := tracetools.StartSpanFromContext(ctx, spanName, b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	err = b.preArtifactHooks(ctx)
	if err != nil {
		return err
	}

	err = b.uploadArtifacts(ctx)
	if err != nil {
		return err
	}

	err = b.postArtifactHooks(ctx)
	if err != nil {
		return err
	}

	return nil
}

// Run the pre-artifact hooks
func (b *Bootstrap) preArtifactHooks(ctx context.Context) error {
	span, ctx := tracetools.StartSpanFromContext(ctx, "pre-artifact", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = b.executeGlobalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = b.executeLocalHook(ctx, "pre-artifact"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "pre-artifact", b.pluginCheckouts); err != nil {
		return err
	}

	return nil
}

// Run the artifact upload command
func (b *Bootstrap) uploadArtifacts(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "artifact-upload", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	b.shell.Headerf("Uploading artifacts")
	args := []string{"artifact", "upload", b.AutomaticArtifactUploadPaths}

	// If blank, the upload destination is buildkite
	if b.ArtifactUploadDestination != "" {
		args = append(args, b.ArtifactUploadDestination)
	}

	if err = b.shell.Run("buildkite-agent", args...); err != nil {
		return err
	}

	return nil
}

// Run the post-artifact hooks
func (b *Bootstrap) postArtifactHooks(ctx context.Context) error {
	span, _ := tracetools.StartSpanFromContext(ctx, "post-artifact", b.Config.TracingBackend)
	var err error
	defer func() { span.FinishWithError(err) }()

	if err = b.executeGlobalHook(ctx, "post-artifact"); err != nil {
		return err
	}

	if err = b.executeLocalHook(ctx, "post-artifact"); err != nil {
		return err
	}

	if err = b.executePluginHook(ctx, "post-artifact", b.pluginCheckouts); err != nil {
		return err
	}

	return nil
}
