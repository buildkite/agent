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
	directory, err := createIdentityDirectory(contextDir, uid, gid)
	if err != nil {
		return directory, 0, 0, err
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

func createIdentityDirectory(contextDir string, uid, gid int) (string, error) {
	directory, err := os.MkdirTemp(contextDir, "identity-")
	if err != nil {
		return "", err
	}
	if err := writeIdentityFiles(directory, uid, gid); err != nil {
		_ = os.RemoveAll(directory)
		return directory, err
	}
	return directory, nil
}

func writeIdentityFiles(directory string, uid, gid int) error {
	rootGID := 0
	if uid == 0 {
		rootGID = gid
	}
	passwd := fmt.Sprintf("root:x:0:%d:root:%s/home:/bin/sh\n", rootGID, containerRoot)
	group := "root:x:0:\n"
	if uid != 0 {
		passwd += fmt.Sprintf("buildkite:x:%d:%d:Buildkite:%s/home:/bin/sh\n", uid, gid, containerRoot)
	}
	if gid != 0 {
		group += fmt.Sprintf("buildkite:x:%d:\n", gid)
	}
	for name, contents := range map[string]string{"passwd": passwd, "group": group} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return err
		}
		// Lookup files must remain readable under restrictive host umasks.
		if err := os.Chmod(path, 0o644); err != nil {
			return err
		}
	}
	return nil
}
