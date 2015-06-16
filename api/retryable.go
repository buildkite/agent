package api

import (
	"io"
	"net"
	"net/url"
	"strings"
	"syscall"
)

var retrableErrorSuffixes = []string{
	syscall.ECONNREFUSED.Error(),
	syscall.ECONNRESET.Error(),
	syscall.ETIMEDOUT.Error(),
	"no such host",
	"remote error: handshake failure",
	io.ErrUnexpectedEOF.Error(),
	io.EOF.Error(),
}

// Looks at a bunch of connection related errors, and returns true if the error
// matches one of them.
func IsRetryableError(err error) bool {
	if neterr, ok := err.(net.Error); ok {
		if neterr.Temporary() {
			return true
		}
	}

	if neterr, ok := err.(net.Error); ok && neterr.Timeout() {
		return true
	}

	if urlerr, ok := err.(*url.Error); ok {
		if strings.Contains(urlerr.Error(), "use of closed network connection") {
			return true
		}

		if neturlerr, ok := urlerr.Err.(net.Error); ok && neturlerr.Timeout() {
			return true
		}
	}

	if strings.Contains(err.Error(), "request canceled while waiting for connection") {
		return true
	}

	s := err.Error()
	for _, suffix := range retrableErrorSuffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}

	return false
}
