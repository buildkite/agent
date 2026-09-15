package containerimage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildImageVersionGuard(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("requires Bash")
	}
	script, err := filepath.Abs("../../.buildkite/steps/build-docker-image.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"alpine", "alpine-k8s", "ubuntu-20.04", "ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04", "sidecar"} {
		for _, version := range []string{"", "3.138.0"} {
			t.Run(variant+"/"+version, func(t *testing.T) {
				dir := t.TempDir()
				log := filepath.Join(dir, "commands")
				wrapper := `record() { printf '%s\n' "$*" >> "$COMMAND_LOG"; }
rm() { record rm "$@"; }
mkdir() { record mkdir "$@"; }
chmod() { record chmod "$@"; }
cp() { record cp "$@"; }
curl() { record curl "$@"; }
buildkite-agent() { record buildkite-agent "$@"; }
docker() { record docker "$@"; return 77; }
export -f record rm mkdir chmod cp curl buildkite-agent docker
exec bash "$@"`
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "-c", wrapper, "test", script, variant, "local-test", "stable", version)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "COMMAND_LOG="+log)
				out, err := cmd.CombinedOutput()
				if err == nil {
					t.Fatal("script reached beyond mocked build boundary")
				}
				commands, readErr := os.ReadFile(log)
				if variant != "sidecar" && version != "" {
					if cmd.ProcessState.ExitCode() != 1 || !strings.Contains(string(out), "Downloaded-version builds are unsupported") {
						t.Fatalf("guard result: %v\n%s", err, out)
					}
					if !os.IsNotExist(readErr) {
						t.Fatalf("guard ran side-effecting commands: %q, %v", commands, readErr)
					}
					return
				}
				if cmd.ProcessState.ExitCode() != 77 || readErr != nil {
					t.Fatalf("expected mocked Docker boundary: %v, %v\n%s", err, readErr, out)
				}
				got := string(commands)
				for _, arch := range []string{"amd64", "arm64"} {
					want := "buildkite-agent artifact download pkg/buildkite-agent-linux-" + arch + " .\n"
					if version != "" {
						want = "curl -Lf -o pkg/buildkite-agent-linux-" + arch + " https://download.buildkite.com/agent/stable/" + version + "/buildkite-agent-linux-" + arch + "\n"
					}
					if !strings.Contains(got, want) {
						t.Fatalf("missing %q in commands:\n%s", want, got)
					}
					if variant != "sidecar" {
						want = "buildkite-agent artifact download tmp/buildkite-container-launch-linux-" + arch + " .\n"
						if !strings.Contains(got, want) {
							t.Fatalf("missing paired helper: %s", got)
						}
					}
				}
				if variant == "sidecar" && strings.Contains(got, "container-launch") {
					t.Fatalf("sidecar requested a helper: %s", got)
				}
			})
		}
	}
}
