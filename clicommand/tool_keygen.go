package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	"github.com/buildkite/go-pipeline/jwkutil"
	petname "github.com/dustinkirkland/golang-petname"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/urfave/cli"
)

type ToolKeygenConfig struct {
	GlobalConfig

	Alg             string `cli:"alg"`
	KeyID           string `cli:"key-id"`
	PrivateJWKSFile string `cli:"private-jwks-file" normalize:"filepath"`
	PublicJWKSFile  string `cli:"public-jwks-file" normalize:"filepath"`
}

// TODO: Add docs link when there is one.
var ToolKeygenCommand = cli.Command{
	Name:  "keygen",
	Usage: "Generate a new JWS key pair, used for signing and verifying jobs in Buildkite",
	Description: `Usage:

    buildkite-agent tool keygen [options...]

Description:

This command generates a new JWS key pair, used for signing and verifying jobs
in Buildkite.

The pair is written as a JSON Web Key Set (JWKS) to two files, a private JWKS
file and a public JWKS file. The private JWKS should be used as for signing,
and the public JWKS for verification.

For more information about JWS, see https://tools.ietf.org/html/rfc7515 and
for information about JWKS, see https://tools.ietf.org/html/rfc7517`,
	Flags: append(globalFlags(),
		cli.StringFlag{
			Name:   "alg",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_ALG",
			Usage:  fmt.Sprintf("The JWS signing algorithm to use for the key pair. Defaults to 'EdDSA'. Valid algorithms are: %v", jwkutil.ValidSigningAlgorithms),
		},
		cli.StringFlag{
			Name:   "key-id",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_KEY_ID",
			Usage:  "The ID to use for the keys generated. If none is provided, a random one will be generated",
		},
		cli.StringFlag{
			Name:   "private-jwks-file",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_PRIVATE_JWKS_FILE",
			Usage:  "The filename to write the private key to. Defaults to a name based on the key id in the current directory",
		},
		cli.StringFlag{
			Name:   "public-jwks-file",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_PUBLIC_JWKS_FILE",
			Usage:  "The filename to write the public keyset to. Defaults to a name based on the key id in the current directory",
		},
	),
	Action: func(c *cli.Context) {
		_, cfg, l, _, done := setupLoggerAndConfig[ToolKeygenConfig](context.Background(), c)
		defer done()

		if cfg.Alg == "" {
			cfg.Alg = "EdDSA"
			l.Info("No algorithm provided, using %s", cfg.Alg)
		}

		if cfg.KeyID == "" {
			cfg.KeyID = petname.Generate(2, "-")
			l.Info("No key ID provided, using a randomly generated one: %s", cfg.KeyID)
		}

		sigAlg := jwa.SignatureAlgorithm(cfg.Alg)

		if !slices.Contains(jwkutil.ValidSigningAlgorithms, sigAlg) {
			l.Fatal("Invalid signing algorithm: %s. Valid signing algorithms are: %s", cfg.Alg, jwkutil.ValidSigningAlgorithms)
		}

		priv, pub, err := jwkutil.NewKeyPair(cfg.KeyID, sigAlg)
		if err != nil {
			l.Fatal("Failed to generate key pair: %v", err)
		}

		if cfg.PrivateJWKSFile == "" {
			cfg.PrivateJWKSFile = fmt.Sprintf("./%s-%s-private.json", cfg.Alg, cfg.KeyID)
		}

		if cfg.PublicJWKSFile == "" {
			cfg.PublicJWKSFile = fmt.Sprintf("./%s-%s-public.json", cfg.Alg, cfg.KeyID)
		}

		l.Info("Writing private key set to %s...", cfg.PrivateJWKSFile)
		pKey, err := json.Marshal(priv)
		if err != nil {
			l.Fatal("Failed to marshal private key: %v", err)
		}

		err = writeIfNotExists(cfg.PrivateJWKSFile, pKey)
		if err != nil {
			l.Fatal("Failed to write private key file: %v", err)
		}

		l.Info("Writing public key set to %s...", cfg.PublicJWKSFile)
		pubKey, err := json.Marshal(pub)
		if err != nil {
			l.Fatal("Failed to marshal private key: %v", err)
		}

		err = writeIfNotExists(cfg.PublicJWKSFile, pubKey)
		if err != nil {
			l.Fatal("Failed to write private key file: %v", err)
		}

		l.Info("Done! Enjoy your new keys ^_^")
	},
}

func writeIfNotExists(filename string, data []byte) error {
	if _, err := os.Stat(filename); err == nil {
		return fmt.Errorf("file %s already exists", filename)
	}

	return os.WriteFile(filename, data, 0o600)
}
