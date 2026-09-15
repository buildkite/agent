//go:build linux || darwin

package containerimage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	envSecret      = "dummy-container-environment-secret"
	overrideSecret = "dummy-container-override-secret"
	firstSecret    = "dummy-container-first-secret"
)

func TestEntrypointsMatch(t *testing.T) {
	root := "../.."
	if _, err := os.Stat("packaging/docker"); err == nil {
		root = "."
	}
	var want string
	for _, distro := range []string{"alpine", "alpine-k8s", "ubuntu-20.04", "ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04"} {
		b, err := os.ReadFile(filepath.Join(root, "packaging/docker", distro, "entrypoint.sh"))
		if err != nil {
			t.Fatal(err)
		}
		got := strings.ReplaceAll(string(b), "/usr/bin/tini", "/sbin/tini")
		if want == "" {
			want = got
		} else if got != want {
			t.Errorf("%s entrypoint differs beyond the tini path", distro)
		}
	}
}

func TestContainerEnvironment(t *testing.T) {
	image := os.Getenv("BUILDKITE_TEST_CONTAINER_IMAGE")
	if image == "" {
		t.Skip("set BUILDKITE_TEST_CONTAINER_IMAGE to a built agent image")
	}
	arch := os.Getenv("BUILDKITE_TEST_CONTAINER_ARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != arch {
		fixture = filepath.Join(dir, "fixture")
		build := exec.CommandContext(t.Context(), "go", "test", "-c", "-o", fixture, ".")
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build fixture: %v\n%s", err, out)
		}
	}
	startup := filepath.Join(dir, "startup")
	if err := os.Mkdir(startup, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
set -eu
if [ "$TOKEN_CASE" = startup-failure ]; then exit 42; fi
if [ "$TOKEN_CASE" = generated-file ]; then /fixture -test.run '^TestContainerManagedFile$'; fi
buildkite-agent --version
/fixture -test.run '^TestContainerFixture$' &
while [ ! -f /tmp/listening ]; do sleep 0.05; done
`
	if err := os.WriteFile(filepath.Join(startup, "fixture"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"environment", "override", "file", "fd", "env-fd", "generated-file", "boundary", "oversized", "unicode-oversized", "empty", "bad-fd", "missing-file", "startup-failure", "rejected", "help", "help-bad-fd", "root-help", "version", "unknown", "invalid", "bootstrap", "space", "equals", "duplicates", "overridden-fd", "overridden-arg-fd", "root-help-false", "root-terminator", "root-version-false", "root-aliases", "root-version-alias", "root-help-overridden", "start-help-false", "file-terminator", "plain-terminator", "token-like-value", "help-like-value", "help-pending", "shared-env-fd", "overridden-shared-env-fd", "error-pending", "int-error-pending", "cli-oversized", "fd-oversized"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
			defer cancel()
			docker := func(args ...string) string {
				t.Helper()
				out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
				if err != nil {
					t.Fatalf("docker %v: %v\n%s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			args := []string{
				"create", "--network=none", "--platform=linux/" + arch,
				"-v", fixture + ":/fixture:ro", "-v", startup + ":/docker-entrypoint.d:ro",
				"-e", "TOKEN_CASE=" + name, "-e", "BUILDKITE_AGENT_TOKEN=" + envSecret,
				"-e", "SSH_CONFIG=Host example.invalid", "-e", "BUILDKITE_AGENT_ENDPOINT=http://127.0.0.1:18080",
				"-e", "BUILDKITE_AGENT_PING_MODE=poll-only", "-e", "BUILDKITE_AGENT_NO_HTTP2=true",
			}
			for key, target := range map[string]string{"BINARY": "buildkite-agent", "ENTRYPOINT": "buildkite-agent-entrypoint", "HELPER": "buildkite-container-launch"} {
				if source := os.Getenv("BUILDKITE_TEST_CONTAINER_" + key); source != "" {
					source, err = filepath.EvalSymlinks(source)
					if err != nil {
						t.Fatal(err)
					}
					args = append(args, "-v", source+":/usr/local/bin/"+target+":ro")
				}
			}
			command := []string{"start", "--name=container-test", "--no-color"}
			success, zero := true, true
			switch name {
			case "space":
				command = append(command, "--token", overrideSecret)
			case "equals":
				command = append(command, "--token="+overrideSecret)
			case "duplicates":
				command = append(command, "--token="+firstSecret, "-token="+overrideSecret)
			case "root-help-false", "root-terminator", "root-aliases", "root-help-overridden":
				prefix := map[string][]string{"root-help-false": {"--help=false"}, "root-terminator": {"--"}, "root-aliases": {"-h=false"}, "root-help-overridden": {"--help=true", "-h=false"}}[name]
				command = append(prefix, append(command, "--token="+overrideSecret)...)
			case "root-version-false", "root-version-alias":
				command = append([]string{map[string]string{"root-version-false": "--version=false", "root-version-alias": "-v=false"}[name]}, command...)
				success = false
			case "start-help-false":
				command = append(command, "--token="+overrideSecret, "--help=true", "-h=false")
			case "plain-terminator":
				command = append(command, "--token="+overrideSecret, "--", "--token=fd://99")
			case "help-like-value":
				command = append(command, "--token="+overrideSecret, "--name", "--help")
			case "token-like-value":
				args = append(args, "--entrypoint=/bin/bash")
				command = append(command, "--token="+overrideSecret, "--name", "--token=fd://9")
				command = append([]string{"-c", "printf ordinary-descriptor > /tmp/ordinary; exec 9</tmp/ordinary; rm /tmp/ordinary; exec /usr/local/bin/buildkite-agent-entrypoint \"$@\"", "input"}, command...)
			case "shared-env-fd", "overridden-shared-env-fd":
				args = append(args, "--entrypoint=/bin/bash")
				command = append(command, "--token=fd://9")
				if name == "overridden-shared-env-fd" {
					command = append(command, "--token="+overrideSecret)
				}
				setup := "exec 9< <(printf '%s' \"$BUILDKITE_AGENT_TOKEN\"); wait $!; export BUILDKITE_AGENT_TOKEN=fd://9 BUILDKITE_AGENT_CONTAINER_TOKEN_FD=9; exec /usr/local/bin/buildkite-agent-entrypoint \"$@\""
				command = append([]string{"-c", setup, "input"}, command...)
			case "help-pending", "error-pending", "int-error-pending", "fd-oversized":
				args = append(args, "--entrypoint=/fixture")
				command = []string{"-test.run", "^TestContainerPendingToken$"}
				success, zero = false, name == "help-pending"
			case "cli-oversized":
				command = append(command, "--token="+overrideSecret+strings.Repeat("x", 4097))
				success, zero = false, false
			case "bootstrap":
				command = []string{"bootstrap", "--phases=command", "--job=example", "--build-path=/tmp/builds", "--repository=.", "--commit=HEAD", "--branch=main", "--pipeline-provider=custom", "--agent=test", "--organization=test", "--pipeline=test", "--command=/fixture -test.run '^TestBootstrapEnvironment$'"}
				success = false
			case "override":
				command = append(command, "--token="+overrideSecret)
			case "file", "file-terminator", "fd", "env-fd", "overridden-fd", "overridden-arg-fd":
				args = append(args, "--entrypoint=/bin/bash")
				setup := "printf '%s' " + overrideSecret + " > /tmp/managed-token; "
				if name == "file" || name == "file-terminator" {
					command = append(command, "--token="+firstSecret, "--token=file:///tmp/managed-token")
					if name == "file-terminator" {
						command = append(command, "--", "--token=fd://99")
					}
				} else {
					setup += "exec 9</tmp/managed-token; rm /tmp/managed-token; "
					if name == "env-fd" {
						setup = "exec 9< <(printf '%s' " + overrideSecret + "); wait $!; "
						setup += "export BUILDKITE_AGENT_TOKEN=fd://9; "
					} else {
						command = append(command, "--token=fd://9")
						if name == "overridden-fd" || name == "overridden-arg-fd" {
							command = append(command, "--token="+firstSecret, "--token="+overrideSecret)
						}
					}
				}
				command = append([]string{"-c", setup + "exec /usr/local/bin/buildkite-agent-entrypoint \"$@\"", "input"}, command...)
			case "generated-file":
				args = append(args, "-e", "BUILDKITE_AGENT_TOKEN=file:///tmp/managed-token")
			case "boundary", "oversized", "unicode-oversized":
				value := envSecret + strings.Repeat("x", 4096-len(envSecret))
				if name == "oversized" {
					value += "x"
				}
				if name == "unicode-oversized" {
					value = strings.Repeat("é", 3000)
				}
				args = append(args, "-e", "BUILDKITE_AGENT_TOKEN="+value, "-e", "LC_ALL=C.UTF-8")
				success, zero = name == "boundary", name == "boundary"
			case "empty", "bad-fd", "missing-file", "invalid":
				flag := map[string]string{"empty": "--token=", "bad-fd": "--token=fd://99", "missing-file": "--token=file:///missing", "invalid": "--not-an-agent-flag"}[name]
				command = append(command, "--token="+firstSecret)
				command = append(command, flag)
				success, zero = false, false
			case "startup-failure", "rejected":
				success, zero = false, false
			case "help", "help-bad-fd":
				command = []string{"start", "--token=" + firstSecret, "--help"}
				if name == "help-bad-fd" {
					command = append(command, "--token=fd://99")
				}
				success = false
			case "root-help", "version", "unknown":
				command = []string{map[string]string{"root-help": "--help", "version": "--version", "unknown": "unknown-command"}[name]}
				success, zero = false, name != "unknown"
			}
			id := docker(append(append(args, image), command...)...)
			t.Cleanup(func() {
				if t.Failed() {
					out, _ := exec.Command("docker", "logs", id).CombinedOutput()
					t.Logf("container logs:\n%s", out)
				}
				_ = exec.Command("docker", "rm", "-f", "-v", id).Run()
			})
			docker("start", id)
			if success {
				for !strings.Contains(docker("logs", id), "fixture: checked") {
					if logs := docker("logs", id); strings.Contains(logs, "--- FAIL:") || strings.Contains(logs, "fixture failure:") {
						t.Fatal("container fixture failed")
					}
					if docker("inspect", "-f", "{{.State.Running}}", id) != "true" {
						t.Fatal("container exited before authenticated polling")
					}
					time.Sleep(100 * time.Millisecond)
				}
				docker("kill", "--signal=TERM", id)
			}
			if exit := docker("wait", id); (exit == "0") != zero {
				t.Fatalf("exit %s; want success=%t", exit, zero)
			}
			logs := docker("logs", id)
			if strings.Contains(logs, "fixture failure:") || strings.Contains(logs, "--- FAIL:") {
				t.Fatal("container fixture failed")
			}
			if success && !strings.Contains(logs, "fixture: disconnected") {
				t.Fatal("SIGTERM did not cause graceful disconnect")
			}
			if strings.Contains(name, "oversized") && (!strings.Contains(logs, "4096-byte limit") || strings.Contains(logs, "Executing scripts")) {
				t.Fatal("oversized token was not rejected before startup")
			}
			if name == "rejected" && !strings.Contains(logs, "fixture: rejected cleanly") {
				t.Fatal("registration failure was not checked")
			}
			if name == "bootstrap" && !strings.Contains(logs, "fixture: bootstrap checked") {
				t.Fatal("bootstrap environment was not checked")
			}
		})
	}
}

func TestBootstrapEnvironment(t *testing.T) {
	if os.Getenv("TOKEN_CASE") != "bootstrap" {
		t.Skip("container-only fixture")
	}
	if err := inspectEnvironment(true, "bootstrap"); err != nil {
		t.Fatal(err)
	}
	fmt.Println("fixture: bootstrap checked")
}

func TestContainerManagedFile(t *testing.T) {
	if os.Getenv("TOKEN_CASE") != "generated-file" {
		t.Skip("container-only fixture")
	}
	if err := os.WriteFile("/tmp/managed-token", []byte(overrideSecret), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestContainerFixture(t *testing.T) {
	name := os.Getenv("TOKEN_CASE")
	if name == "" {
		t.Skip("container-only fixture")
	}
	want := envSecret
	if name != "environment" && name != "boundary" && name != "rejected" && name != "shared-env-fd" {
		want = overrideSecret
	}
	if name == "boundary" {
		want += strings.Repeat("x", 4096-len(envSecret))
	}
	if err := inspectEnvironment(false, name); err != nil {
		t.Fatal(err)
	}
	orphan, err := exec.Command("/bin/sh", "-c", "sleep 0.1 >/dev/null 2>&1 & echo $!").Output()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	fatal := func(message any) {
		fmt.Fprintln(os.Stderr, "fixture failure:", message)
		os.Exit(1)
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/register":
			if name == "token-like-value" || name == "help-like-value" {
				var body struct{ Name string }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					fatal(err)
				}
				wantName := "--token=fd://9"
				if name == "help-like-value" {
					wantName = "--help"
				}
				if body.Name != wantName {
					fatal("non-token flag value changed")
				}
			}
			if r.Header.Get("Authorization") != "Token "+want {
				fatal("registration precedence mismatch")
			}
			if err := inspectEnvironment(true, name); err != nil {
				fatal(err)
			}
			if name == "rejected" {
				fmt.Println("fixture: rejected cleanly")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = fmt.Fprint(w, `{"id":"test-agent","name":"test","access_token":"dummy-session-token","endpoint":"http://127.0.0.1:18080","ping_interval":1,"heartbeat_interval":60,"job_status_interval":1}`)
		case "/ping":
			if r.Header.Get("Authorization") != "Token dummy-session-token" {
				fatal("poll did not use session token")
			}
			if err := inspectEnvironment(true, name); err != nil {
				fatal(err)
			}
			if _, err := os.Stat("/proc/" + strings.TrimSpace(string(orphan))); os.IsNotExist(err) {
				fmt.Println("fixture: checked")
			}
		case "/disconnect":
			fmt.Println("fixture: disconnected")
		}
		_, _ = fmt.Fprint(w, `{}`)
	})}
	defer func() { _ = server.Close() }()
	if err := os.WriteFile("/tmp/listening", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(listener); err != nil {
		t.Fatal(err)
	}
}

func inspectEnvironment(ready bool, name string) error {
	if ready {
		comm, err := os.ReadFile("/proc/1/comm")
		if err != nil || strings.TrimSpace(string(comm)) != "tini" {
			return fmt.Errorf("PID 1 is not tini: %q, %v", comm, err)
		}
		ssh, err := os.ReadFile("/root/.ssh/config")
		if err != nil || !bytes.Contains(ssh, []byte("Host example.invalid")) {
			return fmt.Errorf("SSH setup did not run: %v", err)
		}
		if name == "file" || name == "file-terminator" || name == "generated-file" {
			b, err := os.ReadFile("/tmp/managed-token")
			if err != nil || string(b) != overrideSecret {
				return fmt.Errorf("operator token file changed: %v", err)
			}
		}
		if name == "token-like-value" {
			b, err := os.ReadFile("/proc/1/fd/9")
			if err != nil || string(b) != "ordinary-descriptor" {
				return fmt.Errorf("unrelated descriptor changed: %v", err)
			}
		}
	}
	processes, _ := filepath.Glob("/proc/[0-9]*")
	for _, process := range processes {
		for _, field := range []string{"environ", "cmdline"} {
			b, err := os.ReadFile(process + "/" + field)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || bytes.Contains(b, []byte(envSecret)) || bytes.Contains(b, []byte(overrideSecret)) || bytes.Contains(b, []byte(firstSecret)) {
				return fmt.Errorf("registration token exposed in %s/%s: %v", process, field, err)
			}
		}
		fds, _ := filepath.Glob(process + "/fd/*")
		for _, fd := range fds {
			target, err := os.Readlink(fd)
			if err != nil || !strings.HasPrefix(target, "/") {
				continue
			}
			info, err := os.Stat(fd)
			if err == nil && info.Mode().IsRegular() && info.Size() <= 65536 {
				b, err := os.ReadFile(fd)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				if bytes.Contains(b, []byte(envSecret)) || bytes.Contains(b, []byte(overrideSecret)) || bytes.Contains(b, []byte(firstSecret)) {
					return fmt.Errorf("registration token recoverable in %s", fd)
				}
			}
		}
	}
	if ready {
		return inspectHandoff()
	}
	return nil
}

func TestContainerPendingToken(t *testing.T) {
	name := os.Getenv("TOKEN_CASE")
	if name != "help-pending" && name != "error-pending" && name != "int-error-pending" && name != "fd-oversized" {
		t.Skip("container-only fixture")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	if name == "fd-oversized" {
		if _, err := w.WriteString(strings.Repeat("x", 4097)); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []*os.File{r, w} {
		if _, _, err := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_SETFD, 0); err != 0 {
			t.Fatal(err)
		}
	}
	entrypoint := "/usr/local/bin/buildkite-agent-entrypoint"
	flag := map[string]string{"help-pending": "--help", "error-pending": "--unknown", "int-error-pending": "--spawn=invalid", "fd-oversized": "--no-color"}[name]
	if err := syscall.Exec(entrypoint, []string{entrypoint, "start", fmt.Sprintf("--token=fd://%d", r.Fd()), flag}, os.Environ()); err != nil {
		t.Fatal(err)
	}
}
