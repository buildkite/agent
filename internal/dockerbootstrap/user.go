package dockerbootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func containerUser(value string) (int, int, error) {
	if value == "" {
		return os.Getuid(), os.Getgid(), nil
	}
	user, group, ok := strings.Cut(value, ":")
	if !ok {
		return 0, 0, fmt.Errorf("user must be a numeric UID:GID")
	}
	uid, err := parseUserID(user)
	if err != nil {
		return 0, 0, err
	}
	gid, err := parseUserID(group)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func parseUserID(value string) (int, error) {
	if value == "" || strings.Trim(value, "0123456789") != "" {
		return 0, fmt.Errorf("user must be a numeric UID:GID")
	}
	id, err := strconv.ParseUint(value, 10, 31)
	if err != nil {
		return 0, fmt.Errorf("user IDs must be between 0 and 2147483647")
	}
	return int(id), nil
}

func prepareUser(user, contextDir string) (string, int, int, error) {
	uid, gid, err := containerUser(user)
	if err != nil {
		return "", 0, 0, err
	}
	if err := grantContextAccess(contextDir, uid, gid); err != nil {
		return "", 0, 0, err
	}
	directory, err := os.MkdirTemp(contextDir, "identity-")
	if err != nil {
		return "", 0, 0, err
	}
	return directory, uid, gid, nil
}

func grantContextAccess(contextDir string, uid, gid int) error {
	if uid == 0 || uid == os.Getuid() {
		return nil
	}
	if os.Getuid() != 0 {
		return fmt.Errorf("a different non-root container UID requires a root host agent to grant private context access")
	}
	entries, err := os.ReadDir(contextDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(contextDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("private context must contain only regular coordination files")
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("grant container access to coordination file: %w", err)
		}
	}
	if err := os.Chown(contextDir, uid, gid); err != nil {
		return fmt.Errorf("grant container access to private context: %w", err)
	}
	return nil
}

// mergeIdentity keeps the image's own users and groups, and makes sure the
// container UID and GID resolve. The UID's home directory is pointed at the
// private home, because ssh reads ~/.ssh from the passwd entry rather than $HOME.
func mergeIdentity(passwd, group string, uid, gid int) (string, string) {
	home := containerRoot + "/home"
	lines := strings.Split(strings.TrimSuffix(passwd, "\n"), "\n")
	found := false
	used := map[string]bool{}
	for i, line := range lines {
		fields := strings.Split(line, ":")
		if len(fields) != 7 {
			continue
		}
		used[fields[0]] = true
		if !found && fields[2] == strconv.Itoa(uid) {
			found = true
			fields[5] = home
			lines[i] = strings.Join(fields, ":")
		}
	}
	if !found {
		lines = append(lines, fmt.Sprintf("%s:x:%d:%d:Buildkite:%s:/bin/sh", unusedName(used, uid), uid, gid, home))
	}
	passwd = strings.TrimPrefix(strings.Join(lines, "\n")+"\n", "\n")

	used = map[string]bool{}
	for _, line := range strings.Split(group, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 4 {
			continue
		}
		used[fields[0]] = true
		if fields[2] == strconv.Itoa(gid) {
			return passwd, group
		}
	}
	if group != "" && !strings.HasSuffix(group, "\n") {
		group += "\n"
	}
	return passwd, group + fmt.Sprintf("%s:x:%d:\n", unusedName(used, gid), gid)
}

func unusedName(used map[string]bool, id int) string {
	if !used["buildkite"] {
		return "buildkite"
	}
	return fmt.Sprintf("buildkite-%d", id)
}
