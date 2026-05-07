package job

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/bintest/v3"
)

func init() {
	sshKeyscanRetryInterval = time.Millisecond
}

func TestFindingSSHTools(t *testing.T) {
	t.Parallel()

	sh, err := shell.New()
	if err != nil {
		t.Fatalf("shell.New() error = %v", err)
	}

	sh.Logger = shell.TestingLogger{T: t}

	if _, err := findPathToSSHTools(context.Background(), sh); err != nil {
		t.Errorf("findPathToSSHTools(sh) error = %v", err)
	}
}

func TestSSHKeyscanReturnsOutput(t *testing.T) {
	t.Parallel()

	sh := shell.NewTestShell(t)

	keyScan, err := bintest.NewMock("ssh-keyscan")
	if err != nil {
		t.Fatalf("bintest.NewMock(ssh-keyscan) error = %v", err)
	}
	defer keyScan.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	sh.Env.Set("PATH", filepath.Dir(keyScan.Path))

	keyScan.
		Expect("github.com").
		AndWriteToStdout("github.com ssh-rsa xxx=").
		AndExitWith(0)

	keyScanOutput, err := sshKeyScan(context.Background(), sh, "github.com")

	if got, want := keyScanOutput, "github.com ssh-rsa xxx="; got != want {
		t.Errorf("sshKeyScan(context.Background(), sh, %q) = %q, want %q", "github.com", got, want)
	}
	if err != nil {
		t.Errorf("sshKeyScan(context.Background(), sh, %q) error = %v, want nil", "github.com", err)
	}
}

func TestSSHKeyscanWithHostAndPortReturnsOutput(t *testing.T) {
	t.Parallel()

	sh := shell.NewTestShell(t)

	keyScan, err := bintest.NewMock("ssh-keyscan")
	if err != nil {
		t.Fatalf("bintest.NewMock(ssh-keyscan) error = %v", err)
	}
	defer keyScan.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	sh.Env.Set("PATH", filepath.Dir(keyScan.Path))

	keyScan.
		Expect("-p", "123", "github.com").
		AndWriteToStdout("github.com ssh-rsa xxx=").
		AndExitWith(0)

	keyScanOutput, err := sshKeyScan(context.Background(), sh, "github.com:123")

	if got, want := keyScanOutput, "github.com ssh-rsa xxx="; got != want {
		t.Errorf("sshKeyScan(context.Background(), sh, %q) = %q, want %q", "github.com:123", got, want)
	}
	if err != nil {
		t.Errorf("sshKeyScan(context.Background(), sh, %q) error = %v, want nil", "github.com:123", err)
	}
}

func TestSSHKeyscanRetriesOnExit1(t *testing.T) {
	t.Parallel()

	sh := shell.NewTestShell(t)

	keyScan, err := bintest.NewMock("ssh-keyscan")
	if err != nil {
		t.Fatalf("bintest.NewMock(ssh-keyscan) error = %v", err)
	}
	defer keyScan.CheckAndClose(t) //nolint:errcheck // bintest logs to t

	sh.Env.Set("PATH", filepath.Dir(keyScan.Path))

	keyScan.
		Expect("github.com").
		AndWriteToStderr("it failed").
		Exactly(3).
		AndExitWith(1)

	keyScanOutput, err := sshKeyScan(context.Background(), sh, "github.com")

	if got, want := keyScanOutput, ""; got != want {
		t.Errorf("sshKeyScan(context.Background(), sh, %q) = %q, want %q", "github.com", got, want)
	}
	if want := "`ssh-keyscan \"github.com\"` failed"; err == nil || err.Error() != want {
		t.Errorf("sshKeyScan(context.Background(), sh, %q) error = %v, want error with message %q", "github.com", err, want)
	}
}

func TestSSHKeyscanRetriesOnBlankOutputAndExit0(t *testing.T) {
	t.Parallel()

	sh := shell.NewTestShell(t)

	keyScan, err := bintest.NewMock("ssh-keyscan")
	if err != nil {
		t.Fatalf("bintest.NewMock(ssh-keyscan) error = %v", err)
	}
	t.Cleanup(func() {
		if err := keyScan.CheckAndClose(t); err != nil {
			t.Errorf("keyScan.CheckAndClose(t) = %v", err)
		}
	})
	//nolint:errcheck // bintest logs to t
	sh.Env.Set("PATH", filepath.Dir(keyScan.Path))

	keyScan.
		Expect("github.com").
		AndWriteToStdout("").
		Exactly(3).
		AndExitWith(0)

	keyScanOutput, err := sshKeyScan(context.Background(), sh, "github.com")

	if got, want := keyScanOutput, ""; got != want {
		t.Errorf("sshKeyScan(context.Background(), sh, %q) = %q, want %q", "github.com", got, want)
	}
	if want := "`ssh-keyscan \"github.com\"` returned nothing"; err == nil || err.Error() != want {
		t.Errorf("sshKeyScan(context.Background(), sh, %q) error = %v, want error with message %q", "github.com", err, want)
	}
}
