package vmsandbox

import (
	"fmt"
	"path/filepath"

	"github.com/buildkite/agent/v4/env"
)

// The guest filesystem layout. These paths are baked into the guest image
// (see guest/Dockerfile and guest/init.sh) and are the same for every job,
// so the host can rewrite its own paths to them without asking the guest.
const (
	GuestAgentDir    = "/var/lib/buildkite-agent"
	GuestBuildPath   = GuestAgentDir + "/builds"
	GuestPluginsPath = GuestAgentDir + "/plugins"
	GuestSocketsPath = GuestAgentDir + "/sockets"
	GuestContextDir  = GuestAgentDir + "/job-context"
	GuestHooksPath   = "/etc/buildkite-agent/hooks"
)

// jobContextFileVars hold paths of files the agent creates in its job
// context directory. The runner sends their contents (or, for the timeout
// marker, creates it on demand) and the guest recreates them under
// GuestContextDir with the same base names.
var jobContextFileVars = []string{
	"BUILDKITE_ENV_FILE",
	"BUILDKITE_ENV_JSON_FILE",
	"BUILDKITE_AGENT_JOB_TIMEOUT_FILE",
}

// GuestEnv converts the bootstrap environment the agent computed for a
// local subprocess into one that makes sense inside the guest.
//
// The input is only the env the job runner computed (job env plus agent
// settings), never the agent process's own os.Environ(): the host's PATH,
// HOME and anything the operator exported (including BUILDKITE_AGENT_TOKEN)
// must not leak into the sandbox. The guest helper supplies its own process
// environment underneath the result.
//
// Host paths are rewritten to the guest layout. Settings that name host
// resources the guest can't have (extra hooks directories, git mirrors,
// the agent config file, a JWKS signing key file) are removed, and each
// removal is reported in the returned warnings so the runner can surface it
// in the job log.
func GuestEnv(hostEnv []string) (guestEnv, warnings []string) {
	e := env.FromSlice(hostEnv)

	// Belt and braces: the job runner deletes this before we're called, but
	// nothing about this function should depend on that.
	e.Remove("BUILDKITE_AGENT_TOKEN")

	e.Set("BUILDKITE_BUILD_PATH", GuestBuildPath)
	e.Set("BUILDKITE_HOOKS_PATH", GuestHooksPath)
	e.Set("BUILDKITE_PLUGINS_PATH", GuestPluginsPath)
	e.Set("BUILDKITE_SOCKETS_PATH", GuestSocketsPath)

	// These are set by the guest helper from its own process, not by us.
	e.Remove("BUILDKITE_BIN_PATH")
	e.Remove("BUILDKITE_AGENT_PID")

	for _, name := range jobContextFileVars {
		if val, has := e.Get(name); has && val != "" {
			e.Set(name, filepath.Join(GuestContextDir, filepath.Base(val)))
		}
	}

	// Host-only resources. Setting to "" rather than removing matches what
	// the agent does when the option is unset, so bootstrap sees "disabled"
	// rather than "unset" for the ones it distinguishes.
	dropToEmpty := []string{
		"BUILDKITE_ADDITIONAL_HOOKS_PATHS",
		"BUILDKITE_GIT_MIRRORS_PATH",
		"BUILDKITE_CONFIG_PATH",
	}
	for _, name := range dropToEmpty {
		if val, has := e.Get(name); has && val != "" {
			warnings = append(warnings, fmt.Sprintf("%s=%q refers to the host and is not available inside the sandbox; it has been cleared", name, val))
			e.Set(name, "")
		}
	}

	// A JWKS file path is meaningless in the guest, and without it the key ID
	// is too. Pipeline uploads from inside the sandbox can't be signed with a
	// host key file; that needs a KMS-style key (which the job would reach
	// over the network) or a key baked into the guest image.
	if val, has := e.Get("BUILDKITE_AGENT_JWKS_FILE"); has && val != "" {
		warnings = append(warnings, fmt.Sprintf("BUILDKITE_AGENT_JWKS_FILE=%q refers to the host; pipeline signing with a key file is not available inside the sandbox", val))
		e.Remove("BUILDKITE_AGENT_JWKS_FILE")
		e.Remove("BUILDKITE_AGENT_JWKS_KEY_ID")
	}

	return e.ToSlice(), warnings
}
