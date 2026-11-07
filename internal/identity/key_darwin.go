//go:build darwin && cgo && !nomackms

package identity

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>

static OSStatus verifySecureEnclaveKey(const void *hash, CFIndex length) {
	CFDataRef label = CFDataCreate(NULL, hash, length);
	const void *keys[] = {
		kSecClass, kSecAttrKeyClass, kSecAttrApplicationLabel,
		kSecUseDataProtectionKeychain, kSecReturnRef, kSecMatchLimit
	};
	const void *values[] = {
		kSecClassKey, kSecAttrKeyClassPrivate, label,
		kCFBooleanTrue, kCFBooleanTrue, kSecMatchLimitOne
	};
	CFDictionaryRef query = CFDictionaryCreate(NULL, keys, values, 6,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	SecKeyRef key = NULL;
	OSStatus status = SecItemCopyMatching(query, (CFTypeRef *)&key);
	CFRelease(query);
	CFRelease(label);
	if (status != errSecSuccess) {
		return status;
	}
	CFDictionaryRef attributes = SecKeyCopyAttributes(key);
	CFRelease(key);
	if (attributes == NULL) {
		return errSecInternalComponent;
	}
	CFTypeRef token = CFDictionaryGetValue(attributes, kSecAttrTokenID);
	int hardware = token != NULL && CFEqual(token, kSecAttrTokenIDSecureEnclave);
	CFRelease(attributes);
	return hardware ? errSecSuccess : errSecAuthFailed;
}
*/
import "C"

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/sha1" //nolint:gosec // macOS key identifiers use SHA-1, not a security proof.
	"encoding/hex"
	"fmt"
	"unsafe"

	"go.step.sm/crypto/kms/uri"
)

// secureEnclaveKeyURI checks the actual private key's token attribute, not the
// requested URI's se flag. Pin the signer to this key's application label so
// later signing cannot resolve a different key with the same human label.
func secureEnclaveKeyURI(keyURI string, publicKey crypto.PublicKey) (string, error) {
	pub, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", fmt.Errorf("host identity requires an ECDSA P-256 key")
	}
	ecdh, err := pub.ECDH()
	if err != nil {
		return "", err
	}
	// This is the Keychain's application-label format for an EC key.
	hash := sha1.Sum(ecdh.Bytes()) //nolint:gosec // Matches kSecAttrApplicationLabel.
	if status := C.verifySecureEnclaveKey(unsafe.Pointer(&hash[0]), C.CFIndex(len(hash))); status != C.errSecSuccess {
		return "", fmt.Errorf("private key is not a verified Secure Enclave key (OSStatus %d)", status)
	}
	u, err := uri.Parse(keyURI)
	if err != nil {
		return "", err
	}
	u.Values.Set("hash", hex.EncodeToString(hash[:]))
	return u.String(), nil
}
