//go:build !darwin || !cgo || nomackms

package identity

import (
	"crypto"
	"fmt"
)

func secureEnclaveKeyURI(string, crypto.PublicKey) (string, error) {
	return "", fmt.Errorf("secure enclave verification requires a macOS CGO build with mackms enabled")
}
