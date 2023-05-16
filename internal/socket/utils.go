package socket

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// socketExists returns true if the socket path exists on linux and darwin
// on windows it always returns false, because of https://github.com/golang/go/issues/33357 (stat on sockets is broken on windows)
func socketExists(path string) (bool, error) {
	if runtime.GOOS == "windows" {
		return false, nil
	}

	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("stat socket: %w", err)
	}

	return true, nil
}

// socketPathLength returns the maximum path length a socket can be named for
// the current GOOS.
func socketPathLength() int {
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris":
		return 104
	case "linux":
		fallthrough
	default:
		return 108
	}
}

// GenerateToken generates a new random token that contains approximately
// 8*len bits of entropy.
func GenerateToken(len int) (string, error) {
	b := make([]byte, len)
	_, err := rand.Read(b)
	if err != nil {
		return "", fmt.Errorf("reading from random: %w", err)
	}

	withEqualses := base64.URLEncoding.EncodeToString(b)
	return strings.TrimRight(withEqualses, "="), nil // Trim the equals signs because they're not valid in env vars
}

// WriteError writes an error as an ErrorResponse (JSON-encoded). The err value
// is converted to a string with fmt.Sprint.
func WriteError(w http.ResponseWriter, err any, code int) error {
	w.WriteHeader(code)
	return json.NewEncoder(w).Encode(ErrorResponse{Error: fmt.Sprint(err)})
}
