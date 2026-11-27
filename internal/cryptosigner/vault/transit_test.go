package vaultsigner_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	vaultsigner "github.com/buildkite/agent/v4/internal/cryptosigner/vault"
	"github.com/buildkite/go-pipeline"
	"github.com/buildkite/go-pipeline/signature"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

func TestTransitSignerVerifiesWithPublicJWKS(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	wrongVersionDigest := sha256.Sum256([]byte("Vault returns a different version"))
	wrongSignatureDigest := sha256.Sum256([]byte("Vault signs different data"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/ci/transit/keys/pipeline":
			_, _ = fmt.Fprintf(w, `{"data":{"keys":{"3":{"public_key":%q}},"latest_version":4}}`, publicPEM)
		case "PUT /v1/ci/transit/sign/pipeline":
			var req struct {
				Input      string
				KeyVersion int    `json:"key_version"`
				Hash       string `json:"hash_algorithm"`
				Marshaling string `json:"marshaling_algorithm"`
				Prehashed  bool   `json:"prehashed"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			digest, err := base64.StdEncoding.DecodeString(req.Input)
			if err != nil || len(digest) != sha256.Size || req.KeyVersion != 3 || req.Hash != "sha2-256" || req.Marshaling != "asn1" || !req.Prehashed {
				t.Errorf("unexpected Vault sign request: %+v (decode error: %v)", req, err)
				http.Error(w, "invalid sign request", http.StatusBadRequest)
				return
			}
			if req.Input == base64.StdEncoding.EncodeToString(wrongSignatureDigest[:]) {
				digest = wrongVersionDigest[:]
			}
			sig, err := ecdsa.SignASN1(rand.Reader, key, digest)
			if err != nil {
				t.Error(err)
				http.Error(w, "signing failed", http.StatusBadRequest)
				return
			}
			version := 3
			if req.Input == base64.StdEncoding.EncodeToString(wrongVersionDigest[:]) {
				version = 4
			}
			_, _ = fmt.Fprintf(w, `{"data":{"signature":"vault:v%d:%s"}}`, version, base64.StdEncoding.EncodeToString(sig))
		default:
			t.Errorf("unexpected Vault request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("VAULT_ADDR", server.URL)
	t.Setenv("VAULT_AGENT_ADDR", "")
	t.Setenv("VAULT_PROXY_ADDR", "")
	t.Setenv("VAULT_HTTP_PROXY", "")
	t.Setenv("VAULT_TOKEN", "test-token")
	signer, err := vaultsigner.New(t.Context(), "ci/transit/keys/pipeline@3")
	if err != nil {
		t.Fatal(err)
	}

	const repo = "https://example.test/repo.git"
	step := &pipeline.CommandStep{Command: "echo hello"}
	if err := signature.SignSteps(t.Context(), pipeline.Steps{step}, signer, repo); err != nil {
		t.Fatal(err)
	}
	public, err := jwk.Import(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := jwk.AssignKeyID(public); err != nil {
		t.Fatal(err)
	}
	if err := public.Set(jwk.AlgorithmKey, jwa.ES256()); err != nil {
		t.Fatal(err)
	}
	keys := jwk.NewSet()
	if err := keys.AddKey(public); err != nil {
		t.Fatal(err)
	}
	if err := signature.VerifyStep(t.Context(), step, keys, repo); err != nil {
		t.Fatalf("VerifyStep with public JWKS: %v", err)
	}
	if _, err := signer.Sign(nil, wrongVersionDigest[:], crypto.SHA256); err == nil || !strings.Contains(err.Error(), "not for key version 3") {
		t.Fatalf("Sign with a mismatched signature version: got %v, want version error", err)
	}
	if sig, err := signer.Sign(nil, wrongSignatureDigest[:], crypto.SHA256); sig != nil || err == nil || !strings.Contains(err.Error(), "invalid Vault Transit signature") {
		t.Fatalf("Sign with a valid signature for different data: got (%x, %v), want no signature and an invalid signature error", sig, err)
	}
}
