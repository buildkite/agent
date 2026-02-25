package job

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/buildkite/agent/v3/internal/shell"
	"github.com/buildkite/bintest/v3"
	"github.com/stretchr/testify/assert"
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

	assert.Equal(t, keyScanOutput, "github.com ssh-rsa xxx=")
	assert.NoError(t, err)
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

	assert.Equal(t, keyScanOutput, "github.com ssh-rsa xxx=")
	assert.NoError(t, err)
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

	assert.Equal(t, keyScanOutput, "")
	assert.EqualError(t, err, "`ssh-keyscan \"github.com\"` failed")
}

func TestSSHKeyscanRetriesOnBlankOutputAndExit0(t *testing.T) {
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
		AndWriteToStdout("").
		Exactly(3).
		AndExitWith(0)

	keyScanOutput, err := sshKeyScan(context.Background(), sh, "github.com")

	assert.Equal(t, keyScanOutput, "")
	assert.EqualError(t, err, "`ssh-keyscan \"github.com\"` returned nothing")
}
