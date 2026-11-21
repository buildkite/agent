package dockerbootstrap

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCLIEnvironmentAndArguments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires Unix")
	}
	t.Setenv("DOCKER_HOST", "tcp://untrusted:2375")
	t.Setenv("DOCKER_CONFIG", "/untrusted")
	t.Setenv("HOST_SECRET", "host-only")
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	const script = `#!/bin/sh
printf '%s\n' "$@"
printf 'MULTILINE:%s\n' "$MULTILINE"
printf 'HOST_SECRET:%s\n' "${HOST_SECRET-unset}"
printf 'DOCKER_HOST:%s\n' "${DOCKER_HOST-unset}"
exit 42
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	value := "secret\n世界  "
	code, err := (CLI{Path: path, ConfigDir: dir}).Run(t.Context(), []string{"create", "--env", "MULTILINE", DefaultImage}, map[string]string{"MULTILINE": value}, &out, &out)
	if err != nil || code != 42 {
		t.Fatalf("got (%d, %v)", code, err)
	}
	want := "--host\nunix:///var/run/docker.sock\n--config\n" + dir + "\ncreate\n--env\nMULTILINE\n" + DefaultImage + "\nMULTILINE:" + value + "\nHOST_SECRET:unset\nDOCKER_HOST:unset\n"
	if out.String() != want {
		t.Fatalf("unexpected client arguments/environment: %q", out.String())
	}
}

func TestCLICancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires Unix")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	var out bytes.Buffer
	_, err := (CLI{Path: path, ConfigDir: dir}).Run(ctx, []string{"info"}, nil, &out, &out)
	if err != context.DeadlineExceeded {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("Docker client did not stop promptly")
	}
}

func TestDockerEnvironmentUsesEnvFile(t *testing.T) {
	for _, value := range []string{"1", "two\nlines"} {
		cfg := testConfig(t)
		for _, pair := range cfg.Environment {
			if path, ok := strings.CutPrefix(pair, "BUILDKITE_ENV_JSON_FILE="); ok {
				if err := os.WriteFile(path, []byte(`{"DOCKER_BUILDKIT":""}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		cfg.Environment = append(cfg.Environment, "DOCKER_BUILDKIT="+value)
		job, err := prepare(cfg)
		if strings.Contains(value, "\n") {
			if err == nil {
				t.Fatal("accepted a multiline value the env file cannot carry")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		// The host Docker client must never see job DOCKER_* values.
		if _, ok := job.env["DOCKER_BUILDKIT"]; ok {
			t.Fatal("DOCKER_BUILDKIT reached the Docker client environment")
		}
		i := slices.Index(job.args, "--env-file")
		if i < 0 {
			t.Fatal("missing --env-file")
		}
		data, err := os.ReadFile(job.args[i+1])
		if err != nil || string(data) != "DOCKER_BUILDKIT=1\n" {
			t.Fatalf("env file = %q, %v", data, err)
		}
	}
}
