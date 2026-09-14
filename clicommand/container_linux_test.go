package clicommand

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestContainerRegistrationToken(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		env  string
		want string
	}{
		{"environment", []string{"start"}, "env-secret", "env-secret"},
		{"flag", []string{"start", "--token", "flag-secret"}, "env-secret", "flag-secret"},
		{"duplicates", []string{"start", "--token=first-secret", "-token", "last-secret"}, "env-secret", "last-secret"},
		{"empty flag", []string{"start", "--token="}, "env-secret", ""},
		{"unset", []string{"start"}, "", ""},
		{"file", []string{"start", "--token=first-secret", "--token=file:///managed"}, "env-secret", "file:///managed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			args, env, pipe, err := containerRegistrationToken(test.args, []string{registrationTokenEnvVar + "=" + test.env, "OTHER=value"})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(env, "OTHER=value") {
				t.Fatal("unrelated environment removed")
			}
			if strings.Contains(strings.Join(append(args, env...), " "), "secret") {
				t.Fatal("plaintext token remains")
			}
			if pipe == nil {
				if test.want != "" && !slices.Contains(args, "--token="+test.want) {
					t.Fatalf("missing file reference: %v", args)
				}
				return
			}
			defer func() { _ = pipe.Close() }()
			parent, err := unix.Dup(int(pipe.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			f := os.NewFile(uintptr(parent), "parent")
			defer func() { _ = f.Close() }()
			got, err := io.ReadAll(pipe)
			if err != nil || string(got) != test.want {
				t.Fatalf("read = %q, %v; want %q", got, err, test.want)
			}
			got, err = io.ReadAll(f)
			if err != nil || len(got) != 0 {
				t.Fatalf("parent retained token: %q, %v", got, err)
			}
		})
	}
}

func TestContainerRegistrationTokenOversize(t *testing.T) {
	_, _, pipe, err := containerRegistrationToken([]string{"start"}, []string{registrationTokenEnvVar + "=" + strings.Repeat("x", 2*1024*1024)})
	if pipe != nil {
		_ = pipe.Close()
	}
	if err == nil {
		t.Fatal("oversize token should fail rather than block")
	}
}

func TestContainerRegistrationTokenHelpDoesNotRead(t *testing.T) {
	for _, argument := range []string{"pending", "fd://99"} {
		t.Run(argument, func(t *testing.T) {
			fds := make([]int, 2)
			if err := unix.Pipe2(fds, unix.O_CLOEXEC); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = unix.Close(fds[0]) }()
			defer func() { _ = unix.Close(fds[1]) }()
			ref := fmt.Sprintf("fd://%d", fds[0])
			if argument == "pending" {
				argument = ref
			}
			args, env, pipe, err := containerRegistrationToken([]string{"start", "--help", "--token=" + argument}, []string{registrationTokenEnvVar + "=" + ref})
			if err != nil || pipe != nil {
				t.Fatalf("help handoff = %v, %v", pipe, err)
			}
			if !slices.Equal(args, []string{"start", "--help", "--token="}) || len(env) != 0 {
				t.Fatalf("help args/env = %v / %v", args, env)
			}
			if _, err := unix.FcntlInt(uintptr(fds[0]), unix.F_GETFD, 0); err != unix.EBADF {
				t.Fatalf("read descriptor not closed: %v", err)
			}
		})
	}
}
