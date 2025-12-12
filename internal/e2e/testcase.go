//go:build e2e

package e2e

import (
	"cmp"
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"text/template"
	"time"

	"github.com/buildkite/agent/v3/version"

	"github.com/buildkite/go-buildkite/v4"
	"github.com/buildkite/roko"
)

var (
	// Filled in from secrets
	apiToken   = os.Getenv("CI_E2E_TESTS_BUILDKITE_API_TOKEN")
	agentToken = os.Getenv("CI_E2E_TESTS_AGENT_TOKEN")

	// E2E testing config
	agentPath = os.Getenv("CI_E2E_TESTS_AGENT_PATH")
	printLogs = os.Getenv("CI_E2E_TESTS_PRINT_JOB_LOGS") == "true"

	// Obtained from agentToken in main_test.go
	targetOrg     string
	targetCluster string

	// Values from the Buildkite job running the tests
	jobID = cmp.Or(
		os.Getenv("BUILDKITE_JOB_ID"),
		strconv.FormatInt(time.Now().UnixNano(), 10),
	)
	authorEmail = os.Getenv("BUILDKITE_BUILD_CREATOR_EMAIL")
	authorName  = os.Getenv("BUILDKITE_BUILD_CREATOR")
)

const pipelineRepo = "https://github.com/buildkite/agent.git"

type cleanupFn = func() error

func nopCleanup() error { return nil }

//go:embed fixtures
var fixturesFS embed.FS

// testCase bundles the information needed to run an end-to-end test.
// Note that it embeds testing.TB - each test should create its own testCase.
type testCase struct {
	testing.TB

	fullName       string
	bkClient       *buildkite.Client
	pipelineConfig *template.Template
	queue          *buildkite.ClusterQueue
	pipeline       *buildkite.Pipeline
}

// newTestCase creates a new test case with a given pipeline config template,
// and sets up the temporary queue and pipeline to run it.
// It also registers cleanups with t.Cleanup so that the queue and pipeline
// are (usually) automatically deleted.
// It calls t.Fatal to end the test early if there was a failure setting up.
func newTestCase(t testing.TB, file string) *testCase {
	t.Helper()
	ctx := t.Context()

	name := strings.ToLower(t.Name() + "-" + jobID)

	pipelineCfgTmpl, err := fixturesFS.ReadFile(path.Join("fixtures", file))
	if err != nil {
		t.Fatalf("fixturesFS.ReadFile(%q) error = %v", file, err)
	}

	tmpl, err := template.New("pipeline").Parse(string(pipelineCfgTmpl))
	if err != nil {
		t.Fatalf("template.New(pipeline).Parse(%q) error = %v", pipelineCfgTmpl, err)
	}

	client, err := buildkite.NewClient(
		buildkite.WithTokenAuth(apiToken),
		buildkite.WithUserAgent("buildkite-agent-e2e-tests/0 "+version.UserAgent()),
	)
	if err != nil {
		t.Fatalf("buildkite.NewClient(...) error = %v", err)
	}

	queue, cleanupQueue, err := createQueue(ctx, client, name)
	if err != nil {
		t.Fatalf("Could not create cluster queue in org %q cluster %q: testHelper.createQueue(ctx, %q) error = %v", targetOrg, targetCluster, name, err)
	}
	t.Cleanup(func() {
		if err := cleanupQueue(); err != nil {
			t.Logf("Could not clean up cluster queue %q with id %s in org %q cluster %q: cleanup() error = %v", name, queue.ID, targetOrg, targetCluster, err)
		}
	})

	t.Logf("Created cluster queue %q in org %q", queue.Key, targetOrg)

	var pipelineCfg strings.Builder
	tmplInput := map[string]string{
		"queue":                  queue.Key,
		"buildkite_agent_binary": agentPath,
	}
	if err := tmpl.Execute(&pipelineCfg, tmplInput); err != nil {
		t.Fatalf("Could not execute pipeline config template: tmpl.Execute(%q) error = %v", tmplInput, err)
	}

	pipeline, cleanupPipeline, err := createPipeline(ctx, client, name, pipelineCfg.String())
	if err != nil {
		t.Fatalf("Could not create pipeline with the following config in org %q: testHelper.createPipeline(%q, pipelineCfg) error = %v\n%s", targetOrg, name, err, pipelineCfg.String())
	}
	t.Cleanup(func() {
		if err := cleanupPipeline(); err != nil {
			t.Logf("Could not clean up pipeline %q (id = %s) in org %q: cleanup() = %v", pipeline.Slug, pipeline.ID, targetOrg, err)
		}
	})

	t.Logf("Created pipeline %q in org %q", pipeline.Slug, targetOrg)

	return &testCase{
		TB:             t,
		fullName:       name,
		bkClient:       client,
		pipelineConfig: tmpl,
		queue:          queue,
		pipeline:       pipeline,
	}
}

// triggerBuild creates a new build in the target pipeline. It returns the
// build object. It also registers cleanups with t.Cleanup so that the build is
// (usually) automatically cancelled if it is still running.
// It calls t.Fatal if there was an error creating the build.
func (tc *testCase) triggerBuild() *buildkite.Build {
	tc.Helper()
	ctx := tc.Context()

	createBuild := buildkite.CreateBuild{
		Author: buildkite.Author{
			Email: authorEmail,
			Name:  cmp.Or(authorName, "Agent E2E Tests"),
		},
		Commit:  "HEAD",
		Branch:  "main",
		Message: tc.fullName,
	}

	build, _, err := tc.bkClient.Builds.Create(ctx, targetOrg, tc.pipeline.Slug, createBuild)
	if err != nil {
		tc.Fatalf("tc.bkClient.Builds.Create(ctx, %q, %q, %v) error = %v", targetOrg, tc.pipeline.Slug, createBuild, err)
	}

	tc.Logf("Triggered a build at https://buildkite.com/%s/%s/builds/%d", targetOrg, tc.pipeline.Slug, build.Number)

	tc.Cleanup(func() {
		ctx := context.WithoutCancel(ctx) // allow cleanup after the test
		_, err := tc.bkClient.Builds.Cancel(ctx, targetOrg, tc.pipeline.Slug, strconv.Itoa(build.Number))
		if err != nil {
			reasons := []string{
				"already finished",
				"already being canceled",
				"already been canceled",
				"No build found",
			}
			ignorable := slices.ContainsFunc(reasons, func(r string) bool {
				return strings.Contains(err.Error(), r)
			})
			if ignorable {
				return
			}
			tc.Logf("Couldn't cancel build %s: %v", build.ID, err)
		}
	})
	return &build
}

// waitForBuild waits until the build is in a terminal state
// (passed, failed, canceled, etc). It polls the build once per second.
// Note that the build pointed to by build is updated with the latest state
// after each poll. It calls t.Fatal if there was an error fetching the build
// or the context ends.
// If CI_E2E_TESTS_PRINT_JOB_LOGS=true, it fetches and prints the build logs.
func (tc *testCase) waitForBuild(ctx context.Context, build *buildkite.Build) string {
	tick := time.Tick(time.Second)
	for {
		// The arg is called "id" but it needs the build number...
		state, _, err := tc.bkClient.Builds.Get(ctx, targetOrg, tc.pipeline.Slug, strconv.Itoa(build.Number), &buildkite.BuildGetOptions{})
		if err != nil {
			tc.Fatalf("buildkite.Client.Builds.Get(ctx, %q, %q, %d, &{}) error = %v", targetOrg, tc.pipeline.Slug, build.Number, err)
			return ""
		}

		*build = state
		switch state.State {
		case "passed", "failed", "canceled", "canceling":
			if printLogs {
				logs := tc.fetchLogs(ctx, build)
				tc.Logf("Build logs:\n%s", logs)
			}
			return state.State

		case "scheduled", "running":
			select {
			case <-tick:
				// time to poll again
			case <-ctx.Done():
				tc.Fatalf("waitForBuild context ended: %v", ctx.Err())
				return ""
			}

		default:
			tc.Logf("waitForBuild read an unknown build state: %q", state.State)
			return state.State
		}
	}
}

// createQueue creates a cluster queue for running an end-to-end test in.
// The returned cleanup function deletes the queue and should be called after
// the test is finished.
func createQueue(ctx context.Context, client *buildkite.Client, name string) (*buildkite.ClusterQueue, cleanupFn, error) {
	cq, _, err := client.ClusterQueues.Create(ctx, targetOrg, targetCluster, buildkite.ClusterQueueCreate{
		Key:         name,
		Description: "Buildkite Agent E2E Test",
	})
	if err != nil {
		return nil, nopCleanup, err
	}

	cleanup := func() error {
		ctx := context.WithoutCancel(ctx) // allow cleanup after the test
		r := roko.NewRetrier(
			roko.WithStrategy(roko.Constant(5*time.Second)),
			// The agent could take a while to become lost after being killed,
			// so retry for a long time.
			roko.WithMaxAttempts(65),
		)
		return r.Do(func(*roko.Retrier) error {
			_, err := client.ClusterQueues.Delete(ctx, targetOrg, targetCluster, cq.ID)
			return err
		})
	}
	return &cq, cleanup, nil
}

// createPipeline creates a pipeline for running an end-to-end test in.
// The returned cleanup function deletes the pipeline and should be called after
// the test is finished.
func createPipeline(ctx context.Context, client *buildkite.Client, name, config string) (*buildkite.Pipeline, cleanupFn, error) {
	p, _, err := client.Pipelines.Create(ctx, targetOrg, buildkite.CreatePipeline{
		Name:        name,
		Repository:  pipelineRepo,
		Description: "Buildkite Agent E2E Test",
		ProviderSettings: &buildkite.GitHubSettings{
			TriggerMode: "none",
		},
		Configuration: config,
		ClusterID:     targetCluster,
	})
	if err != nil {
		var errResp *buildkite.ErrorResponse
		if errors.As(err, &errResp) && len(errResp.RawBody) > 0 {
			return nil, nopCleanup, fmt.Errorf("%w: %s", err, errResp.RawBody)
		}
		return nil, nopCleanup, err
	}

	cleanup := func() error {
		ctx := context.WithoutCancel(ctx) // allow cleanup after the test
		r := roko.NewRetrier(
			roko.WithStrategy(roko.Constant(5*time.Second)),
			roko.WithMaxAttempts(5),
		)
		return r.Do(func(*roko.Retrier) error {
			_, err := client.Pipelines.Delete(ctx, targetOrg, p.Slug)
			return err
		})
	}
	return &p, cleanup, nil
}

// startAgent starts a copy of the agent (at agentPath, using agentToken).
// The agent should be automatically cleaned up at the end of the test.
func (tc *testCase) startAgent(extraArgs ...string) *exec.Cmd {
	tc.Helper()
	dir := tc.TempDir()
	buildPath := filepath.Join(dir, "builds")
	hooksPath := filepath.Join(dir, "hooks")
	pluginsPath := filepath.Join(dir, "plugins")
	for _, path := range []string{buildPath, hooksPath, pluginsPath} {
		if err := os.Mkdir(path, 0o700); err != nil {
			tc.Fatalf("Couldn't create dir inside temporary agent dir: os.Mkdir(%q, %o) = %v", path, 0o700, err)
		}
	}

	// Unix domain sockets have a path length limit (~104 chars), so use a short
	// path in /tmp instead of the potentially long tc.TempDir() path.
	socketsPath, err := os.MkdirTemp("/tmp", "bk")
	if err != nil {
		tc.Fatalf("Couldn't create sockets dir: os.MkdirTemp(/tmp, bk) = %v", err)
	}
	tc.Cleanup(func() {
		os.RemoveAll(socketsPath)
	})

	args := append([]string{
		"start",
		"--debug",
		"--token", agentToken,
		"--name", tc.fullName,
		"--queue", tc.queue.Key,
		"--build-path", buildPath,
		"--hooks-path", hooksPath,
		"--sockets-path", socketsPath,
		"--plugins-path", pluginsPath,
	}, extraArgs...)
	tc.Logf("Starting agent with args: %q", args)

	cmd := exec.CommandContext(tc.Context(), agentPath, args...)
	// Ensure minimal environment variable shenanigans by setting only these:
	cmd.Env = []string{
		"HOME=" + os.Getenv("HOME"),
		"PATH=" + os.Getenv("PATH"),
	}
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	tc.Cleanup(func() {
		if err := cmd.Wait(); err != nil {
			tc.Logf("Couldn't wait for agent to exit: cmd.Wait() = %v", err)
		}
		tc.Log("Agent output:")
		tc.Log(buf.String())
	})

	// The agent should be cancelled automatically by t.Context.
	// The default Cancel func set by CommandContext is `cmd.Process.Kill()`,
	// so the agent would exit immediately and disconnect uncleanly.
	// It is eventually marked lost on the backend, but not for a few minutes,
	// which blocks queue cleanup for a while.
	// This replacement Cancel func SIGQUITs it, which forces an ungraceful
	// but clean exit (jobs are cancelled but it has time to disconnect).
	// To ensure the agent _is_ eventually SIGKILLed, WaitDelay is set.
	cmd.Cancel = func() error {
		return cmd.Process.Signal(syscall.SIGQUIT)
	}
	cmd.WaitDelay = 10 * time.Second

	if err := cmd.Start(); err != nil {
		tc.Fatalf("Couldn't start agent command %v: %v", cmd, err)
	}
	return cmd
}

// fetchLogs fetches the logs for all jobs in a build, as a single string.
// It calls t.Fatal to end the test if the logs of any job' cannot be fetched
// within a few retries.
func (tc *testCase) fetchLogs(ctx context.Context, build *buildkite.Build) string {
	tc.Helper()

	r := roko.NewRetrier(
		roko.WithStrategy(roko.Constant(5*time.Second)),
		roko.WithMaxAttempts(5),
	)
	logs, err := roko.DoFunc(ctx, r, func(*roko.Retrier) (string, error) {
		var logs strings.Builder
		for _, job := range build.Jobs {
			jobLog, _, err := tc.bkClient.Jobs.GetJobLog(
				ctx,
				targetOrg,
				tc.pipeline.Slug,
				strconv.Itoa(build.Number),
				job.ID,
			)
			if err != nil {
				return "", err
			}
			if jobLog.Content == "" {
				return "", fmt.Errorf("job %q log empty", job.ID)
			}

			logs.WriteString(jobLog.Content)
		}
		return logs.String(), nil
	})
	if err != nil {
		tc.Fatalf("fetchLogs failed to fetch logs: %v", err)
	}
	return logs
}
