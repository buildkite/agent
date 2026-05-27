package job

// configureSSHKeyChecking sets up GIT_SSH_COMMAND with host key checking options.
// If GIT_SSH is set (a binary path), we skip configuration since we can't add flags.
// For SSH >= 7.6, uses StrictHostKeyChecking=accept-new (TOFU: accept new, reject changed).
// For older SSH, falls back to StrictHostKeyChecking=no with ephemeral known_hosts.
func (e *Executor) configureSSHKeyChecking() {
	// If GIT_SSH is set, it's a path to a binary - we can't add flags to it
	if _, hasGitSSH := e.shell.Env.Get("GIT_SSH"); hasGitSSH {
		e.shell.Commentf("GIT_SSH is set, skipping SSH host key configuration")
		return
	}

	// OpenSSH 7.6+ supports accept-new: accept new host keys, reject changed ones
	sshOptions := "-o StrictHostKeyChecking=accept-new"
	if !sshSupportsAcceptNew() {
		// Older SSH: disable host key checking entirely.
		// Use /dev/null for known_hosts to avoid polluting the user's file.
		sshOptions = "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
		e.shell.Commentf("SSH version < 7.6 detected, using StrictHostKeyChecking=no")
	}

	// Append to existing GIT_SSH_COMMAND or create new one
	existingSSHCommand, _ := e.shell.Env.Get("GIT_SSH_COMMAND")
	if existingSSHCommand == "" {
		existingSSHCommand = "ssh"
	}
	e.shell.Env.Set("GIT_SSH_COMMAND", existingSSHCommand+" "+sshOptions)
}
