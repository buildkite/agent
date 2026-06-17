package osutil

import "os"

// Real umask set by init func in umask_unix.go. 0o022 is a common default.
var Umask = os.FileMode(0o022)
