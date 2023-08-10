package pipeline

// import (
// 	"testing"

// 	"github.com/lestrrat-go/jwx/v2/jwk"
// )

// func newSymmetricKeyPair(t *testing.T, key string) (jwk.Key, jwk.Set) {
// 	t.Helper()
// 	k, err := jwk.FromRaw([]byte(key))
// 	if err != nil {
// 		t.Fatalf("failed to create symmetric key: %s", err)
// 	}

// 	err = k.Set(jwk.AlgorithmKey, "HS256")
// 	if err != nil {
// 		t.Fatalf("failed to set algorithm: %s", err)
// 	}

// 	k.Set(jwk.KeyIDKey, "test-key")
// 	if err != nil {
// 		t.Fatalf("failed to set key id: %s", err)
// 	}

// 	set := jwk.NewSet()
// 	err = set.AddKey(k)
// 	if err != nil {
// 		t.Fatalf("failed to add key to set: %s", err)
// 	}

// 	return k, set
// }
