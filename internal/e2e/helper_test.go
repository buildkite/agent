//--go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/buildkite/agent/v3/version"

	"github.com/buildkite/go-buildkite/v4"
)

var (
	jobID         = os.Getenv("BUILDKITE_JOB_ID")
	apiToken      = os.Getenv("BUILDKITE_E2E_TESTS_API_TOKEN")
	targetOrg     = os.Getenv("BUILDKITE_E2E_TESTS_TARGET_ORG")
	targetCluster = os.Getenv("BUILDKITE_E2E_TESTS_TARGET_CLUSTER")
	authorEmail   = os.Getenv("BUILDKITE_BUILD_CREATOR_EMAIL")
	authorName    = os.Getenv("BUILDKITE_BUILD_CREATOR")
)

const pipelineRepo = "https://github.com/buildkite/agent.git"

type cleanupFn = func(context.Context) error

var nopCleanup = func(context.Context) error { return nil }

// SetupTestInfra creates a temporary queue and pipeline for running an
// end-to-end test, and takes care of removing it as test cleanup. It calls
// t.Fatal to exit early if there was a failure setting up the queue or
// pipeline.
func SetupTestInfra(t *testing.T, pipelineConfigTemplate string) (triggerBuild func(context.Context) error) {
	ctx := t.Context()

	tmpl, err := template.New("pipeline").Parse(pipelineConfigTemplate)
	if err != nil {
		t.Fatalf("template.New(pipeline).Parse(%q) error = %v", pipelineConfigTemplate, err)
	}

	th, err := newTestHelper()
	if err != nil {
		t.Fatalf("Could not create Buildkite API client: newTestHelper() error = %v", err)
	}

	name := nameForTest(t) // used for both pipeline and queue

	queueID, cleanup, err := th.createQueue(ctx, name)
	if err != nil {
		t.Fatalf("Could not create cluster queue in org %q cluster %q: testHelper.createQueue(ctx, %q) error = %v", targetOrg, targetCluster, name, err)
	}
	t.Cleanup(func() {
		if err := cleanup(ctx); err != nil {
			t.Logf("Could not clean up cluster queue %q with id %s in org %q cluster %q: cleanup(ctx) error = %v", name, queueID, targetOrg, targetCluster, err)
		}
	})

	var pipelineCfg strings.Builder
	tmplInput := map[string]string{"queue": name}
	if err := tmpl.Execute(&pipelineCfg, tmplInput); err != nil {
		t.Fatalf("Could not execute pipeline config template: tmpl.Execute(%q) error = %v", tmplInput, err)
	}

	pipeline, cleanup, err := th.createPipeline(ctx, name, pipelineCfg.String())
	if err != nil {
		t.Fatalf("Could not create pipeline with the following config in org %q: testHelper.createPipeline(%q, pipelineCfg) error = %v\n%s", targetOrg, name, err, pipelineCfg.String())
	}
	t.Cleanup(func() {
		if err := cleanup(ctx); err != nil {
			t.Logf("Could not clean up pipeline %q (id = %s) in org %q: %v", name, pipeline.ID, targetOrg, err)
		}
	})

	return func(ctx context.Context) error {

		return nil
	}
}

// nameForTest returns a string identifying (as uniquely as possible)
// the name and execution of a test function.
func nameForTest(t interface{ Name() string }) string {
	jobID := jobID // shadow the package var
	if jobID == "" {
		jobID = strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return strings.ToLower(t.Name() + "-" + jobID)
}

// testHelper implements key parts of the end-to-end testing framework.
type testHelper struct {
	client *buildkite.Client
}

// newTestHelper creates a new TestHelper.
func newTestHelper() (*testHelper, error) {
	client, err := buildkite.NewClient(
		buildkite.WithTokenAuth(apiToken),
		buildkite.WithUserAgent("buildkite-agent-e2e-tests/0 "+version.UserAgent()),
	)
	if err != nil {
		return nil, err
	}
	return &testHelper{client: client}, nil
}

// createQueue creates a cluster queue for running an end-to-end test in.
// The returned cleanup function deletes the queue and should be called after
// the test is finished.
func (th *testHelper) createQueue(ctx context.Context, name string) (id string, cleanup cleanupFn, err error) {
	cq, _, err := th.client.ClusterQueues.Create(ctx, targetOrg, targetCluster, buildkite.ClusterQueueCreate{
		Key:         name,
		Description: "Buildkite Agent E2E Test",
	})
	if err != nil {
		return "", nopCleanup, err
	}

	id = cq.ID
	cleanup = func(ctx context.Context) error {
		_, err := th.client.ClusterQueues.Delete(ctx, targetOrg, targetCluster, cq.ID)
		return err
	}
	return id, cleanup, nil
}

// createPipeline creates a pipeline for running an end-to-end test in.
// The returned cleanup function deletes the pipeline and should be called after
// the test is finished.
func (th *testHelper) createPipeline(ctx context.Context, name, config string) (*buildkite.Pipeline, cleanupFn, error) {
	p, _, err := th.client.Pipelines.Create(ctx, targetOrg, buildkite.CreatePipeline{
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
		return nil, nopCleanup, err
	}

	cleanup := func(ctx context.Context) error {
		_, err := th.client.Pipelines.Delete(ctx, targetOrg, p.Slug)
		return err
	}
	return &p, cleanup, nil
}

func (th *testHelper) createBuild(ctx context.Context, name string) (*buildkite.Build, cleanupFn, error) {
	authorName := authorName // shadow the package var
	if authorName == "" {
		authorName = "Agent E2E Tests"
	}

	build, _, err := th.client.Builds.Create(ctx, targetOrg, name,
		buildkite.CreateBuild{
			Author: buildkite.Author{
				Email: authorEmail,
				Name:  authorName,
			},
			Commit:  "HEAD",
			Branch:  "main",
			Message: name,
		},
	)
	if err != nil {
		return nil, nopCleanup, err
	}

	cleanup := func(ctx context.Context) error {
		_, err := th.client.Builds.Cancel(ctx, targetOrg, name, build.ID)
		return err
	}
	return &build, cleanup, nil
}
