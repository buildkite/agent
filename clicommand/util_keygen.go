package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/internal/jwkutil"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/urfave/cli"
	"golang.org/x/exp/slices"
)

type KeygenConfig struct {
	Alg                  string `cli:"alg" validate:"required"`
	KeyID                string `cli:"key-id" validate:"required"`
	PrivateKeyFilename   string `cli:"private-key-filename" normalize:"filepath"`
	PublicKeysetFilename string `cli:"public-keyset-filename" normalize:"filepath"`

	LogLevel string `cli:"log-level"`
	Debug    bool   `cli:"debug"`
}

var KeygenCommand = cli.Command{
	Name:  "keygen",
	Usage: "Generate a new key pair, used to sign and verify jobs",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "alg",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_ALG",
			Usage:  fmt.Sprintf("The JWS signing algorithm to use for the key pair. Valid algorithms are: %v", ValidSigningAlgorithms),
		},
		cli.StringFlag{
			Name:   "key-id",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_KEY_ID",
			Usage:  "The ID to use for the keys generated",
		},
		cli.StringFlag{
			Name:   "private-key-filename",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_PRIVATE_KEY_FILENAME",
			Usage:  "The filename to write the private key to. Defaults to a name based on the key id in the current directory",
		},
		cli.StringFlag{
			Name:   "public-keyset-filename",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_PUBLIC_KEYSET_FILENAME",
			Usage:  "The filename to write the public keyset to. Defaults to a name based on the key id in the current directory",
		},

		DebugFlag,
		LogLevelFlag,
	},
	Action: func(c *cli.Context) {
		ctx := context.Background()
		_, cfg, l, _, done := setupLoggerAndConfig[KeygenConfig](ctx, c)
		defer done()

		sigAlg := jwa.SignatureAlgorithm(cfg.Alg)

		if !slices.Contains(ValidSigningAlgorithms, sigAlg) {
			l.Fatal("Invalid signing algorithm: %s. Valid signing algorithms are: %s", cfg.Alg, ValidSigningAlgorithms)
		}

		priv, pub, err := jwkutil.NewKeyPair(cfg.KeyID, sigAlg)
		if err != nil {
			l.Fatal("Failed to generate key pair: %v", err)
		}

		if cfg.PrivateKeyFilename == "" {
			cfg.PrivateKeyFilename = fmt.Sprintf("./%s-%s-private.json", cfg.Alg, cfg.KeyID)
		}

		if cfg.PublicKeysetFilename == "" {
			cfg.PublicKeysetFilename = fmt.Sprintf("./%s-%s-public.json", cfg.Alg, cfg.KeyID)
		}

		privFile, err := os.Create(cfg.PrivateKeyFilename)
		if err != nil {
			l.Fatal("Failed to open private key file: %v", err)
		}

		defer privFile.Close()

		err = json.NewEncoder(privFile).Encode(priv)
		if err != nil {
			l.Fatal("Failed to encode private key file: %v", err)
		}

		l.Info("Wrote private key to %s", cfg.PrivateKeyFilename)

		pubFile, err := os.Create(cfg.PublicKeysetFilename)
		if err != nil {
			l.Fatal("Failed to open public key file: %v", err)
		}

		defer pubFile.Close()

		err = json.NewEncoder(pubFile).Encode(pub)
		if err != nil {
			l.Fatal("Failed to encode public key file: %v", err)
		}

		l.Info("Wrote public key set to %s", cfg.PublicKeysetFilename)

		l.Info("Done! Enjoy your new keys ^_^")

		if slices.Contains(ValidOctetAlgorithms, sigAlg) {
			l.Info(`Note: Because you're using the %s algorithm, which is symmetric, the public and private keys are identical, save for the fact that the "public" key has been output as a Key Set, rather than a single key.`, sigAlg)
		}
	},
}
