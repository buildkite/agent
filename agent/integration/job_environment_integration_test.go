package integration

import (
	"slices"
	"strings"
	"testing"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/bintest/v3"
	"github.com/buildkite/go-pipeline"
)

func TestWhenCachePathsSetInJobStep_CachePathsEnvVarIsSet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	job := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Step: pipeline.CommandStep{
			Cache: &pipeline.Cache{
				Paths: []string{"foo", "bar"},
			},
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_AGENT_CACHE_PATHS"), "foo,bar"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_AGENT_CACHE_PATHS) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}

func TestCacheSettingsOnSelfHosted_LogsMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	jobID := "cache-self-hosted-job"
	job := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMPUTE_TYPE": "self-hosted",
		},
		Step: pipeline.CommandStep{
			Cache: &pipeline.Cache{
				Paths: []string{"vendor", "node_modules"},
			},
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
	mb.Expect().Once().AndExitWith(0)

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

	logs := e.logsFor(t, jobID)
	if !strings.Contains(logs, "Cache settings detected on self-hosted agent") {
		t.Errorf("expected logs to contain cache warning for self-hosted agent, got %q", logs)
	}
	if !strings.Contains(logs, "vendor, node_modules") {
		t.Errorf("expected logs to contain cache paths, got %q", logs)
	}
}

func TestCacheSettingsOnHosted_DoesNotLogMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	jobID := "cache-hosted-job"
	job := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMPUTE_TYPE": "hosted",
		},
		Step: pipeline.CommandStep{
			Cache: &pipeline.Cache{
				Paths: []string{"vendor", "node_modules"},
			},
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
	mb.Expect().Once().AndExitWith(0)

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

	logs := e.logsFor(t, jobID)
	if strings.Contains(logs, "Cache settings detected on self-hosted agent") {
		t.Errorf("expected logs to NOT contain cache warning for hosted agent, got %q", logs)
	}
}

func TestNoCacheSettings_DoesNotLogMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	jobID := "no-cache-job"
	job := &api.Job{
		ID:                 jobID,
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMPUTE_TYPE": "self-hosted",
		},
		Step:  pipeline.CommandStep{},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t
	mb.Expect().Once().AndExitWith(0)

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}

	logs := e.logsFor(t, jobID)
	if strings.Contains(logs, "Cache settings detected on self-hosted agent") {
		t.Errorf("expected logs to NOT contain cache warning when no cache settings, got %q", logs)
	}
}

func TestBuildkiteRequestHeaders(t *testing.T) {
	t.Parallel()

	// create a mock agent API
	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	// create a client with server-specified headers
	l := logger.NewConsoleLogger(logger.NewTestPrinter(t), func(int) {})
	client := api.NewClient(l, api.Config{
		Endpoint:  server.URL,
		Token:     "llamasrock",
		DebugHTTP: true,
	})
	headers := client.ServerSpecifiedRequestHeaders()
	// That getter isn't designed to modify the headers, but all's fair in test setup code and war.
	headers.Set("Buildkite-Hello", "world")

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	// The main assertion: that the `Buildkite-Hello: world` server-specified request header is
	// passed to the job environment as BUILDKITE_REQUEST_HEADER_BUILDKITE_HELLO=world. From there,
	// it'll be picked up by api.NewClient() in sub-processes like `buildkite-agent annotate` etc.
	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_REQUEST_HEADER_BUILDKITE_HELLO"), "world"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_REQUEST_HEADER_BUILDKITE_HELLO) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	err := runJob(t, t.Context(), testRunJobConfig{
		job: &api.Job{
			ID:                 "00000000-0000-0000-0000-000000000123",
			ChunksMaxSizeBytes: 1024,
			Step:               pipeline.CommandStep{},
			Token:              "bkaj_job-token",
		},
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
		client:        client,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}

func TestCheckoutScopedJobEnvOverrideHonorsCheckoutOverrideMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		varName            string
		jobEnv             map[string]string
		agentCfg           agent.AgentConfiguration
		wantEnvValue       string
		wantIgnoredEnvVars []string
	}{
		{
			name:    "none_allows_job_env_to_override_clone_flags",
			varName: "BUILDKITE_GIT_CLONE_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CLONE_FLAGS": "--no-tags",
			},
			agentCfg: agent.AgentConfiguration{
				GitCloneFlags:        "--mirror",
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "--no-tags",
		},
		{
			name:    "strict_locks_clone_flags_to_agent_config",
			varName: "BUILDKITE_GIT_CLONE_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CLONE_FLAGS": "--no-tags",
			},
			agentCfg: agent.AgentConfiguration{
				GitCloneFlags:        "--mirror",
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "--mirror",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_CLONE_FLAGS"},
		},
		{
			name:    "none_allows_job_env_to_enable_submodules",
			varName: "BUILDKITE_GIT_SUBMODULES",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SUBMODULES": "true",
			},
			agentCfg: agent.AgentConfiguration{
				GitSubmodules:        false,
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "true",
		},
		{
			name:    "strict_locks_submodules_to_agent_config",
			varName: "BUILDKITE_GIT_SUBMODULES",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SUBMODULES": "true",
			},
			agentCfg: agent.AgentConfiguration{
				GitSubmodules:        false,
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "false",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_SUBMODULES"},
		},
		{
			name:    "none_allows_job_env_to_override_skip_checkout",
			varName: "BUILDKITE_SKIP_CHECKOUT",
			jobEnv: map[string]string{
				"BUILDKITE_SKIP_CHECKOUT": "false",
			},
			agentCfg: agent.AgentConfiguration{
				SkipCheckout:         true,
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "false",
		},
		{
			name:    "strict_locks_skip_checkout_to_agent_config",
			varName: "BUILDKITE_SKIP_CHECKOUT",
			jobEnv: map[string]string{
				"BUILDKITE_SKIP_CHECKOUT": "false",
			},
			agentCfg: agent.AgentConfiguration{
				SkipCheckout:         true,
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "true",
			wantIgnoredEnvVars: []string{"BUILDKITE_SKIP_CHECKOUT"},
		},
		{
			name:    "none_allows_job_env_to_override_sparse_checkout_paths",
			varName: "BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS": "job/path",
			},
			agentCfg: agent.AgentConfiguration{
				GitSparseCheckoutPaths: []string{"agent/path"},
				CheckoutOverrideMode:   env.CheckoutOverrideNone,
			},
			wantEnvValue: "job/path",
		},
		{
			name:    "strict_locks_sparse_checkout_paths_to_agent_config",
			varName: "BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS": "job/path",
			},
			agentCfg: agent.AgentConfiguration{
				GitSparseCheckoutPaths: []string{"agent/path"},
				CheckoutOverrideMode:   env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "agent/path",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS"},
		},
		// Empty agent config: sparse paths are exported unconditionally (unlike the
		// main-era len()>0 guard), so cover the no-agent-paths edge. Unlocked must
		// still defer to job env; locked must ignore it. The lock-on case exports an
		// empty string where main left the var unset, which the checkout consumer
		// treats identically (cleanGitSparseCheckoutPaths drops it).
		{
			name:    "none_preserves_job_env_sparse_paths_with_empty_agent_config",
			varName: "BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS": "job/path",
			},
			agentCfg: agent.AgentConfiguration{
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "job/path",
		},
		{
			name:    "strict_locks_sparse_paths_with_empty_agent_config",
			varName: "BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS": "job/path",
			},
			agentCfg: agent.AgentConfiguration{
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_SPARSE_CHECKOUT_PATHS"},
		},
		// Inverse cases: when the agent config sits on the side that emits no var
		// by default, the lock must still force the agent value (regression for the
		// leak where backend job env survived while checkout override was locked).
		{
			name:    "strict_locks_submodules_on_to_agent_config",
			varName: "BUILDKITE_GIT_SUBMODULES",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SUBMODULES": "false",
			},
			agentCfg: agent.AgentConfiguration{
				GitSubmodules:        true,
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "true",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_SUBMODULES"},
		},
		{
			name:    "none_allows_job_env_to_disable_submodules",
			varName: "BUILDKITE_GIT_SUBMODULES",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SUBMODULES": "false",
			},
			agentCfg: agent.AgentConfiguration{
				GitSubmodules:        true,
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "false",
		},
		{
			name:    "strict_locks_skip_checkout_off_to_agent_config",
			varName: "BUILDKITE_SKIP_CHECKOUT",
			jobEnv: map[string]string{
				"BUILDKITE_SKIP_CHECKOUT": "true",
			},
			agentCfg: agent.AgentConfiguration{
				SkipCheckout:         false,
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "false",
			wantIgnoredEnvVars: []string{"BUILDKITE_SKIP_CHECKOUT"},
		},
		{
			name:    "strict_locks_skip_fetch_existing_commits_to_agent_config",
			varName: "BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS": "true",
			},
			agentCfg: agent.AgentConfiguration{
				GitSkipFetchExistingCommits: false,
				CheckoutOverrideMode:        env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "false",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_SKIP_FETCH_EXISTING_COMMITS"},
		},
		// The remaining checkout-scoped vars all flow through setCheckoutEnv; cover
		// each in both directions so a regression in any one is caught. The git flag
		// vars are the injection vectors the lock exists to contain.
		{
			name:    "none_allows_job_env_to_override_checkout_flags",
			varName: "BUILDKITE_GIT_CHECKOUT_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CHECKOUT_FLAGS": "--quiet",
			},
			agentCfg: agent.AgentConfiguration{
				GitCheckoutFlags:     "-f",
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "--quiet",
		},
		{
			name:    "strict_locks_checkout_flags_to_agent_config",
			varName: "BUILDKITE_GIT_CHECKOUT_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CHECKOUT_FLAGS": "--quiet",
			},
			agentCfg: agent.AgentConfiguration{
				GitCheckoutFlags:     "-f",
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "-f",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_CHECKOUT_FLAGS"},
		},
		{
			name:    "none_allows_job_env_to_override_fetch_flags",
			varName: "BUILDKITE_GIT_FETCH_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_FETCH_FLAGS": "--prune",
			},
			agentCfg: agent.AgentConfiguration{
				GitFetchFlags:        "-v",
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "--prune",
		},
		{
			name:    "strict_locks_fetch_flags_to_agent_config",
			varName: "BUILDKITE_GIT_FETCH_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_FETCH_FLAGS": "--prune",
			},
			agentCfg: agent.AgentConfiguration{
				GitFetchFlags:        "-v",
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "-v",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_FETCH_FLAGS"},
		},
		{
			name:    "none_allows_job_env_to_override_clean_flags",
			varName: "BUILDKITE_GIT_CLEAN_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CLEAN_FLAGS": "-fdq",
			},
			agentCfg: agent.AgentConfiguration{
				GitCleanFlags:        "-ffxdq",
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "-fdq",
		},
		{
			name:    "strict_locks_clean_flags_to_agent_config",
			varName: "BUILDKITE_GIT_CLEAN_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CLEAN_FLAGS": "-fdq",
			},
			agentCfg: agent.AgentConfiguration{
				GitCleanFlags:        "-ffxdq",
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "-ffxdq",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_CLEAN_FLAGS"},
		},
		{
			name:    "none_allows_job_env_to_override_clone_mirror_flags",
			varName: "BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CLONE_MIRROR_FLAGS": "--mirror",
			},
			agentCfg: agent.AgentConfiguration{
				GitCloneMirrorFlags:  "--bare",
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "--mirror",
		},
		{
			name:    "strict_locks_clone_mirror_flags_to_agent_config",
			varName: "BUILDKITE_GIT_CLONE_MIRROR_FLAGS",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CLONE_MIRROR_FLAGS": "--mirror",
			},
			agentCfg: agent.AgentConfiguration{
				GitCloneMirrorFlags:  "--bare",
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "--bare",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_CLONE_MIRROR_FLAGS"},
		},
		{
			name:    "none_allows_job_env_to_override_mirrors_skip_update",
			varName: "BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE": "true",
			},
			agentCfg: agent.AgentConfiguration{
				GitMirrorsSkipUpdate: false,
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "true",
		},
		{
			name:    "strict_locks_mirrors_skip_update_to_agent_config",
			varName: "BUILDKITE_GIT_MIRRORS_SKIP_UPDATE",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE": "true",
			},
			agentCfg: agent.AgentConfiguration{
				GitMirrorsSkipUpdate: false,
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "false",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_MIRRORS_SKIP_UPDATE"},
		},
		{
			name:    "none_allows_job_env_to_override_checkout_timeout",
			varName: "BUILDKITE_GIT_CHECKOUT_TIMEOUT",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CHECKOUT_TIMEOUT": "99",
			},
			agentCfg: agent.AgentConfiguration{
				GitCheckoutTimeout:   60,
				CheckoutOverrideMode: env.CheckoutOverrideNone,
			},
			wantEnvValue: "99",
		},
		{
			name:    "strict_locks_checkout_timeout_to_agent_config",
			varName: "BUILDKITE_GIT_CHECKOUT_TIMEOUT",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CHECKOUT_TIMEOUT": "99",
			},
			agentCfg: agent.AgentConfiguration{
				GitCheckoutTimeout:   60,
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "60",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_CHECKOUT_TIMEOUT"},
		},
		{
			// Agent timeout at its default 0 is the silent side: the lock must
			// still emit it so a job can't reintroduce a checkout timeout.
			name:    "strict_locks_checkout_timeout_off_to_agent_config",
			varName: "BUILDKITE_GIT_CHECKOUT_TIMEOUT",
			jobEnv: map[string]string{
				"BUILDKITE_GIT_CHECKOUT_TIMEOUT": "99",
			},
			agentCfg: agent.AgentConfiguration{
				CheckoutOverrideMode: env.CheckoutOverrideStrict,
			},
			wantEnvValue:       "0",
			wantIgnoredEnvVars: []string{"BUILDKITE_GIT_CHECKOUT_TIMEOUT"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			jobEnv := map[string]string{
				"BUILDKITE_COMMAND": "echo hello world",
			}
			for k, v := range tc.jobEnv {
				jobEnv[k] = v
			}

			job := &api.Job{
				ID:                 "my-job-id",
				ChunksMaxSizeBytes: 1024,
				Env:                jobEnv,
				Token:              "bkaj_job-token",
			}

			mb := mockBootstrap(t)
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

			mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
				if got, want := c.GetEnv(tc.varName), tc.wantEnvValue; got != want {
					t.Errorf("c.GetEnv(%s) = %q, want %q", tc.varName, got, want)
					c.Exit(1)
					return
				}

				ignored := strings.Split(strings.TrimSpace(c.GetEnv("BUILDKITE_IGNORED_ENV")), ",")
				for _, wantIgnored := range tc.wantIgnoredEnvVars {
					if !slices.Contains(ignored, wantIgnored) {
						t.Errorf("BUILDKITE_IGNORED_ENV = %q, want it to contain %q", c.GetEnv("BUILDKITE_IGNORED_ENV"), wantIgnored)
						c.Exit(1)
						return
					}
				}
				if len(tc.wantIgnoredEnvVars) == 0 && c.GetEnv("BUILDKITE_IGNORED_ENV") != "" {
					t.Errorf("BUILDKITE_IGNORED_ENV = %q, want empty", c.GetEnv("BUILDKITE_IGNORED_ENV"))
					c.Exit(1)
					return
				}

				c.Exit(0)
			})

			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()

			if err := runJob(t, ctx, testRunJobConfig{
				job:           job,
				server:        server,
				agentCfg:      tc.agentCfg,
				mockBootstrap: mb,
			}); err != nil {
				t.Fatalf("runJob() error = %v", err)
			}
		})
	}
}

func TestCheckoutInfraVarsAreAgentAuthoritative(t *testing.T) {
	t.Parallel()

	// SSH_KEYSCAN, GIT_MIRRORS_PATH, GIT_MIRRORS_LOCK_TIMEOUT and
	// GIT_MIRROR_CHECKOUT_MODE are agent-only: job env cannot override them even
	// under the most permissive checkout-override mode (none).
	tests := []struct {
		name         string
		varName      string
		jobEnvValue  string
		agentCfg     agent.AgentConfiguration
		wantEnvValue string
	}{
		{
			name:         "ssh_keyscan",
			varName:      "BUILDKITE_SSH_KEYSCAN",
			jobEnvValue:  "false",
			agentCfg:     agent.AgentConfiguration{SSHKeyscan: true, CheckoutOverrideMode: env.CheckoutOverrideNone},
			wantEnvValue: "true",
		},
		{
			name:         "git_mirrors_path",
			varName:      "BUILDKITE_GIT_MIRRORS_PATH",
			jobEnvValue:  "/tmp/attacker-mirrors",
			agentCfg:     agent.AgentConfiguration{GitMirrorsPath: "/agent/mirrors", CheckoutOverrideMode: env.CheckoutOverrideNone},
			wantEnvValue: "/agent/mirrors",
		},
		{
			name:         "git_mirrors_lock_timeout",
			varName:      "BUILDKITE_GIT_MIRRORS_LOCK_TIMEOUT",
			jobEnvValue:  "1",
			agentCfg:     agent.AgentConfiguration{GitMirrorsLockTimeout: 300, CheckoutOverrideMode: env.CheckoutOverrideNone},
			wantEnvValue: "300",
		},
		{
			name:         "git_mirror_checkout_mode",
			varName:      "BUILDKITE_GIT_MIRROR_CHECKOUT_MODE",
			jobEnvValue:  "id",
			agentCfg:     agent.AgentConfiguration{GitMirrorCheckoutMode: "raw", CheckoutOverrideMode: env.CheckoutOverrideNone},
			wantEnvValue: "raw",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			job := &api.Job{
				ID:                 "my-job-id",
				ChunksMaxSizeBytes: 1024,
				Env: map[string]string{
					"BUILDKITE_COMMAND": "echo hello world",
					tc.varName:          tc.jobEnvValue,
				},
				Token: "bkaj_job-token",
			}

			mb := mockBootstrap(t)
			defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

			mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
				if got, want := c.GetEnv(tc.varName), tc.wantEnvValue; got != want {
					t.Errorf("c.GetEnv(%s) = %q, want %q", tc.varName, got, want)
					c.Exit(1)
					return
				}

				ignored := strings.Split(strings.TrimSpace(c.GetEnv("BUILDKITE_IGNORED_ENV")), ",")
				if !slices.Contains(ignored, tc.varName) {
					t.Errorf("BUILDKITE_IGNORED_ENV = %q, want it to contain %q", c.GetEnv("BUILDKITE_IGNORED_ENV"), tc.varName)
					c.Exit(1)
					return
				}

				c.Exit(0)
			})

			e := createTestAgentEndpoint()
			server := e.server()
			defer server.Close()

			if err := runJob(t, ctx, testRunJobConfig{
				job:           job,
				server:        server,
				agentCfg:      tc.agentCfg,
				mockBootstrap: mb,
			}); err != nil {
				t.Fatalf("runJob() error = %v", err)
			}
		})
	}
}

func TestCheckoutOverrideModeIgnoresJobEnvOverride(t *testing.T) {
	t.Parallel()

	// The agent's checkout-override mode is authoritative: a job that supplies
	// BUILDKITE_CHECKOUT_OVERRIDE_MODE cannot relax the lock.
	ctx := t.Context()
	job := &api.Job{
		ID:                 "my-job-id",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_COMMAND":                "echo hello world",
			"BUILDKITE_CHECKOUT_OVERRIDE_MODE": "none",
		},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_CHECKOUT_OVERRIDE_MODE"), "strict"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_CHECKOUT_OVERRIDE_MODE) = %q, want %q", got, want)
			c.Exit(1)
			return
		}

		ignored := strings.Split(strings.TrimSpace(c.GetEnv("BUILDKITE_IGNORED_ENV")), ",")
		if !slices.Contains(ignored, "BUILDKITE_CHECKOUT_OVERRIDE_MODE") {
			t.Errorf("BUILDKITE_IGNORED_ENV = %q, want it to contain BUILDKITE_CHECKOUT_OVERRIDE_MODE", c.GetEnv("BUILDKITE_IGNORED_ENV"))
			c.Exit(1)
			return
		}

		c.Exit(0)
	})

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	if err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{CheckoutOverrideMode: env.CheckoutOverrideStrict},
		mockBootstrap: mb,
	}); err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}

func TestArtifactUploadConcurrencyFromAgentConfigIsSet(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	job := &api.Job{
		ID:                 "artifact-upload-concurrency-job",
		ChunksMaxSizeBytes: 1024,
		Step:               pipeline.CommandStep{},
		Token:              "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_ARTIFACT_UPLOAD_CONCURRENCY"), "7"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_ARTIFACT_UPLOAD_CONCURRENCY) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:    job,
		server: server,
		agentCfg: agent.AgentConfiguration{
			ArtifactUploadConcurrency: 7,
		},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}

func TestArtifactUploadConcurrencyFromJobEnvIsPreservedWhenAgentConfigUnset(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	job := &api.Job{
		ID:                 "artifact-upload-concurrency-env-job",
		ChunksMaxSizeBytes: 1024,
		Env: map[string]string{
			"BUILDKITE_ARTIFACT_UPLOAD_CONCURRENCY": "5",
		},
		Step:  pipeline.CommandStep{},
		Token: "bkaj_job-token",
	}

	mb := mockBootstrap(t)
	defer mb.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	mb.Expect().Once().AndExitWith(0).AndCallFunc(func(c *bintest.Call) {
		if got, want := c.GetEnv("BUILDKITE_ARTIFACT_UPLOAD_CONCURRENCY"), "5"; got != want {
			t.Errorf("c.GetEnv(BUILDKITE_ARTIFACT_UPLOAD_CONCURRENCY) = %q, want %q", got, want)
		}
		c.Exit(0)
	})

	e := createTestAgentEndpoint()
	server := e.server()
	defer server.Close()

	err := runJob(t, ctx, testRunJobConfig{
		job:           job,
		server:        server,
		agentCfg:      agent.AgentConfiguration{},
		mockBootstrap: mb,
	})
	if err != nil {
		t.Fatalf("runJob() error = %v", err)
	}
}
