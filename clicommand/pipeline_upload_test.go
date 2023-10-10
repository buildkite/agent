package clicommand

import (
	"testing"

	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/logger"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"gotest.tools/v3/assert"
)

func TestSearchForSecrets(t *testing.T) {
	t.Parallel()

	cfg := &PipelineUploadConfig{
		RedactedVars:  []string{"SEKRET", "SSH_KEY"},
		RejectSecrets: true,
	}

	p := &pipeline.Pipeline{
		Steps: pipeline.Steps{
			&pipeline.CommandStep{
				Command: "secret squirrels and alpacas",
			},
		},
	}

	tests := []struct {
		desc    string
		environ map[string]string
		wantLog string
	}{
		{
			desc:    "no secret",
			environ: map[string]string{"SEKRET": "llamas", "UNRELATED": "horses"},
			wantLog: "",
		},
		{
			desc:    "one secret",
			environ: map[string]string{"SEKRET": "squirrel", "PYTHON": "not a chance"},
			wantLog: `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET], and cannot be uploaded to Buildkite`,
		},
		{
			desc:    "two secrets",
			environ: map[string]string{"SEKRET": "squirrel", "SSH_KEY": "alpacas", "SPECIES": "Felix sylvestris"},
			wantLog: `pipeline "cat-o-matic.yaml" contains values interpolated from the following secret environment variables: [SEKRET SSH_KEY], and cannot be uploaded to Buildkite`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()
			l := logger.NewBuffer()
			err := searchForSecrets(l, cfg, test.environ, p, "cat-o-matic.yaml")
			if len(test.wantLog) == 0 {
				assert.NilError(t, err)
				return
			}
			assert.ErrorContains(t, err, test.wantLog)
		})
	}
}

func TestValidateJWKDisallows(t *testing.T) {
	t.Parallel()

	globallyDisallowed := []jwa.SignatureAlgorithm{"", "none", "foo", "bar", "baz"}

	cases := []struct {
		name           string
		key            jwk.Key
		allowedAlgs    []jwa.SignatureAlgorithm
		disallowedAlgs []jwa.SignatureAlgorithm
	}{
		{
			name:        "RSA only allows PS256, PS384, PS512",
			key:         newRSAJWK(t),
			allowedAlgs: ValidRSAAlgorithms,
			disallowedAlgs: concat(
				[]jwa.SignatureAlgorithm{jwa.RS256, jwa.RS384, jwa.RS512}, // Specific to RSA, these are possible but we choose not to support them
				globallyDisallowed,
				ValidECAlgorithms,
				ValidOKPAlgorithms,
				ValidOctetAlgorithms,
			),
		},
		{
			name:        "EC only allows ES256, ES384, ES512",
			key:         newECJWK(t),
			allowedAlgs: ValidECAlgorithms,
			disallowedAlgs: concat(
				globallyDisallowed,
				ValidRSAAlgorithms,
				ValidOKPAlgorithms,
				ValidOctetAlgorithms,
			),
		},
		{
			name:        "OKP only allows EdDSA",
			key:         newOKPJWK(t),
			allowedAlgs: ValidOKPAlgorithms,
			disallowedAlgs: concat(
				globallyDisallowed,
				ValidRSAAlgorithms,
				ValidECAlgorithms,
				ValidOctetAlgorithms,
			),
		},
		{
			name:        "Octet only allows HS256, HS384, HS512",
			key:         newOctetSeqJWK(t),
			allowedAlgs: ValidOctetAlgorithms,
			disallowedAlgs: concat(
				globallyDisallowed,
				ValidRSAAlgorithms,
				ValidECAlgorithms,
				ValidOKPAlgorithms,
			),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, alg := range tc.allowedAlgs {
				err := tc.key.Set(jwk.AlgorithmKey, alg)
				if err != nil {
					t.Fatalf("key.Set(%v, %v) error = %v", jwk.AlgorithmKey, alg, err)
				}

				err = validateJWK(tc.key)
				if err != nil {
					t.Errorf("validateJWK({keyType: %s, alg: %s}) error = %v", tc.key.KeyType(), tc.key.Algorithm(), err)
				}
			}

			for _, alg := range tc.disallowedAlgs {
				err := tc.key.Set(jwk.AlgorithmKey, alg)
				if err != nil {
					t.Fatalf("key.Set(%v, %v) error = %v", jwk.AlgorithmKey, alg, err)
				}

				err = validateJWK(tc.key)
				if err == nil {
					t.Errorf("validateJWK({keyType: %s, alg: %s}) expected error, got nil", tc.key.KeyType(), tc.key.Algorithm())
				}
			}
		})
	}
}
