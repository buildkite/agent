package job

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/buildkite/agent/v4/internal/shell"
	"github.com/buildkite/bintest/v3"
)

func TestConfigureSSHKeyChecking(t *testing.T) {
	for _, tc := range []struct {
		name        string
		installed   bool
		version     string
		exitCode    int
		acceptNew   bool
		wantOptions string
		wantWarning bool
	}{
		{name: "missing SSH", acceptNew: true, wantOptions: "-o StrictHostKeyChecking=accept-new"},
		{name: "missing SSH with strict checking", wantOptions: "-o StrictHostKeyChecking=yes"},
		{name: "modern SSH", installed: true, version: "OpenSSH_7.6p1", acceptNew: true, wantOptions: "-o StrictHostKeyChecking=accept-new"},
		{name: "old SSH", installed: true, version: "OpenSSH_7.5p1", acceptNew: true, wantOptions: "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"},
		{name: "unknown SSH", installed: true, version: "unknown", acceptNew: true, wantOptions: "-o StrictHostKeyChecking=accept-new", wantWarning: true},
		{name: "broken SSH", installed: true, exitCode: 1, acceptNew: true, wantOptions: "-o StrictHostKeyChecking=accept-new", wantWarning: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pathDir := t.TempDir()
			if tc.installed {
				ssh, err := bintest.NewMock(filepath.Join(pathDir, "ssh"))
				if err != nil {
					t.Fatal(err)
				}
				defer ssh.Close() //nolint:errcheck // Best-effort cleanup.
				ssh.Expect("-V").AndWriteToStderr(tc.version).AndExitWith(tc.exitCode)
			}
			t.Setenv("PATH", pathDir)
			var logs bytes.Buffer
			e := &Executor{shell: shell.NewTestShell(t,
				shell.WithEnv(env.New()),
				shell.WithLogger(shell.NewWriterLogger(&logs, false, nil)),
			)}

			e.configureSSHKeyChecking(tc.acceptNew)

			if got, _ := e.shell.Env.Get("GIT_SSH_COMMAND"); got != "ssh "+tc.wantOptions {
				t.Errorf("GIT_SSH_COMMAND = %q, want %q", got, "ssh "+tc.wantOptions)
			}
			if got := strings.Contains(logs.String(), "Failed to check SSH version"); got != tc.wantWarning {
				t.Errorf("SSH version warning = %v, want %v; logs: %s", got, tc.wantWarning, &logs)
			}
		})
	}
}
