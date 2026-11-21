package clicommand

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/cache/store"
	"github.com/buildkite/agent/v4/internal/reuse"
	"github.com/buildkite/agent/v4/internal/self"
	"github.com/buildkite/agent/v4/logger"
	"github.com/buildkite/roko"
	"github.com/urfave/cli/v3"
	"go.opentelemetry.io/otel"
)

const (
	// reuseRetention is how long stored step outputs are kept.
	reuseRetention = 30 * 24 * time.Hour

	reuseNoStoreReason = "no cache store configured (BUILDKITE_AGENT_CACHE_STORE_URL)"
)

const reuseRestoreHelpDescription = `Usage:

    buildkite-agent reuse restore [options...]

Description:

Restores the outputs of upstream steps that the current step declares in
′inputs.steps′, according to the job's step reuse plan. The job executor runs
this before the command phase when step reuse is enabled for the job.

If an upstream output is unavailable, it is reported to Buildkite, which can
schedule a repair run, and the command exits with status 94.

When the agent verifies job signatures (′--verification-jwks-file′ or a KMS key),
each upstream result's signed manifest must verify and cover the paths being
restored; otherwise the output is treated as unavailable.`

const reuseSaveHelpDescription = `Usage:

    buildkite-agent reuse save --exit-status <status> [options...]

Description:

Stores the current step's ′outputs.paths′ and reports the job's result for step
reuse, according to the job's step reuse plan. The job executor runs this after
the command phase when step reuse is enabled for the job.

When the job has a pipeline signing key (′--jwks-file′ or a KMS key), the result
is reported with a signed manifest of its exit status, output paths and values.

Failures are logged as warnings; this command never fails the job.`

type ReuseRestoreConfig struct {
	GlobalConfig
	APIConfig

	Job       string `cli:"job" validate:"required"`
	BucketURL string `cli:"cache-store-url"`

	VerificationJWKSFile string `cli:"verification-jwks-file" normalize:"filepath"`
	SigningAWSKMSKey     string `cli:"signing-aws-kms-key"`
	SigningGCPKMSKey     string `cli:"signing-gcp-kms-key"`
}

type ReuseSaveConfig struct {
	GlobalConfig
	APIConfig

	Job        string `cli:"job" validate:"required"`
	BucketURL  string `cli:"cache-store-url"`
	ExitStatus int    `cli:"exit-status"`

	JWKSFile         string `cli:"jwks-file" normalize:"filepath"`
	JWKSKeyID        string `cli:"jwks-key-id"`
	SigningAWSKMSKey string `cli:"signing-aws-kms-key"`
	SigningGCPKMSKey string `cli:"signing-gcp-kms-key"`
}

func reuseFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "job",
			Value:   "",
			Usage:   "The job to restore or save step reuse outputs for",
			Sources: cli.EnvVars("BUILDKITE_JOB_ID"),
		},
		&cli.StringFlag{
			Name:    "cache-store-url",
			Value:   "",
			Usage:   "The URL of the blob store for step outputs (e.g., s3://bucket-name); uploads/downloads use ambient credentials",
			Sources: cli.EnvVars("BUILDKITE_AGENT_CACHE_STORE_URL"),
		},
	}
}

// reuseKMSFlags are the KMS keys that sign pipelines, which the agent also
// verifies jobs with.
func reuseKMSFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "signing-aws-kms-key",
			Usage:   "The AWS KMS key identifier which is used to sign and verify step reuse results",
			Sources: cli.EnvVars("BUILDKITE_AGENT_AWS_KMS_KEY"),
		},
		&cli.StringFlag{
			Name:    "signing-gcp-kms-key",
			Usage:   "The GCP KMS key identifier which is used to sign and verify step reuse results",
			Sources: cli.EnvVars("BUILDKITE_AGENT_GCP_KMS_KEY"),
		},
	}
}

var ReuseRestoreCommand = &cli.Command{
	Name:        "restore",
	Usage:       "Restore upstream step outputs before the command runs",
	Description: reuseRestoreHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), reuseFlags(), reuseKMSFlags(), []cli.Flag{
		&cli.StringFlag{
			Name:    "verification-jwks-file",
			Usage:   "Path to a JWKS file used to verify the signed results of upstream steps. The agent passes its own to jobs",
			Sources: cli.EnvVars("BUILDKITE_AGENT_VERIFICATION_JWKS_FILE"),
		},
	}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[ReuseRestoreConfig](ctx, c)
		defer done()
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "reuse-restore")
		defer span.End()

		start := time.Now()
		defer func() { writeReuseRestoreElapsed(l, cfg.Job, time.Since(start)) }()

		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		plan, err := getReusePlan(ctx, l, client, cfg.Job)
		if err != nil {
			l.Warnf("Step reuse: couldn't get the step reuse plan, not restoring inputs: %v", err)
			return nil
		}

		r := &reuseRestorer{l: l, client: client, jobID: cfg.Job, bucketURL: cfg.BucketURL}

		keySet, err := loadReuseVerificationKeys(ctx, cfg)
		if err != nil {
			l.Errorf("Step reuse: couldn't load the keys to verify upstream results, not restoring inputs: %v", err)
			return err
		}
		if keySet != nil {
			for _, entry := range plan.Restore {
				if path, reason := verifyRestoreSignature(keySet, entry); reason != "" {
					return r.inputUnavailable(ctx, entry.StepKey, path, reason)
				}
			}
		}

		for _, entry := range plan.Restore {
			for _, p := range entry.Paths {
				reason, err := r.restorePath(ctx, entry.StepKey, p)
				if reason != "" {
					return r.inputUnavailable(ctx, entry.StepKey, p.Path, reason)
				}
				if err != nil {
					l.Warnf("Step reuse: couldn't restore %s from step %s: %v", p.Path, entry.StepKey, err)
				}
			}
			if entry.Download {
				r.downloadArtifacts(ctx, entry.StepKey)
			}
		}
		return nil
	},
}

var ReuseSaveCommand = &cli.Command{
	Name:        "save",
	Usage:       "Store step outputs and report the result for step reuse",
	Description: reuseSaveHelpDescription,
	Flags: slices.Concat(globalFlags(), apiFlags(), reuseFlags(), reuseKMSFlags(), []cli.Flag{
		&cli.IntFlag{
			Name:  "exit-status",
			Usage: "The exit status of the job's command",
		},
		&cli.StringFlag{
			Name:    "jwks-file",
			Usage:   "Path to a JWKS file used to sign the step reuse result. The agent passes its pipeline signing key to jobs",
			Sources: cli.EnvVars("BUILDKITE_AGENT_JWKS_FILE"),
		},
		&cli.StringFlag{
			Name:    "jwks-key-id",
			Usage:   "The JWKS key ID to sign the step reuse result with",
			Sources: cli.EnvVars("BUILDKITE_AGENT_JWKS_KEY_ID"),
		},
	}),
	Action: func(ctx context.Context, c *cli.Command) error {
		ctx, cfg, l, _, done := setupLoggerAndConfig[ReuseSaveConfig](ctx, c)
		defer done()
		ctx, span := otel.Tracer("buildkite-agent").Start(ctx, "reuse-save")
		defer span.End()

		start := time.Now()
		client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
		plan, err := getReusePlan(ctx, l, client, cfg.Job)
		if err != nil {
			l.Warnf("Step reuse: couldn't get the step reuse plan, not saving result: %v", err)
			return nil
		}

		if !plan.Save {
			reason := "saving is disabled for this job"
			if plan.SaveDisabledReason != nil && *plan.SaveDisabledReason != "" {
				reason = *plan.SaveDisabledReason
			}
			if cfg.ExitStatus != 0 || len(plan.Outputs.Paths) == 0 {
				l.Infof("Step reuse: not saving result: %s", reason)
				return nil
			}
			// Later steps in this build still restore the output paths.
			l.Infof("Step reuse: not saving result (%s); storing output paths for later steps in this build", reason)
		}

		result := &api.ReuseResult{ExitStatus: cfg.ExitStatus, Paths: []api.ReusePath{}}
		if cfg.ExitStatus == 0 {
			s := &reuseSaver{l: l, bucketURL: cfg.BucketURL}
			s.save(ctx, plan, result)
		}
		signReuseResult(ctx, l, cfg, plan, result)
		result.OverheadMS = (readReuseRestoreElapsed(cfg.Job) + time.Since(start)).Milliseconds()

		resp, err := submitReuseResult(ctx, l, client, cfg.Job, result)
		if err != nil {
			l.Warnf("Step reuse: couldn't report the result: %v", err)
			return nil
		}
		switch {
		case resp.Saved:
			l.Infof("Step reuse: result saved under action key %s", resp.ActionKey)
		case resp.Reason != "":
			l.Infof("Step reuse: result not saved: %s", resp.Reason)
		default:
			l.Infof("Step reuse: result not saved")
		}
		return nil
	},
}

// signReuseResult signs a manifest of result, if the job has a signing key.
// Failures are logged, and the result is reported unsigned.
func signReuseResult(ctx context.Context, l logger.Logger, cfg ReuseSaveConfig, plan *api.ReusePlan, result *api.ReuseResult) {
	key, err := loadSigningKey(ctx, cfg.SigningAWSKMSKey, cfg.SigningGCPKMSKey, cfg.JWKSFile, cfg.JWKSKeyID)
	if err != nil {
		l.Warnf("Step reuse: couldn't load the signing key, reporting the result unsigned: %v", err)
		return
	}
	if key == nil {
		return
	}
	m := reuse.Manifest{JobID: cfg.Job, StepKey: plan.StepKey, ExitStatus: result.ExitStatus, Values: plan.Values}
	for _, p := range result.Paths {
		m.Paths = append(m.Paths, reuse.ManifestPath{Path: p.Path, Digest: p.Digest})
	}
	sig, err := reuse.SignManifest(key, m)
	if err != nil {
		l.Warnf("Step reuse: couldn't sign the result, reporting it unsigned: %v", err)
		return
	}
	result.Signature = &sig
}

// loadReuseVerificationKeys loads the keys the agent verifies jobs with, in
// the same order of precedence as `agent start`. It returns nil if there are
// none.
func loadReuseVerificationKeys(ctx context.Context, cfg ReuseRestoreConfig) (any, error) {
	if cfg.SigningAWSKMSKey != "" || cfg.SigningGCPKMSKey != "" {
		key, err := loadSigningKey(ctx, cfg.SigningAWSKMSKey, cfg.SigningGCPKMSKey, "", "")
		if err != nil {
			return nil, err
		}
		return key, nil
	}
	if cfg.VerificationJWKSFile != "" {
		return parseAndValidateJWKS(ctx, "verification", cfg.VerificationJWKSFile)
	}
	return nil, nil
}

// verifyRestoreSignature checks that an upstream result's signed manifest
// covers every path to restore from it. It returns the first path and a
// reason when it doesn't.
func verifyRestoreSignature(keySet any, entry api.ReuseRestore) (path, reason string) {
	if len(entry.Paths) == 0 {
		return "", ""
	}
	if entry.Signature == nil || *entry.Signature == "" {
		return entry.Paths[0].Path, "result signature missing"
	}
	m, err := reuse.VerifyManifest(keySet, *entry.Signature)
	if err != nil {
		return entry.Paths[0].Path, fmt.Sprintf("result signature invalid: %v", err)
	}
	if m.StepKey != entry.StepKey {
		return entry.Paths[0].Path, fmt.Sprintf("result signature invalid: signed for step %q", m.StepKey)
	}
	for _, p := range entry.Paths {
		if !m.Covers(p.Path, p.Digest) {
			return p.Path, fmt.Sprintf("result signature invalid: doesn't cover %s with digest %s", p.Path, p.Digest)
		}
	}
	return "", ""
}

func reuseRetrier() *roko.Retrier {
	return roko.NewRetrier(
		roko.WithMaxAttempts(5),
		roko.WithStrategy(roko.ExponentialSubsecond(2*time.Second)),
	)
}

func getReusePlan(ctx context.Context, l logger.Logger, client *api.Client, jobID string) (*api.ReusePlan, error) {
	plan, _, err := roko.DoFunc2(ctx, reuseRetrier(), func(r *roko.Retrier) (*api.ReusePlan, *api.Response, error) {
		plan, resp, err := client.GetReusePlan(ctx, jobID)
		if api.BreakOnNonRetryable(r, resp, err) {
			return nil, resp, err
		}
		if err != nil {
			l.Warnf("%s (%s)", err, r)
		}
		return plan, resp, err
	})
	return plan, err
}

func submitReuseResult(ctx context.Context, l logger.Logger, client *api.Client, jobID string, result *api.ReuseResult) (*api.ReuseResultResponse, error) {
	resp, _, err := roko.DoFunc2(ctx, reuseRetrier(), func(r *roko.Retrier) (*api.ReuseResultResponse, *api.Response, error) {
		out, resp, err := client.SubmitReuseResult(ctx, jobID, result)
		if api.BreakOnNonRetryable(r, resp, err) {
			return nil, resp, err
		}
		if err != nil {
			l.Warnf("%s (%s)", err, r)
		}
		return out, resp, err
	})
	return resp, err
}

// reuseStatePath is where `reuse restore` records how long it took, so that
// `reuse save` can report the total overhead of step reuse.
func reuseStatePath(jobID string) string {
	return filepath.Join(os.TempDir(), "buildkite-step-reuse-"+filepath.Base(jobID)+".ms")
}

func writeReuseRestoreElapsed(l logger.Logger, jobID string, elapsed time.Duration) {
	if err := os.WriteFile(reuseStatePath(jobID), []byte(strconv.FormatInt(elapsed.Milliseconds(), 10)), 0o600); err != nil {
		l.Warnf("Step reuse: couldn't record restore time: %v", err)
	}
}

// readReuseRestoreElapsed reads and removes the time recorded by `reuse
// restore`, returning zero if there is none.
func readReuseRestoreElapsed(jobID string) time.Duration {
	p := reuseStatePath(jobID)
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	_ = os.Remove(p)
	ms, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

// cleanOutputPath validates an output path from the plan, which must be
// relative to the checkout and stay within it. It returns the cleaned path
// using OS separators.
func cleanOutputPath(p string) (string, error) {
	trimmed := strings.TrimSuffix(p, "/")
	if trimmed == "" || path.IsAbs(trimmed) || filepath.IsAbs(trimmed) {
		return "", fmt.Errorf("output path %q must be relative to the checkout", p)
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("output path %q must be within the checkout", p)
	}
	return filepath.FromSlash(cleaned), nil
}

// reuseStoreKey is the blob store key of an output archive with digest.
func reuseStoreKey(digest string) string {
	return "step-reuse/" + strings.TrimPrefix(digest, reuse.DigestPrefix) + ".tar.gz"
}

type reuseRestorer struct {
	l         logger.Logger
	client    *api.Client
	jobID     string
	bucketURL string
	blob      store.Blob
}

func (r *reuseRestorer) store(ctx context.Context) (store.Blob, error) {
	if r.blob == nil {
		b, err := store.NewBlobStore(ctx, store.AgentManaged, r.bucketURL)
		if err != nil {
			return nil, err
		}
		r.blob = b
	}
	return r.blob, nil
}

// restorePath restores one upstream output path into the checkout. It returns
// a non-empty reason if the output is unavailable, or an error for any other
// failure.
func (r *reuseRestorer) restorePath(ctx context.Context, stepKey string, p api.ReusePath) (unavailable string, err error) {
	dest, err := cleanOutputPath(p.Path)
	if err != nil {
		return "", err
	}
	if r.bucketURL == "" {
		return reuseNoStoreReason, nil
	}
	blob, err := r.store(ctx)
	if err != nil {
		return fmt.Sprintf("couldn't open the cache store: %v", err), nil
	}

	archive, err := os.CreateTemp("", "buildkite-step-reuse-*.tar.gz")
	if err != nil {
		return "", err
	}
	_ = archive.Close()
	defer func() { _ = os.Remove(archive.Name()) }()

	if _, err := blob.Download(ctx, p.Key, archive.Name()); err != nil {
		if errors.Is(err, store.ErrBlobNotFound) {
			return store.ErrBlobNotFound.Error(), nil
		}
		return fmt.Sprintf("download failed: %v", err), nil
	}

	// Unpack next to the destination, so it can be moved into place with a
	// rename.
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, ".buildkite-step-reuse-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	unpacked := filepath.Join(tmp, "out")
	if err := reuse.Unpack(archive.Name(), unpacked); err != nil {
		return fmt.Sprintf("couldn't unpack the archive: %v", err), nil
	}

	content := unpacked
	if !strings.HasSuffix(p.Path, "/") {
		// A single file is archived under its base name.
		single := filepath.Join(unpacked, filepath.Base(dest))
		entries, err := os.ReadDir(unpacked)
		if err == nil && len(entries) == 1 && entries[0].Name() == filepath.Base(dest) && !entries[0].IsDir() {
			content = single
		}
	}

	digest, files, err := reuse.TreeDigest(content)
	if err != nil {
		return fmt.Sprintf("couldn't digest the unpacked content: %v", err), nil
	}
	if digest != p.Digest {
		return fmt.Sprintf("digest mismatch: got %s, want %s", digest, p.Digest), nil
	}

	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := os.Rename(content, dest); err != nil {
		return "", err
	}
	r.l.Infof("Restored %s from step %s (%d files, %s)", p.Path, stepKey, files, digest)
	return "", nil
}

func (r *reuseRestorer) inputUnavailable(ctx context.Context, stepKey, outputPath, reason string) error {
	r.l.Errorf("Step reuse: upstream output %s from step %s is unavailable (%s); requesting a repair run", outputPath, stepKey, reason)
	report := &api.ReuseInputUnavailable{StepKey: stepKey, Path: outputPath, Reason: reason}
	resp, _, err := roko.DoFunc2(ctx, reuseRetrier(), func(rt *roko.Retrier) (*api.ReuseInputUnavailableResponse, *api.Response, error) {
		out, resp, err := r.client.ReportReuseInputUnavailable(ctx, r.jobID, report)
		if api.BreakOnNonRetryable(rt, resp, err) {
			return nil, resp, err
		}
		if err != nil {
			r.l.Warnf("%s (%s)", err, rt)
		}
		return out, resp, err
	})
	switch {
	case err != nil:
		r.l.Warnf("Step reuse: couldn't report the unavailable input: %v", err)
	case resp.Message != "":
		r.l.Infof("Step reuse: %s", resp.Message)
	}
	return NewSilentExitError(reuse.ExitCodeInputUnavailable)
}

// downloadArtifacts downloads all artifacts of an upstream step into the
// checkout, using the agent's own artifact download command.
func (r *reuseRestorer) downloadArtifacts(ctx context.Context, stepKey string) {
	r.l.Infof("Step reuse: downloading artifacts from step %s", stepKey)
	cmd := exec.CommandContext(ctx, self.Path(ctx), "artifact", "download", "*", ".", "--step", stepKey)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		r.l.Warnf("Step reuse: couldn't download artifacts from step %s: %v", stepKey, err)
	}
}

type reuseSaver struct {
	l         logger.Logger
	bucketURL string
}

// save verifies the input files and stores the output paths, filling in
// result. Failures are logged, and recorded in result.DiscardReason when the
// result must not be saved.
func (s *reuseSaver) save(ctx context.Context, plan *api.ReusePlan, result *api.ReuseResult) {
	if plan.Files != nil {
		res, err := reuse.HashFiles(ctx, ".", plan.Files.Include, plan.Files.Exclude, plan.Files.Untracked)
		if err != nil {
			s.l.Warnf("Step reuse: couldn't hash input files: %v", err)
		} else {
			result.VerifiedFilesDigest = &res.Digest
		}
	}

	if len(plan.Outputs.Paths) == 0 {
		return
	}
	discard := func(reason string) {
		s.l.Warnf("Step reuse: %s", reason)
		result.DiscardReason = &reason
		result.Paths = []api.ReusePath{}
	}
	if s.bucketURL == "" {
		discard(reuseNoStoreReason)
		return
	}

	// Check every path exists before uploading any.
	dests := make([]string, 0, len(plan.Outputs.Paths))
	for _, p := range plan.Outputs.Paths {
		dest, err := cleanOutputPath(p)
		if err != nil {
			discard(err.Error())
			return
		}
		if _, err := os.Lstat(dest); err != nil {
			discard(fmt.Sprintf("output path %s does not exist", p))
			return
		}
		dests = append(dests, dest)
	}

	blob, err := store.NewBlobStore(ctx, store.AgentManaged, s.bucketURL)
	if err != nil {
		discard(fmt.Sprintf("couldn't open the cache store: %v", err))
		return
	}

	for i, p := range plan.Outputs.Paths {
		stored, err := s.storePath(ctx, blob, p, dests[i])
		if err != nil {
			discard(fmt.Sprintf("couldn't store output path %s: %v", p, err))
			return
		}
		result.Paths = append(result.Paths, stored)
	}
}

func (s *reuseSaver) storePath(ctx context.Context, blob store.Blob, outputPath, dest string) (api.ReusePath, error) {
	digest, files, err := reuse.TreeDigest(dest)
	if err != nil {
		return api.ReusePath{}, err
	}
	key := reuseStoreKey(digest)

	size, err := blob.Stat(ctx, key)
	switch {
	case err == nil:
		if refresher, ok := blob.(store.RetentionRefresher); ok {
			refresher.RefreshRetention(ctx, key, reuseRetention)
		}
		s.l.Infof("Step reuse: output %s already stored (%d files, %s)", outputPath, files, digest)

	case errors.Is(err, store.ErrBlobNotFound):
		archive, err := os.CreateTemp("", "buildkite-step-reuse-*.tar.gz")
		if err != nil {
			return api.ReusePath{}, err
		}
		_ = archive.Close()
		defer func() { _ = os.Remove(archive.Name()) }()

		if err := reuse.Pack(dest, archive.Name()); err != nil {
			return api.ReusePath{}, err
		}
		fi, err := os.Stat(archive.Name())
		if err != nil {
			return api.ReusePath{}, err
		}
		if _, err := blob.Upload(ctx, archive.Name(), key, reuseRetention); err != nil {
			return api.ReusePath{}, err
		}
		size = fi.Size()
		s.l.Infof("Step reuse: output %s uploaded (%d files, %s)", outputPath, files, digest)

	default:
		return api.ReusePath{}, err
	}

	return api.ReusePath{Path: outputPath, Digest: digest, Key: key, Size: size, Files: files}, nil
}
