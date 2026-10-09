package clicommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/buildkite/agent/v4/agent"
	"github.com/buildkite/agent/v4/api"
	"github.com/buildkite/agent/v4/internal/agenthttp"
	"github.com/buildkite/agent/v4/internal/artifact"
	"github.com/buildkite/agent/v4/jobapi"
	"github.com/buildkite/agent/v4/logger"
	gha "github.com/buildkite/buildkite-gha"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// This identity binds the opaque plans to the compiler/runtime module, not the
// agent release. The executable digest additionally binds them to this binary.
const ghaVersion = "agent-0099d1ba090e"

type ghaBackend struct {
	client     *api.Client
	log        logger.Logger
	job, build string
	replace    bool
}

var (
	_ gha.Backend     = (*ghaBackend)(nil)
	_ gha.Credentials = (*ghaBackend)(nil)
)

func newGHAClient(cfg PipelineUploadConfig, l logger.Logger, stdout, stderr io.Writer) (gha.Client, error) {
	// The pinned compiler also reads these from the environment. Do not allow
	// preparation and publication to use different job identities.
	if cfg.Job == "" || cfg.AgentAccessToken == "" || os.Getenv("BUILDKITE_AGENT_ENDPOINT") == "" {
		return gha.Client{}, errors.New("GitHub Actions import requires a Buildkite job environment (job ID, access token and endpoint)")
	}
	if cfg.Job != os.Getenv("BUILDKITE_JOB_ID") || cfg.AgentAccessToken != os.Getenv("BUILDKITE_AGENT_ACCESS_TOKEN") ||
		strings.TrimRight(cfg.Endpoint, "/") != strings.TrimRight(os.Getenv("BUILDKITE_AGENT_ENDPOINT"), "/") {
		return gha.Client{}, errors.New("GitHub Actions commands do not support job, token or endpoint overrides that differ from the job environment")
	}
	if cfg.JWKSFile != "" || cfg.SigningAWSKMSKey != "" || cfg.SigningGCPKMSKey != "" ||
		os.Getenv("BUILDKITE_AGENT_JWKS_FILE") != "" || os.Getenv("BUILDKITE_AGENT_AWS_KMS_KEY") != "" || os.Getenv("BUILDKITE_AGENT_GCP_KMS_KEY") != "" {
		return gha.Client{}, errors.New("GitHub Actions pipeline signing is not yet supported; refusing to publish unsigned steps")
	}
	client := api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken"))
	b := &ghaBackend{client: client, log: l, job: cfg.Job, build: os.Getenv("BUILDKITE_BUILD_ID"), replace: cfg.Replace}
	transport := agenthttp.NewClient(agenthttp.WithAuthToken(cfg.AgentAccessToken), agenthttp.WithAllowHTTP2(!cfg.NoHTTP2)).Transport
	return gha.Client{
		Version: ghaVersion, Backend: b, Credentials: b,
		AgentAPI: ghaTransport{transport, client.ServerSpecifiedRequestHeaders()},
		Stdout:   stdout, Stderr: stderr,
	}, nil
}

type ghaTransport struct {
	http.RoundTripper
	headers http.Header
}

func (t ghaTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for key, values := range t.headers {
		r.Header[key] = slices.Clone(values)
	}
	return t.RoundTripper.RoundTrip(r)
}

func (b *ghaBackend) uploadArtifacts(ctx context.Context, root, path string, literal bool) error {
	return artifact.NewUploader(b.log, b.client, artifact.UploaderConfig{
		JobID: b.job, WorkingDirectory: root, Paths: path, Literal: literal,
		AllowMultipart: true, DisableHTTP2: b.client.Config().DisableHTTP2,
	}).Upload(ctx)
}

func (b *ghaBackend) UploadArtifacts(ctx context.Context, root, pattern string) error {
	return b.uploadArtifacts(ctx, root, pattern, false)
}

func (b *ghaBackend) UploadArtifactFrom(ctx context.Context, root, path string) error {
	return b.uploadArtifacts(ctx, root, path, true)
}

func (b *ghaBackend) DownloadArtifact(ctx context.Context, path, destination, producer string) error {
	// Avoid the CLI downloader's intentional merging of matching destination
	// and artifact path components: this boundary promises the full path.
	root, err := os.MkdirTemp(destination, "gha-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root) //nolint:errcheck // Temporary download directory.
	d := artifact.NewDownloader(b.log, b.client, artifact.DownloaderConfig{
		BuildID: b.build, Query: path, Step: producer, Destination: root,
		AllowS3Multipart: true, DisableHTTP2: b.client.Config().DisableHTTP2,
	})
	if err := d.Download(ctx); err != nil {
		return err
	}
	target := filepath.Join(destination, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.Rename(filepath.Join(root, filepath.FromSlash(path)), target)
}

func (b *ghaBackend) SearchArtifactProducer(ctx context.Context, path, step string) (string, error) {
	artifacts, err := artifact.NewSearcher(b.log, b.client, b.build).Search(ctx, path, step, false, true)
	if err != nil {
		return "", err
	}
	jobs := make(map[string]bool)
	for _, a := range artifacts {
		if a.Path == path && a.JobID != "" {
			jobs[a.JobID] = true
		}
	}
	if len(jobs) != 1 {
		return "", fmt.Errorf("expected one artifact producer for %q under %q, found %d", path, step, len(jobs))
	}
	for job := range jobs {
		return job, nil
	}
	panic("unreachable")
}

func (b *ghaBackend) UploadPipeline(ctx context.Context, data []byte) error {
	var pipeline map[string]any
	if err := yaml.Unmarshal(data, &pipeline); err != nil {
		return err
	}
	u := agent.PipelineUploader{
		Client: b.client, JobID: b.job, RetrySleepFunc: time.Sleep,
		Change: &api.PipelineChange{UUID: api.NewUUID(), Pipeline: pipeline, Replace: b.replace},
	}
	return u.Upload(ctx, b.log)
}

func (b *ghaBackend) SetMetadata(ctx context.Context, key, value string) error {
	_, err := b.client.SetMetaData(ctx, b.job, &api.MetaData{Key: key, Value: value})
	return err
}

func (b *ghaBackend) GetMetadataBounded(ctx context.Context, key string, limit int) ([]byte, error) {
	m, response, err := b.client.GetMetaData(ctx, "job", b.job, key)
	if err != nil {
		if key == "buildkite:webhook" && response != nil &&
			(response.StatusCode == 400 && strings.Contains(err.Error(), "Build was not triggered by a webhook") ||
				response.StatusCode == 404 && strings.Contains(err.Error(), "Build webhook is not available")) {
			return nil, gha.ErrMetadataUnavailable
		}
		return nil, err
	}
	if len(m.Value) > limit {
		return nil, fmt.Errorf("metadata output exceeds %d bytes", limit)
	}
	return []byte(m.Value), nil
}

func (b *ghaBackend) GetStepAttribute(ctx context.Context, step, attribute string) ([]byte, error) {
	r, _, err := b.client.StepExport(ctx, step, &api.StepExportRequest{Build: b.build, Attribute: attribute})
	if err != nil {
		return nil, err
	}
	return []byte(r.Output), nil
}

func (b *ghaBackend) EnsureStepLabelSuffix(ctx context.Context, suffix string) error {
	step := os.Getenv("BUILDKITE_STEP_ID")
	if step == "" {
		return errors.New("step label update requires BUILDKITE_STEP_ID")
	}
	r, _, err := b.client.StepExport(ctx, step, &api.StepExportRequest{Build: b.build, Attribute: "label"})
	if err != nil {
		return err
	}
	if strings.HasSuffix(strings.TrimSpace(r.Output), strings.TrimSpace(suffix)) {
		return nil
	}
	_, err = b.client.StepUpdate(ctx, step, &api.StepUpdate{
		Build: b.build, Attribute: "label", Value: suffix, Append: true, IdempotencyUUID: api.NewUUID(),
	})
	return err
}

func (b *ghaBackend) AnnotateJob(ctx context.Context, job, annotationContext, style, body string) error {
	_, err := b.client.Annotate(ctx, job, &api.Annotation{Context: annotationContext, Style: style, Body: body, Scope: "job"})
	return err
}

func (b *ghaBackend) ResolveSecret(ctx context.Context, key string) (string, error) {
	// Secret bodies must never enter HTTP debug logs, even if redaction fails.
	config := b.client.Config()
	config.DebugHTTP = false
	s, _, err := b.client.New(config).GetSecret(ctx, &api.GetSecretRequest{Key: key, JobID: b.job})
	if err != nil {
		return "", err
	}
	if err := b.AddRedaction(ctx, s.Value); err != nil {
		return "", err
	}
	return s.Value, nil
}

func (b *ghaBackend) AddRedaction(ctx context.Context, value string) error {
	c, err := jobapi.NewDefaultClient(ctx)
	if err != nil {
		return err
	}
	return AddToRedactor(ctx, b.log, c, value)
}

func (b *ghaBackend) GitCredentialHelper() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return "!" + shellQuote(executable) + " git-credentials-helper", nil
}

func uploadGitHubActions(ctx context.Context, c *cli.Command, cfg PipelineUploadConfig, l logger.Logger) (err error) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer func() {
		if err != nil && strings.Contains(err.Error(), "runtime distribution for") {
			err = fmt.Errorf("this GitHub Actions POC supplies only the importing agent's %s/%s executable; use runner labels and queues matching that platform: %w", runtime.GOOS, runtime.GOARCH, err)
		}
	}()
	client, err := newGHAClient(cfg, l, c.Root().Writer, c.Root().ErrWriter)
	if err != nil {
		return err
	}
	request := gha.CompileRequest{
		WorkflowPaths: cfg.FilePaths, EventPath: cfg.GHAEventPath,
		DisableRunnerUser: cfg.GHADisableRunnerUser, Runners: make(map[string]gha.Runner),
	}
	for _, mapping := range cfg.GHARunnerQueues {
		label, queue, ok := strings.Cut(mapping, "=")
		if !ok || label == "" || queue == "" {
			return fmt.Errorf("invalid gha-runner-queue %q: expected label=queue", mapping)
		}
		if _, exists := request.Runners[label]; exists {
			return fmt.Errorf("duplicate gha-runner-queue %q", label)
		}
		request.Runners[label] = gha.Runner{Queue: queue}
	}
	if !cfg.DryRun {
		return client.Upload(ctx, request)
	}
	// Compile diagnostics go to stderr so stdout contains only pipeline YAML.
	client.Stdout = c.Root().ErrWriter
	compiled, err := client.Compile(ctx, request)
	if err != nil {
		return err
	}
	_, err = c.Root().Writer.Write(compiled.Pipeline)
	return err
}

type ghaRunConfig struct {
	GlobalConfig
	APIConfig
	Job              string `cli:"job"`
	PlanPath         string `cli:"plan"`
	PlanDigest       string `cli:"plan-digest"`
	PlanProducer     string `cli:"plan-producer"`
	ArtifactProducer string `cli:"artifact-producer"`
	ResultPath       string `cli:"result"`
	HostedToolCache  bool   `cli:"hosted-tool-cache"`
	DockerBuildLoad  bool   `cli:"docker-build-load"`
}

type ghaStageConfig struct {
	GlobalConfig
	APIConfig
	Job           string `cli:"job"`
	StageDigest   string `cli:"stage-digest"`
	StageProducer string `cli:"stage-producer"`
}

var GHACommand = &cli.Command{
	Name: "gha", Category: categoryJobCommands, Usage: "Execute imported GitHub Actions jobs",
	Commands: []*cli.Command{
		{
			Name: "run-job", Usage: "Run a verified GitHub Actions job plan",
			Description: "Usage:\n\n    buildkite-agent gha run-job --plan <path> --artifact-producer <job>\n\nExecute a verified imported job plan, preserving its exit status (including 78).",
			Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
				&cli.StringFlag{Name: "job", Sources: cli.EnvVars("BUILDKITE_JOB_ID")},
				&cli.StringFlag{Name: "plan"}, &cli.StringFlag{Name: "plan-digest"},
				&cli.StringFlag{Name: "plan-producer"}, &cli.StringFlag{Name: "artifact-producer"},
				&cli.StringFlag{Name: "result"}, &cli.BoolFlag{Name: "hosted-tool-cache"},
				&cli.BoolFlag{Name: "docker-build-load"},
			}),
			Action: func(ctx context.Context, c *cli.Command) error {
				ctx, cfg, l, _, done := setupLoggerAndConfig[ghaRunConfig](ctx, c)
				defer done()
				client, err := newGHAClient(PipelineUploadConfig{GlobalConfig: cfg.GlobalConfig, APIConfig: cfg.APIConfig, Job: cfg.Job}, l, c.Root().Writer, c.Root().ErrWriter)
				if err != nil {
					return err
				}
				code, _ := client.RunJob(ctx, gha.RunJobRequest{
					PlanPath: cfg.PlanPath, PlanDigest: cfg.PlanDigest, PlanProducer: cfg.PlanProducer,
					ArtifactProducer: cfg.ArtifactProducer, ResultPath: cfg.ResultPath,
					HostedToolCache: cfg.HostedToolCache, DockerBuildLoad: cfg.DockerBuildLoad,
				})
				if code != 0 {
					return NewSilentExitError(code)
				}
				return nil
			},
		},
		{
			Name: "upload", Hidden: true,
			Description: "Usage:\n\n    buildkite-agent gha upload --stage-digest <digest> --stage-producer <job>\n\nUpload a verified deferred workflow stage.",
			Flags: slices.Concat(globalFlags(), apiFlags(), []cli.Flag{
				&cli.StringFlag{Name: "job", Sources: cli.EnvVars("BUILDKITE_JOB_ID")},
				&cli.StringFlag{Name: "stage-digest", Required: true},
				&cli.StringFlag{Name: "stage-producer", Required: true},
			}),
			Action: func(ctx context.Context, c *cli.Command) error {
				ctx, cfg, l, _, done := setupLoggerAndConfig[ghaStageConfig](ctx, c)
				defer done()
				client, err := newGHAClient(PipelineUploadConfig{GlobalConfig: cfg.GlobalConfig, APIConfig: cfg.APIConfig, Job: cfg.Job}, l, c.Root().Writer, c.Root().ErrWriter)
				if err != nil {
					return err
				}
				return client.UploadStage(ctx, gha.StageRequest{Digest: cfg.StageDigest, Producer: cfg.StageProducer})
			},
		},
	},
}

// GHAProtocolArgs normalizes the compiler's reserved top-level argv forms to
// native typed handlers. Ordinary agent commands, help and version are untouched.
func GHAProtocolArgs(args []string) []string {
	if len(args) < 2 {
		return args
	}
	if args[1] == "run-job" || args[1] == "upload" &&
		slices.ContainsFunc(args[2:], func(s string) bool { return s == "--stage-digest" || strings.HasPrefix(s, "--stage-digest=") }) {
		return append([]string{args[0], "gha"}, args[1:]...)
	}
	return args
}

// RunGHAPrivateHelper is the only RunCLI path: the pinned API cannot inject
// native services into RunCLI, so upload and run-job must use typed methods.
func RunGHAPrivateHelper(args []string) (int, bool) {
	if len(args) < 2 || args[1] != "__container-process" {
		return 0, false
	}
	return gha.RunCLI(args[1:], os.Stdout, os.Stderr, ghaVersion), true
}
