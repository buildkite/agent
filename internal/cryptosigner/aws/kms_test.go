package awssigner_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	awssigner "github.com/buildkite/agent/v4/internal/cryptosigner/aws"
	"github.com/buildkite/go-pipeline"
	"github.com/buildkite/go-pipeline/signature"
	"github.com/lestrrat-go/jwx/v3/jws"
)

func TestKMSSignedStepKeyID(t *testing.T) {
	const (
		id       = "1234abcd-12ab-34cd-56ef-1234567890ab"
		arn      = "arn:aws:kms:us-east-1:111122223333:key/" + id
		otherARN = "arn:aws:kms:us-east-1:111122223333:key/abcdef12-1234-abcd-5678-abcd12345678"
	)
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, input, signingARN string }{
		{"key ID", id, arn},
		{"alias", "alias/builds", arn},
		{"alias moved before signing", "alias/builds", otherARN},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					KeyID   string
					Message []byte
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.KeyID != tc.input {
					http.Error(w, "expected configured key ID or alias", http.StatusBadRequest)
					return
				}
				var response any
				switch r.Header.Get("X-Amz-Target") {
				case "TrentService.GetPublicKey":
					response = kms.GetPublicKeyOutput{KeyId: aws.String(arn), KeyUsage: "SIGN_VERIFY", KeySpec: "ECC_NIST_P256", PublicKey: der}
				case "TrentService.Sign":
					sig, err := ecdsa.SignASN1(rand.Reader, priv, req.Message)
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					response = kms.SignOutput{KeyId: aws.String(tc.signingARN), Signature: sig}
				default:
					http.NotFound(w, r)
					return
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			signer, err := awssigner.NewKMS(kms.New(kms.Options{
				Region: "us-east-1", Credentials: aws.AnonymousCredentials{}, BaseEndpoint: aws.String(server.URL),
			}), tc.input)
			if err != nil {
				t.Fatal(err)
			}
			step := &pipeline.CommandStep{Command: "echo hello"}
			err = signature.SignSteps(t.Context(), pipeline.Steps{step}, signer, "repo")
			if tc.signingARN != arn {
				if err == nil || step.Signature != nil {
					t.Errorf("alias moved: got signature %v, error %v; want no signature and an error", step.Signature, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			msg, err := jws.ParseString(step.Signature.Value)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := msg.Signatures()[0].ProtectedHeaders().KeyID(); got != arn {
				t.Errorf("protected kid = %q, want %q", got, arn)
			}
		})
	}
}
