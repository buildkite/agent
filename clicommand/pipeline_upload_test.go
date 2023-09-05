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

func TestInjectAlgorithm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     PipelineUploadConfig
		want    jwa.SignatureAlgorithm
		keyFunc func(t *testing.T) jwk.Key
		keyName string
		wantErr bool
	}{
		{
			name:    "it injects the algorithm from config when there's none on the key",
			cfg:     PipelineUploadConfig{SigningAlgorithm: jwa.PS256.String()},
			keyFunc: newRSAJWK,
			keyName: "newRSAJWK",
			wantErr: false,
			want:    jwa.PS256,
		},
		{
			name:    "it uses the algorithm on the key when there's none given in the config",
			cfg:     PipelineUploadConfig{},
			keyFunc: keyPS256,
			keyName: "keyPS256",
			wantErr: false,
			want:    jwa.PS256,
		},
		{
			name:    "when both the config and the key have algorithms defined, it returns an error",
			cfg:     PipelineUploadConfig{SigningAlgorithm: jwa.PS256.String()},
			keyFunc: keyPS256,
			keyName: "keyPS256",
			want:    "",
			wantErr: true,
		},
		{
			name:    "when neither the config nor the key have algorithms defined, it returns an error",
			cfg:     PipelineUploadConfig{},
			keyFunc: newRSAJWK,
			keyName: "newRSAJWK",
			want:    "",
			wantErr: true,
		},
		{
			name:    "when the key has an algorithm that's not allowed (ie encryption and not signing), it returns an error",
			cfg:     PipelineUploadConfig{},
			keyFunc: encryptionKey,
			keyName: "encryptionKey",
			want:    "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key := tc.keyFunc(t)
			err := key.Set(jwk.KeyIDKey, tc.keyName)
			if err != nil {
				t.Fatalf("key.Set(%v, %v) error = %v", jwk.KeyIDKey, tc.keyName, err)
			}

			err = injectAlgorithm(key, tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Errorf("injectAlgorithm(%v, %q) error = nil, want error", tc.keyName, tc.cfg.SigningAlgorithm)
				}
				return
			}

			if err != nil {
				t.Errorf("injectAlgorithm(%v, %v) error = %v, want nil", tc.keyName, tc.cfg.SigningAlgorithm, err)
			}

			alg, ok := key.Get(jwk.AlgorithmKey)
			if !ok {
				t.Errorf("key.Get(%v) ok = false, want true", jwk.AlgorithmKey)
			}

			if alg != tc.want {
				t.Errorf("key.Get(%v) = %v, want %v", jwk.AlgorithmKey, alg, tc.want)
			}
		})
	}
}
