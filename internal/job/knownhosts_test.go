package job

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/gliderlabs/ssh"
)

func TestAddingToKnownHosts(t *testing.T) {
	t.Parallel()

	// Start a fake SSH server for ssh-keyscan to poke at.
	// It will generate its own host key.
	// This uses ':0' to pick an ephemeral port to listen on.
	svr := &ssh.Server{
		Addr:    "localhost:0",
		Version: "Fake SSH Server v0.1",
	}
	ln, err := net.Listen("tcp", svr.Addr)
	if err != nil {
		t.Fatalf("net.Listen(tcp, %q) error = %v", svr.Addr, err)
	}
	go svr.Serve(ln)
	defer svr.Close()

	hostAddr := ln.Addr().String()
	repoURL := fmt.Sprintf("ssh://git@%s/var/cache/git/project.git", hostAddr)
	t.Logf("Fake SSH server listening at address %s", hostAddr)

	// Create a new empty known-hosts file to add to.
	f, err := os.CreateTemp("", "known-hosts")
	if err != nil {
		t.Fatalf(`os.CreateTemp("", "known-hosts") error = %v`, err)
	}
	t.Cleanup(func() {
		os.RemoveAll(f.Name()) //nolint:errcheck // Best-effort cleanup.
	})
	if err := f.Close(); err != nil {
		t.Fatalf("f.Close() = %v", err)
	}

	sh := shell.NewTestShell(t)
	sh.Env.Set("PATH", os.Getenv("PATH"))
	kh := knownHosts{
		Shell: sh,
		Path:  f.Name(),
	}

	// The file should not contain this host
	exists, err := kh.Contains(hostAddr)
	if err != nil {
		t.Errorf("kh.Contains(%q) error = %v", hostAddr, err)
	}
	if got, want := exists, false; got != want {
		t.Errorf("kh.Contains(%q) = %t, want %t", hostAddr, got, want)
	}

	// Add the host - this resolves any SSH client configuration
	// (in case the URL contains an alias), and runs ssh-keyscan to get its key.
	if err := kh.AddFromRepository(context.Background(), repoURL); err != nil {
		t.Errorf("kh.AddFromRespository(%q) = %v", repoURL, err)
	}

	// The file should now contain the host key
	exists, err = kh.Contains(hostAddr)
	if err != nil {
		t.Errorf("kh.Contains(%q) error = %v", hostAddr, err)
	}
	if got, want := exists, true; got != want {
		t.Errorf("kh.Contains(%q) = %t, want %t", hostAddr, got, want)
	}
}
