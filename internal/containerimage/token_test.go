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
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	envSecret   = "a1868-dummy-environment-secret"
	firstSecret = "a1868-dummy-first-secret"
	lastSecret  = "a1868-dummy-last-secret"
)

func TestContainerRegistrationToken(t *testing.T) {
	image := os.Getenv("BUILDKITE_TEST_CONTAINER_IMAGE")
	if image == "" {
		t.Skip("set BUILDKITE_TEST_CONTAINER_IMAGE to test a built agent image")
	}
	arch := os.Getenv("BUILDKITE_TEST_CONTAINER_ARCH")
	if arch == "" {
		arch = runtime.GOARCH
	}
	dir := t.TempDir()
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
if [ "$TOKEN_CASE" = generated-file ]; then printf '%s' a1868-dummy-last-secret > /tmp/managed-token; fi
/fixture -test.run '^TestContainerTokenFixture$' &
while [ ! -f /tmp/listening ]; do sleep 0.05; done
`
	if err := os.WriteFile(filepath.Join(startup, "fixture"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"environment", "space", "equals", "duplicates", "file", "generated-file", "fd", "env-fd", "overridden-fd", "overridden-arg-fd", "empty", "bad-fd", "missing-file", "startup-failure", "rejected", "help", "root-help", "version", "unknown", "root-help-false", "root-terminator", "root-version-false", "root-aliases", "root-version-alias", "root-help-overridden", "start-help-false", "file-terminator", "plain-terminator", "token-like-value", "help-like-value", "help-bad-fd", "help-pending"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
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
				"run", "-d", "--network=none", "--platform=linux/" + arch,
				"-v", fixture + ":/fixture:ro", "-v", startup + ":/docker-entrypoint.d:ro",
				"-e", "TOKEN_CASE=" + name, "-e", "BUILDKITE_AGENT_TOKEN=" + envSecret,
				"-e", "SSH_CONFIG=Host example.invalid", "-e", "BUILDKITE_AGENT_ENDPOINT=http://127.0.0.1:18080",
				"-e", "BUILDKITE_AGENT_PING_MODE=poll-only", "-e", "BUILDKITE_AGENT_NO_HTTP2=true",
			}
			if binary := os.Getenv("BUILDKITE_TEST_CONTAINER_BINARY"); binary != "" {
				args = append(args, "-v", binary+":/usr/local/bin/buildkite-agent:ro")
			}
			if entrypoint := os.Getenv("BUILDKITE_TEST_CONTAINER_ENTRYPOINT"); entrypoint != "" {
				args = append(args, "-v", entrypoint+":/usr/local/bin/buildkite-agent-entrypoint:ro")
			}
			command := []string{"start", "--name=container-token-test", "--no-color"}
			success := true
			switch name {
			case "root-help-false":
				command = append([]string{"--help=false"}, command...)
			case "root-terminator":
				command = append([]string{"--"}, command...)
			case "root-version-false":
				command = append([]string{"--version=false"}, command...)
				success = false
			case "root-aliases":
				command = append([]string{"-h=false"}, command...)
			case "root-version-alias":
				command = append([]string{"-v=false"}, command...)
				success = false
			case "root-help-overridden":
				command = append([]string{"--help=true", "-h=false"}, command...)
			case "start-help-false":
				command = append(command, "--help=true", "-h=false")
			case "plain-terminator":
				command = append(command, "--token="+lastSecret, "--", "--token=fd://99")
			case "help-like-value":
				command = append(command, "--token="+lastSecret, "--name", "--help")
			case "token-like-value":
				args = append(args, "--entrypoint=/bin/sh")
				command = append(command, "--token="+lastSecret, "--name", "--token=fd://9")
				command = append([]string{"-c", "printf ordinary-descriptor > /tmp/non-token; exec 9</tmp/non-token; rm /tmp/non-token; exec /usr/local/bin/buildkite-agent-entrypoint \"$@\"", "input"}, command...)
			case "space":
				command = append(command, "--token", lastSecret)
			case "equals":
				command = append(command, "--token="+lastSecret)
			case "duplicates":
				command = append(command, "--token", firstSecret, "-token="+lastSecret)
			case "file", "file-terminator", "fd", "env-fd", "overridden-fd", "overridden-arg-fd":
				args = append(args, "--entrypoint=/bin/sh")
				setup := "printf '%s' " + lastSecret + " > /tmp/managed-token; "
				switch name {
				case "file-terminator":
					setup += "unset BUILDKITE_AGENT_TOKEN; "
					command = append(command, "--token=file:///tmp/managed-token", "--", "--token=fd://99")
				case "file":
					command = append(command, "--token="+firstSecret, "--token=file:///tmp/managed-token")
				default:
					setup += "exec 9</tmp/managed-token; rm /tmp/managed-token; "
					switch name {
					case "fd":
						command = append(command, "--token=fd://9")
					case "env-fd":
						setup += "export BUILDKITE_AGENT_TOKEN=fd://9; "
					case "overridden-arg-fd":
						command = append(command, "--token=fd://9", "--token="+lastSecret)
					default:
						setup += "export BUILDKITE_AGENT_TOKEN=fd://9; "
						command = append(command, "--token="+firstSecret, "--token="+lastSecret)
					}
				}
				command = append([]string{"-c", setup + "exec /usr/local/bin/buildkite-agent-entrypoint \"$@\"", "input"}, command...)
			case "generated-file":
				args = append(args, "-e", "BUILDKITE_AGENT_TOKEN=file:///tmp/managed-token")
			case "empty":
				command = append(command, "--token=")
				success = false
			case "bad-fd":
				command = append(command, "--token=fd://99")
				success = false
			case "missing-file":
				command = append(command, "--token=file:///missing")
				success = false
			case "startup-failure", "rejected":
				success = false
			case "help":
				command = []string{"start", "--help"}
				success = false
			case "help-bad-fd":
				command = []string{"start", "--help", "--token=fd://99"}
				success = false
			case "help-pending":
				args = append(args, "--entrypoint=/fixture")
				command = []string{"-test.run", "^TestContainerPendingToken$"}
				success = false
			case "root-help":
				command = []string{"--help"}
				success = false
			case "version":
				command = []string{"--version"}
				success = false
			case "unknown":
				command = []string{"does-not-exist"}
				success = false
			}
			id := docker(append(append(args, image), command...)...)
			t.Cleanup(func() {
				if t.Failed() {
					out, _ := exec.Command("docker", "logs", id).CombinedOutput()
					t.Logf("container logs:\n%s", out)
				}
				_ = exec.Command("docker", "rm", "-f", "-v", id).Run()
			})
			if success {
				for {
					logs := docker("logs", id)
					if strings.Contains(logs, "fixture: checked") {
						break
					}
					if strings.Contains(logs, "panic") {
						t.Fatal("fixture rejected container state")
					}
					if ctx.Err() != nil || docker("inspect", "-f", "{{.State.Running}}", id) != "true" {
						t.Fatal("container did not reach a clean authenticated ping")
					}
					time.Sleep(100 * time.Millisecond)
				}
				docker("kill", "--signal=TERM", id)
			}
			exit := docker("wait", id)
			wantZero := success || name == "help" || name == "help-bad-fd" || name == "help-pending" || name == "root-help" || name == "version" || name == "root-version-false" || name == "root-version-alias"
			if (exit == "0") != wantZero {
				t.Fatalf("exit code %s; want success=%t", exit, wantZero)
			}
			if success && !strings.Contains(docker("logs", id), "fixture: disconnected") {
				t.Fatal("SIGTERM did not reach the agent for a graceful disconnect")
			}
			if name == "root-help" && strings.Contains(docker("logs", id), "internal-container-launch") {
				t.Fatal("internal launcher exposed in public help")
			}
			if name == "rejected" && !strings.Contains(docker("logs", id), "fixture: rejected cleanly") {
				t.Fatal("registration rejection did not leave a clean startup state")
			}
			if name == "help-bad-fd" || name == "help-pending" {
				logs := docker("logs", id)
				if !strings.Contains(logs, "Options:") || strings.Contains(logs, "Registering agent") {
					t.Fatal("expected help without registration")
				}
			}
		})
	}
}

func TestContainerPendingToken(t *testing.T) {
	if os.Getenv("TOKEN_CASE") != "help-pending" {
		t.Skip("container-only fixture")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	for _, file := range []*os.File{r, w} {
		if _, _, err := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_SETFD, 0); err != 0 {
			t.Fatal(err)
		}
	}
	entrypoint := "/usr/local/bin/buildkite-agent-entrypoint"
	if err := syscall.Exec(entrypoint, []string{entrypoint, "start", "--help", fmt.Sprintf("--token=fd://%d", r.Fd())}, os.Environ()); err != nil {
		t.Fatal(err)
	}
}

func TestContainerTokenFixture(t *testing.T) {
	name := os.Getenv("TOKEN_CASE")
	if name == "" {
		t.Skip("container-only fixture")
	}
	want := lastSecret
	if name == "environment" || name == "rejected" || strings.HasPrefix(name, "root-") || name == "start-help-false" {
		want = envSecret
	}
	orphan, err := exec.Command("/bin/sh", "-c", "sleep 0.1 >/dev/null 2>&1 & echo $!").Output()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:18080")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/tmp/listening", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/register":
			if name == "token-like-value" || name == "help-like-value" {
				wantName := "--token=fd://9"
				if name == "help-like-value" {
					wantName = "--help"
				}
				var registration struct{ Name string }
				if err := json.NewDecoder(r.Body).Decode(&registration); err != nil || registration.Name != wantName {
					panic("non-token flag value was changed")
				}
			}
			if r.Header.Get("Authorization") != "Token "+want {
				panic("registration token precedence mismatch")
			}
			if name == "rejected" {
				if err := inspectContainer(name); err != nil {
					panic(err)
				}
				fmt.Println("fixture: rejected cleanly")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = fmt.Fprint(w, `{"message":"dummy registration rejected"}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"id":"test-agent","name":"test","access_token":"dummy-session-token","endpoint":"http://127.0.0.1:18080","ping_interval":1,"heartbeat_interval":60,"job_status_interval":1}`)
		case "/ping":
			if r.Header.Get("Authorization") != "Token dummy-session-token" {
				panic("ping did not use session token")
			}
			if err := inspectContainer(name); err != nil {
				panic(err)
			}
			if _, err := os.Stat("/proc/" + strings.TrimSpace(string(orphan))); err == nil {
				_, _ = fmt.Fprint(w, `{}`)
				return
			}
			if err := os.WriteFile("/tmp/checked", nil, 0o600); err != nil {
				panic(err)
			}
			fmt.Println("fixture: checked")
			_, _ = fmt.Fprint(w, `{}`)
		case "/disconnect":
			fmt.Println("fixture: disconnected")
			_, _ = fmt.Fprint(w, `{}`)
		default:
			_, _ = fmt.Fprint(w, `{}`)
		}
	})}
	defer func() { _ = server.Close() }()
	if err := server.Serve(listener); err != nil {
		t.Fatal(err)
	}
}

func inspectContainer(name string) error {
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
		if err != nil || string(b) != lastSecret {
			return fmt.Errorf("user-managed file was changed: %v", err)
		}
	} else if _, err := os.Stat("/tmp/managed-token"); !os.IsNotExist(err) {
		return fmt.Errorf("ephemeral token file remains: %v", err)
	}
	if name == "token-like-value" {
		b, err := os.ReadFile("/proc/1/fd/9")
		if err != nil || string(b) != "ordinary-descriptor" {
			return fmt.Errorf("non-token descriptor was closed or changed: %v", err)
		}
	}
	if name == "file-terminator" || name == "plain-terminator" {
		b, err := os.ReadFile("/proc/1/cmdline")
		if err != nil || !bytes.Contains(b, []byte("--\x00--token=fd://99\x00")) {
			return fmt.Errorf("positional token-looking text was changed: %v", err)
		}
	}
	processes, err := filepath.Glob("/proc/[0-9]*")
	if err != nil {
		return err
	}
	for _, process := range processes {
		var state []byte
		for _, field := range []string{"environ", "cmdline"} {
			b, err := os.ReadFile(filepath.Join(process, field))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			state = append(state, b...)
			for _, secret := range []string{envSecret, firstSecret, lastSecret} {
				if bytes.Contains(b, []byte(secret)) {
					return fmt.Errorf("plaintext registration token in %s/%s", process, field)
				}
			}
		}
		fds, _ := filepath.Glob(process + "/fd/*")
		for _, fd := range fds {
			target, err := os.Readlink(fd)
			if err != nil {
				continue
			}
			info, err := os.Stat(fd)
			if err != nil {
				continue
			}
			if strings.HasPrefix(target, "/") && info.Mode().IsRegular() && info.Size() <= 64*1024 {
				b, err := os.ReadFile(fd)
				if err != nil {
					return err
				}
				for _, secret := range []string{envSecret, firstSecret, lastSecret} {
					if bytes.Contains(b, []byte(secret)) {
						return fmt.Errorf("recoverable token in regular descriptor %s", fd)
					}
				}
			}
			if !bytes.Contains(state, []byte("fd://"+filepath.Base(fd)+"\x00")) {
				continue
			}
			if name == "token-like-value" && filepath.Base(fd) == "9" {
				continue
			}
			if process != "/proc/1" && info.Mode()&os.ModeNamedPipe == 0 {
				continue
			}
			n, err := strconv.Atoi(filepath.Base(fd))
			if err != nil || n < 3 || info.Mode()&os.ModeNamedPipe == 0 {
				return fmt.Errorf("handoff %s is not a pipe", fd)
			}
			handle, err := syscall.Open(fd, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
			if err != nil {
				return err
			}
			b := make([]byte, 65536)
			count, readErr := syscall.Read(handle, b)
			_ = syscall.Close(handle)
			if count != 0 || readErr != nil {
				return fmt.Errorf("handoff %s not drained and closed: bytes=%d, err=%v", fd, count, readErr)
			}
		}
	}
	return nil
}
