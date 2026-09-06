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
test -w "$HOME"
touch "$HOME/home-test"
echo workspace-ok > ownership-test
`, uid, gid)
			cfg.Environment = append(cfg.Environment, "BUILDKITE_BUILD_PATH="+build, "BUILDKITE_BUILD_CHECKOUT_PATH="+build, "BUILDKITE_BOOTSTRAP_PHASES=command", "BUILDKITE_COMMAND="+command, "BUILDKITE_JOB_ID=user-integration", "BUILDKITE_CANCEL_SIGNAL_TIMEOUT=10s", "BUILDKITE_PTY=false", "BUILDKITE_REPO=.", "BUILDKITE_COMMIT=HEAD", "BUILDKITE_BRANCH=main", "BUILDKITE_PIPELINE_PROVIDER=custom", "BUILDKITE_AGENT_NAME=test-agent", "BUILDKITE_ORGANIZATION_SLUG=test-org", "BUILDKITE_PIPELINE_SLUG=test-pipeline")
			cfg.PullPolicy = "never"
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
			var remaining bytes.Buffer
			if _, err := client.Run(t.Context(), []string{"ps", "-a", "-q", "--filter", "label=com.buildkite.job-id=user-integration"}, nil, &remaining, &remaining); err != nil || remaining.Len() != 0 {
				t.Fatalf("containers remain: %v %s", err, remaining.String())
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
