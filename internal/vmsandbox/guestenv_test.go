package vmsandbox

import (
	"strings"
	"testing"

	"github.com/buildkite/agent/v4/env"
	"github.com/google/go-cmp/cmp"
)

func TestGuestEnv(t *testing.T) {
	t.Parallel()

	hostEnv := []string{
		"BUILDKITE_AGENT_TOKEN=registration-token-must-not-leak",
		"BUILDKITE_AGENT_ACCESS_TOKEN=job-scoped-token",
		"BUILDKITE_BUILD_PATH=/var/lib/host-agent/builds",
		"BUILDKITE_HOOKS_PATH=/etc/host/hooks",
		"BUILDKITE_PLUGINS_PATH=/var/lib/host-agent/plugins",
		"BUILDKITE_SOCKETS_PATH=/var/lib/host-agent/sockets",
		"BUILDKITE_BIN_PATH=/usr/local/host/bin",
		"BUILDKITE_AGENT_PID=4242",
		"BUILDKITE_ENV_FILE=/tmp/job-env/job-env-abc",
		"BUILDKITE_ENV_JSON_FILE=/tmp/job-env/job-env-abc.json",
		"BUILDKITE_AGENT_JOB_TIMEOUT_FILE=/tmp/job-env/job-timeout-abc",
		"BUILDKITE_ADDITIONAL_HOOKS_PATHS=/opt/extra-hooks",
		"BUILDKITE_GIT_MIRRORS_PATH=/var/lib/mirrors",
		"BUILDKITE_CONFIG_PATH=/etc/buildkite-agent/buildkite-agent.cfg",
		"BUILDKITE_AGENT_JWKS_FILE=/etc/keys/jwks.json",
		"BUILDKITE_AGENT_JWKS_KEY_ID=key-1",
		"BUILDKITE_COMMAND=echo one\necho two",
		"BUILDKITE_MESSAGE=multi\nline\nmessage",
	}

	got, warnings := GuestEnv(hostEnv)
	e := env.FromSlice(got)

	want := map[string]string{
		"BUILDKITE_AGENT_ACCESS_TOKEN":     "job-scoped-token",
		"BUILDKITE_BUILD_PATH":             GuestBuildPath,
		"BUILDKITE_HOOKS_PATH":             GuestHooksPath,
		"BUILDKITE_PLUGINS_PATH":           GuestPluginsPath,
		"BUILDKITE_SOCKETS_PATH":           GuestSocketsPath,
		"BUILDKITE_ENV_FILE":               GuestContextDir + "/job-env-abc",
		"BUILDKITE_ENV_JSON_FILE":          GuestContextDir + "/job-env-abc.json",
		"BUILDKITE_AGENT_JOB_TIMEOUT_FILE": GuestContextDir + "/job-timeout-abc",
		"BUILDKITE_ADDITIONAL_HOOKS_PATHS": "",
		"BUILDKITE_GIT_MIRRORS_PATH":       "",
		"BUILDKITE_CONFIG_PATH":            "",
		"BUILDKITE_COMMAND":                "echo one\necho two",
		"BUILDKITE_MESSAGE":                "multi\nline\nmessage",
	}
	if diff := cmp.Diff(want, e.Dump()); diff != "" {
		t.Errorf("GuestEnv(hostEnv) diff (-want +got):\n%s", diff)
	}

	// Each cleared host-only setting is reported exactly once, and the
	// registration token's removal is silent (it should never have been
	// there; there's nothing for the user to act on).
	wantWarnPrefixes := []string{
		"BUILDKITE_ADDITIONAL_HOOKS_PATHS=",
		"BUILDKITE_GIT_MIRRORS_PATH=",
		"BUILDKITE_CONFIG_PATH=",
		"BUILDKITE_AGENT_JWKS_FILE=",
	}
	if len(warnings) != len(wantWarnPrefixes) {
		t.Fatalf("got %d warnings, want %d: %q", len(warnings), len(wantWarnPrefixes), warnings)
	}
	for i, prefix := range wantWarnPrefixes {
		if !strings.HasPrefix(warnings[i], prefix) {
			t.Errorf("warnings[%d] = %q, want prefix %q", i, warnings[i], prefix)
		}
	}
	for _, w := range warnings {
		if strings.Contains(w, "token") {
			t.Errorf("warning mentions a token: %q", w)
		}
	}
}

func TestGuestEnv_UnsetOptionalVarsStayUnset(t *testing.T) {
	t.Parallel()

	// A job without env files or optional host settings must not gain
	// empty-string versions of them: bootstrap distinguishes unset from "".
	got, warnings := GuestEnv([]string{"BUILDKITE_BUILD_PATH=/x"})
	e := env.FromSlice(got)
	for _, name := range []string{
		"BUILDKITE_ENV_FILE", "BUILDKITE_ADDITIONAL_HOOKS_PATHS", "BUILDKITE_AGENT_JWKS_FILE",
		"BUILDKITE_BIN_PATH", "BUILDKITE_AGENT_PID", "BUILDKITE_AGENT_TOKEN",
	} {
		if v, has := e.Get(name); has {
			t.Errorf("%s=%q is set, want unset", name, v)
		}
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
}

func TestParseTap(t *testing.T) {
	t.Parallel()

	tap, err := ParseTap("bko-tap0:172.16.0.1/30:172.16.0.2")
	if err != nil {
		t.Fatalf("ParseTap error = %v", err)
	}
	if tap.Device != "bko-tap0" || tap.HostAddr.String() != "172.16.0.1/30" || tap.GuestIP.String() != "172.16.0.2" {
		t.Errorf("ParseTap = %+v", tap)
	}

	for _, bad := range []string{
		"bko-tap0:172.16.0.1/30",            // missing guest
		":172.16.0.1/30:172.16.0.2",         // empty device
		"bko-tap0:172.16.0.1:172.16.0.2",    // host missing prefix length
		"bko-tap0:172.16.0.1/30:172.16.0.5", // guest outside the subnet
		"bko-tap0:172.16.0.1/30:not-an-ip",
	} {
		if _, err := ParseTap(bad); err == nil {
			t.Errorf("ParseTap(%q) succeeded, want error", bad)
		}
	}
}
