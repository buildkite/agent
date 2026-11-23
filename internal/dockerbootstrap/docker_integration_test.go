//go:build linux

package dockerbootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestDockerToolsIntegration uses an agent-free, glibc image with git and ssh.
// It runs Compose against the VM-local daemon, never a nested Docker daemon.
func TestDockerToolsIntegration(t *testing.T) {
	binary, image, plugins := os.Getenv("DOCKER_BOOTSTRAP_TEST_BINARY"), os.Getenv("DOCKER_BOOTSTRAP_TEST_IMAGE"), os.Getenv("DOCKER_BOOTSTRAP_TEST_CLI_PLUGINS")
	if binary == "" || image == "" || plugins == "" {
		t.Skip("set DOCKER_BOOTSTRAP_TEST_BINARY, DOCKER_BOOTSTRAP_TEST_IMAGE and DOCKER_BOOTSTRAP_TEST_CLI_PLUGINS")
	}
	for _, socket := range []bool{false, true} {
		t.Run(fmt.Sprintf("socket=%t", socket), func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Binary, cfg.ImageFromJob, cfg.PullPolicy, cfg.User = binary, true, "never", "0:0"
			cfg.CleanupMargin, cfg.OperationTimeout, cfg.PullTimeout = 3*time.Second, 15*time.Second, time.Minute
			cfg.MountDockerSocket, cfg.DockerPath = socket, "/usr/bin/docker"
			if socket {
				cfg.DockerCLIPlugins = plugins
			}
			build := t.TempDir()
			project := fmt.Sprintf("bk-tools-%d", time.Now().UnixNano())
			// Compose sees the same checkout path as the job and host daemon.
			compose := fmt.Sprintf("services:\n  proof:\n    image: %s\n    pull_policy: never\n    command: [sh, -c, 'echo compose-ok > /workspace/compose-result']\n    volumes:\n      - %s:/workspace\n", image, build)
			if err := os.WriteFile(filepath.Join(build, "compose.yml"), []byte(compose), 0o600); err != nil {
				t.Fatal(err)
			}
			command := `set -eu
test -f /.dockerenv
test "$(command -v buildkite-agent)" = /bin/buildkite-agent
test "$BUILDKITE_BIN_PATH" = /bin
awk '$2 == "/usr/bin/buildkite-agent" || $2 == "/bin/buildkite-agent" { if ($4 ~ /^ro,/) found=1 } END { exit !found }' /proc/mounts
test -n "$(command -v node)"
node --version
`
			if socket {
				command += fmt.Sprintf(`
test -S /var/run/docker.sock
docker version
docker buildx version
docker compose version
trap 'docker compose -p %s -f compose.yml down --remove-orphans' EXIT
docker compose -p %s -f compose.yml up --abort-on-container-exit --exit-code-from proof
test "$(cat compose-result)" = compose-ok
echo tools-and-compose-ok
`, project, project)
			} else {
				command += "test ! -S /var/run/docker.sock\necho isolated-ok\n"
			}
			cfg.Environment = append(cfg.Environment,
				"BUILDKITE_IMAGE=ambient-invalid-image", "BUILDKITE_BUILD_PATH="+build, "BUILDKITE_BUILD_CHECKOUT_PATH="+build,
				"BUILDKITE_BOOTSTRAP_PHASES=command", "BUILDKITE_COMMAND="+command,
				"BUILDKITE_JOB_ID="+project, "BUILDKITE_CANCEL_SIGNAL_TIMEOUT=10s", "BUILDKITE_PTY=false",
				"BUILDKITE_REPO=.", "BUILDKITE_COMMIT=HEAD", "BUILDKITE_BRANCH=main", "BUILDKITE_PIPELINE_PROVIDER=custom",
				"BUILDKITE_AGENT_NAME=test-agent", "BUILDKITE_ORGANIZATION_SLUG=test-org", "BUILDKITE_PIPELINE_SLUG=test-pipeline")
			for _, pair := range cfg.Environment {
				if path, ok := strings.CutPrefix(pair, "BUILDKITE_ENV_JSON_FILE="); ok {
					data, _ := json.Marshal(map[string]string{"BUILDKITE_IMAGE": image})
					if err := os.WriteFile(path, data, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			var output bytes.Buffer
			client := CLI{Path: "/usr/bin/docker", ConfigDir: t.TempDir()}
			code, err := (Runner{Client: client, Stdout: &output, Stderr: &output}).Run(t.Context(), cfg)
			want := "isolated-ok"
			if socket {
				want = "tools-and-compose-ok"
			}
			if code != 0 || err != nil || !strings.Contains(output.String(), want) || !strings.Contains(output.String(), "Docker bootstrap image: "+image) {
				t.Fatalf("code=%d err=%v\n%s", code, err, output.String())
			}
			for _, args := range [][]string{
				{"ps", "--all", "--quiet", "--filter", "label=com.buildkite.job-id=" + project},
				{"network", "ls", "--quiet", "--filter", "label=com.buildkite.job-id=" + project},
				{"ps", "--all", "--quiet", "--filter", "label=com.docker.compose.project=" + project},
				{"network", "ls", "--quiet", "--filter", "label=com.docker.compose.project=" + project},
			} {
				var remaining bytes.Buffer
				status, err := client.Run(t.Context(), args, nil, &remaining, &remaining)
				if status != 0 || err != nil || remaining.Len() != 0 {
					t.Fatalf("cleanup check %v: %d %v %s", args, status, err, remaining.String())
				}
			}
			t.Log(output.String())
		})
	}
}

// An explicit binary opts into tests that create resources on the local daemon.
func TestDockerUserIntegration(t *testing.T) {
	binary := os.Getenv("DOCKER_BOOTSTRAP_TEST_BINARY")
	if binary == "" {
		t.Skip("set DOCKER_BOOTSTRAP_TEST_BINARY to a static Linux agent binary")
	}
	users := []string{"", "0:0"}
	if os.Getuid() == 0 {
		users = append(users, "12345:12345")
	}
	for _, user := range users {
		t.Run("user="+user, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.Binary = binary
			cfg.User = user
			cfg.CleanupMargin = 3 * time.Second
			cfg.OperationTimeout = 15 * time.Second
			cfg.PullTimeout = time.Minute
			build := filepath.Join(t.TempDir(), "workspace")
			hooks := filepath.Join(build, ".buildkite", "hooks")
			if err := os.MkdirAll(hooks, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, script := range map[string]string{
				"pre-command": "#!/bin/sh\ntest -f /.dockerenv || exit 99\nexport HOOK_WORKED=yes\n",
				"pre-exit":    "#!/bin/sh\ntest -f /.dockerenv || exit 99\necho pre-exit-ok\n",
			} {
				if err := os.WriteFile(filepath.Join(hooks, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			uid, gid, err := containerUser(user)
			if err != nil {
				t.Fatal(err)
			}
			if os.Getuid() == 0 && uid != 0 {
				if err := os.Chown(build, uid, gid); err != nil {
					t.Fatal(err)
				}
			}
			command := fmt.Sprintf(`set -eu
test -f /.dockerenv
test "$(id -u)" = %d
test "$(id -g)" = %d
test "$HOOK_WORKED" = yes
getent passwd "$(id -u)"
getent group "$(id -g)"
getent passwd _apt
test "$(getent passwd "$(id -u)" | cut -d: -f6)" = "$HOME"
test "$USER" = "$(id -un)"
test "$DOCKER_BUILDKIT" = 1
test -w "$HOME"
touch "$HOME/home-test"
echo workspace-ok > ownership-test
`, uid, gid)
			cfg.Environment = append(cfg.Environment, "BUILDKITE_BUILD_PATH="+build, "BUILDKITE_BUILD_CHECKOUT_PATH="+build, "BUILDKITE_BOOTSTRAP_PHASES=command", "BUILDKITE_COMMAND="+command, "BUILDKITE_JOB_ID=user-integration", "BUILDKITE_CANCEL_SIGNAL_TIMEOUT=10s", "BUILDKITE_PTY=false", "BUILDKITE_REPO=.", "BUILDKITE_COMMIT=HEAD", "BUILDKITE_BRANCH=main", "BUILDKITE_PIPELINE_PROVIDER=custom", "BUILDKITE_AGENT_NAME=test-agent", "BUILDKITE_ORGANIZATION_SLUG=test-org", "BUILDKITE_PIPELINE_SLUG=test-pipeline")
			cfg.PullPolicy = "never"
			for _, pair := range cfg.Environment {
				if path, ok := strings.CutPrefix(pair, "BUILDKITE_ENV_JSON_FILE="); ok {
					if err := os.WriteFile(path, []byte(`{"DOCKER_BUILDKIT":"1"}`), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			cfg.Environment = append(cfg.Environment, "DOCKER_BUILDKIT=1")
			client := CLI{Path: "/usr/bin/docker", ConfigDir: t.TempDir()}
			for range 2 {
				var output bytes.Buffer
				code, err := (Runner{Client: client, Stdout: &output, Stderr: &output}).Run(t.Context(), cfg)
				if err != nil || code != 0 || !strings.Contains(output.String(), "pre-exit-ok") {
					t.Fatalf("code=%d err=%v\n%s", code, err, output.String())
				}
			}
			if uid != 0 {
				if err := os.Chmod(build, 0o555); err != nil {
					t.Fatal(err)
				}
				var output bytes.Buffer
				code, runErr := (Runner{Client: client, Stdout: &output, Stderr: &output}).Run(t.Context(), cfg)
				if err := os.Chmod(build, 0o755); err != nil {
					t.Fatal(err)
				}
				if runErr != nil || code != SetupFailure || !strings.Contains(output.String(), "cannot write directory") || strings.Contains(output.String(), "pre-exit-ok") {
					t.Fatalf("permission preflight: code=%d err=%v %s", code, runErr, output.String())
				}
			}
			info, err := os.Stat(filepath.Join(build, "ownership-test"))
			if err != nil {
				t.Fatal(err)
			}
			stat := info.Sys().(*syscall.Stat_t)
			if int(stat.Uid) != uid || int(stat.Gid) != gid {
				t.Fatalf("owner=%d:%d want=%d:%d", stat.Uid, stat.Gid, uid, gid)
			}
			// Root-created fixtures need removal before the unprivileged test cleanup.
			if uid == 0 && os.Getuid() != 0 {
				cmd := exec.CommandContext(t.Context(), "docker", "run", "--rm", "--entrypoint", "/bin/sh", "--mount", "type=bind,src="+build+",dst=/workspace", DefaultImage, "-c", "rm -f /workspace/ownership-test")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("fixture cleanup: %v %s", err, out)
				}
			}
			for _, args := range [][]string{
				{"ps", "-a", "-q", "--filter", "label=com.buildkite.job-id=user-integration"},
				{"network", "ls", "-q", "--filter", "label=com.buildkite.job-id=user-integration"},
			} {
				var remaining bytes.Buffer
				if status, err := client.Run(t.Context(), args, nil, &remaining, &remaining); err != nil || status != 0 || remaining.Len() != 0 {
					t.Fatalf("resources remain (%v): %v %s", args, err, remaining.String())
				}
			}
		})
	}
}

func TestDockerRegistryIntegration(t *testing.T) {
	binary, image, source := os.Getenv("DOCKER_BOOTSTRAP_TEST_BINARY"), os.Getenv("DOCKER_BOOTSTRAP_TEST_IMAGE"), os.Getenv("DOCKER_BOOTSTRAP_TEST_AUTH")
	if binary == "" || image == "" || source == "" {
		t.Skip("set Docker bootstrap binary, image and auth fixture variables")
	}
	auth, err := LoadAuthentication(source, "", "")
	if err != nil {
		t.Fatal(err)
	}
	config := t.TempDir()
	if err := os.WriteFile(filepath.Join(config, "config.json"), auth.Config, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, policy               string
		authenticated, wantFailure bool
	}{
		{"uncached never", "never", false, true},
		{"uncached missing", "missing", false, true},
		{"authenticated refresh", "always", true, false},
		{"unauthenticated refresh with cache", "always", false, true},
		{"cached missing policy", "missing", false, false},
		{"cached never policy", "never", false, false},
		{"authenticated missing policy", "missing", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "authenticated missing policy" || tc.name == "uncached never" {
				cmd := exec.CommandContext(t.Context(), "docker", "image", "rm", image)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("remove test image tag: %v %s", err, out)
				}
			}
			cfg := testConfig(t)
			cfg.Binary = binary
			cfg.Image = image
			cfg.PullPolicy = tc.policy
			cfg.DockerConfig = source
			cfg.RedactedValues = auth.Secrets
			cfg.CleanupMargin = 3 * time.Second
			cfg.OperationTimeout = 15 * time.Second
			cfg.PullTimeout = time.Minute
			cfg.Environment = append(cfg.Environment, "BUILDKITE_CANCEL_SIGNAL_TIMEOUT=10s", "BUILDKITE_BOOTSTRAP_PHASES=command", "BUILDKITE_COMMAND=test -f /.dockerenv", "BUILDKITE_JOB_ID=registry-integration", "BUILDKITE_REPO=.", "BUILDKITE_COMMIT=HEAD", "BUILDKITE_BRANCH=main", "BUILDKITE_PIPELINE_PROVIDER=custom", "BUILDKITE_AGENT_NAME=test-agent", "BUILDKITE_ORGANIZATION_SLUG=test-org", "BUILDKITE_PIPELINE_SLUG=test-pipeline")
			client := CLI{Path: "/usr/bin/docker", ConfigDir: t.TempDir()}
			if tc.authenticated {
				client.AuthConfigDir = config
			}
			var out bytes.Buffer
			code, err := (Runner{Client: client, Stdout: &out, Stderr: &out}).Run(t.Context(), cfg)
			if (err != nil) != tc.wantFailure || (code != 0) != tc.wantFailure {
				t.Fatalf("code=%d err=%v\n%s", code, err, out.String())
			}
			for _, secret := range auth.Secrets {
				if strings.Contains(out.String(), secret) || (err != nil && strings.Contains(err.Error(), secret)) {
					t.Fatal("credential leaked in diagnostics")
				}
			}
		})
	}
}

func TestDockerCredentialHelperIntegration(t *testing.T) {
	image, source := os.Getenv("DOCKER_BOOTSTRAP_TEST_IMAGE"), os.Getenv("DOCKER_BOOTSTRAP_TEST_AUTH")
	if image == "" || source == "" {
		t.Skip("requires private registry fixture")
	}
	data, err := os.ReadFile(filepath.Join(source, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sourceConfig struct {
		Auths map[string]struct{ Auth string }
	}
	if err := json.Unmarshal(data, &sourceConfig); err != nil {
		t.Fatal(err)
	}
	registry := strings.Split(image, "/")[0]
	decoded, err := base64.StdEncoding.DecodeString(sourceConfig.Auths[registry].Auth)
	if err != nil {
		t.Fatal(err)
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok {
		t.Fatal("fixture requires basic auth")
	}
	credentials, err := json.Marshal(map[string]string{"Username": username, "Secret": password})
	if err != nil {
		t.Fatal(err)
	}
	helperDir, config := t.TempDir(), t.TempDir()
	script := `#!/bin/sh
if [ "${DELAY:-}" = yes ]; then sleep 60; fi
printf '%s\n' "$CREDENTIAL_JSON"
`
	if err := os.WriteFile(filepath.Join(helperDir, "docker-credential-phase2-test"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	configData, _ := json.Marshal(map[string]any{"credHelpers": map[string]string{registry: "phase2-test"}})
	if err := os.WriteFile(filepath.Join(config, "config.json"), configData, 0o600); err != nil {
		t.Fatal(err)
	}
	client := CLI{Path: "/usr/bin/docker", ConfigDir: t.TempDir(), AuthConfigDir: config, HelperPath: helperDir + ":/usr/bin:/bin", HelperEnvironment: map[string]string{"CREDENTIAL_JSON": string(credentials)}}
	var output bytes.Buffer
	code, err := client.Run(t.Context(), []string{"pull", image}, nil, &output, &output)
	if code != 0 || err != nil {
		t.Fatalf("helper pull failed (status %d): %v", code, err)
	}
	if strings.Contains(output.String(), password) {
		t.Fatal("helper credential leaked")
	}
	client.HelperEnvironment["CREDENTIAL_JSON"] = `{"Username":"test","Secret":"invalid-fixture-password"}`
	code, err = client.Run(t.Context(), []string{"pull", image}, nil, &output, &output)
	if err != nil || code == 0 {
		t.Fatalf("invalid credential result: %d %v", code, err)
	}
	client.HelperEnvironment["DELAY"] = "yes"
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = client.Run(ctx, []string{"pull", image}, nil, &output, &output)
	if err != context.DeadlineExceeded || time.Since(started) > 3*time.Second {
		t.Fatalf("helper cancellation: %v after %v", err, time.Since(started))
	}
}
