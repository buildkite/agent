package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/agent/v4/clicommand"
	"github.com/buildkite/agent/v4/internal/registrationtoken"
	"github.com/google/go-cmp/cmp"
	"github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
)

func TestPrepare(t *testing.T) {
	flags := append(slices.Clone(clicommand.AgentStartCommand.Flags), cli.HelpFlag)
	encoded, err := registrationtoken.EncodeFlags(flags)
	if err != nil {
		t.Fatal(err)
	}
	flags, err = registrationtoken.DecodeFlags(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"literal", "duplicate", "file", "fd", "overridden-fd", "shared-env", "overridden-shared-env", "token-value", "positional", "help", "error", "int-error", "duration-error"} {
		t.Run(name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			defer func() { _ = w.Close() }()
			envFD := strconv.Itoa(int(r.Fd()))
			envRef := "fd://" + envFD
			file, err := os.CreateTemp(t.TempDir(), "token")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			if _, err := file.WriteString("dummy-cli-token"); err != nil {
				t.Fatal(err)
			}
			if _, err := file.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			fileFD := int(file.Fd())
			fileRef := fmt.Sprintf("fd://%d", fileFD)
			args := []string{"start", "--token=dummy-cli-token"}
			want, pipeExpected, closed := "dummy-cli-token", true, false
			switch name {
			case "duplicate":
				args = []string{"start", "--token=unused-token", "-token", "dummy-cli-token"}
			case "file":
				want, pipeExpected = "file://"+file.Name(), false
				args = []string{"start", "--token=unused-token", "--token=" + want}
			case "fd":
				args, closed = []string{"start", "--token=" + fileRef}, true
			case "overridden-fd":
				args, closed = []string{"start", "--token=" + fileRef, "--token=dummy-cli-token"}, true
			case "shared-env":
				args = []string{"start", "--token=" + envRef}
				want, pipeExpected = envRef, false
			case "overridden-shared-env":
				args = []string{"start", "--token=" + envRef, "--token=dummy-cli-token"}
			case "token-value":
				args = append(args, "--name", "--token="+fileRef)
			case "positional":
				args = append(args, "--", "--token="+fileRef)
			case "help", "error", "int-error", "duration-error":
				args = []string{"start", "--token=" + envRef, "--token=" + fileRef, map[string]string{"help": "--help", "error": "--unknown", "int-error": "--spawn=invalid", "duration-error": "--disconnect-after-job-timeout=invalid"}[name]}
				want, pipeExpected, closed = "", false, true
			}
			got, pipe, err := prepare(args, envFD, flags)
			if err != nil {
				t.Fatal(err)
			}
			if (pipe != nil) != pipeExpected {
				t.Fatalf("pipe present=%t; want %t", pipe != nil, pipeExpected)
			}
			if pipe != nil {
				defer func() { _ = pipe.Close() }()
				b, err := io.ReadAll(pipe)
				if err != nil || string(b) != want {
					t.Fatalf("pipe=%q, %v; want %q", b, err, want)
				}
				want = fmt.Sprintf("fd://%d", pipe.Fd())
			}
			parsed := registrationtoken.ParseArgs(args, flags)
			if diff := cmp.Diff(parsed.Replace(args, want), got); diff != "" {
				t.Fatal(diff)
			}
			if closed {
				if _, err := unix.FcntlInt(uintptr(fileFD), unix.F_GETFD, 0); err == nil && (pipe == nil || int(pipe.Fd()) != fileFD) {
					t.Fatal("CLI token file descriptor left open")
				}
			} else if _, err := file.Stat(); err != nil {
				t.Fatalf("unrelated descriptor closed: %v", err)
			}
			if _, err := w.WriteString("dummy-env-token"); err != nil {
				t.Fatalf("environment pipe closed: %v", err)
			}
			_ = w.Close()
			b, err := io.ReadAll(r)
			if err != nil || string(b) != "dummy-env-token" {
				t.Fatalf("environment pipe changed: %q, %v", b, err)
			}
			b, err = os.ReadFile(file.Name())
			if err != nil || string(b) != "dummy-cli-token" {
				t.Fatalf("operator file changed: %q, %v", b, err)
			}
		})
	}
	for _, token := range []string{strings.Repeat("x", 4097), "fd://invalid"} {
		if _, _, err := prepare([]string{"start", "--token=" + token}, "", flags); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}

func TestPreparePipeInputLimit(t *testing.T) {
	flags := append(slices.Clone(clicommand.AgentStartCommand.Flags), cli.HelpFlag)
	for _, name := range []string{"oversized", "oversized-whitespace", "boundary-eof", "delayed-producer", "help-pending"} {
		t.Run(name, func(t *testing.T) {
			fds := make([]int, 2)
			if err := unix.Pipe(fds); err != nil {
				t.Fatal(err)
			}
			w := os.NewFile(uintptr(fds[1]), "token-producer")
			defer func() { _ = w.Close() }()
			payload := strings.Repeat("x", 4096)
			switch name {
			case "oversized":
				payload += "x"
			case "oversized-whitespace":
				payload += "\n"
			case "delayed-producer":
				payload = " dummy-token\n"
			}
			if name != "delayed-producer" && name != "help-pending" {
				if _, err := w.WriteString(payload); err != nil {
					_ = unix.Close(fds[0])
					t.Fatal(err)
				}
			}
			args := []string{"start", fmt.Sprintf("--token=fd://%d", fds[0])}
			if name == "help-pending" {
				args = append(args, "--help")
			}
			type result struct {
				pipe *os.File
				err  error
			}
			done := make(chan result, 1)
			go func() {
				_, pipe, err := prepare(args, "", flags)
				done <- result{pipe, err}
			}()
			if name == "delayed-producer" || name == "boundary-eof" {
				select {
				case got := <-done:
					if got.pipe != nil {
						_ = got.pipe.Close()
					}
					t.Fatalf("returned before producer EOF: %v", got.err)
				case <-time.After(100 * time.Millisecond):
				}
				if name == "delayed-producer" {
					if _, err := w.WriteString(payload); err != nil {
						t.Fatal(err)
					}
				}
				_ = w.Close()
			}
			var got result
			select {
			case got = <-done:
			case <-time.After(2 * time.Second):
				_ = w.Close()
				got = <-done
				if got.pipe != nil {
					_ = got.pipe.Close()
				}
				t.Fatal("preparation did not finish promptly")
			}
			if got.pipe != nil {
				defer func() { _ = got.pipe.Close() }()
			}
			if strings.HasPrefix(name, "oversized") {
				if got.err == nil || !strings.Contains(got.err.Error(), "4096-byte limit") || got.pipe != nil {
					t.Fatalf("oversized input: pipe=%v, err=%v", got.pipe, got.err)
				}
			} else if name == "help-pending" {
				if got.err != nil || got.pipe != nil {
					t.Fatalf("pending help: pipe=%v, err=%v", got.pipe, got.err)
				}
			} else {
				if got.err != nil || got.pipe == nil {
					t.Fatalf("valid input: pipe=%v, err=%v", got.pipe, got.err)
				}
				b, err := io.ReadAll(got.pipe)
				if err != nil || string(b) != strings.TrimSpace(payload) {
					t.Fatalf("token=%q, err=%v", b, err)
				}
			}
		})
	}
}
