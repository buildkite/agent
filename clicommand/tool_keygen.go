package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/buildkite/agent/v3/internal/jwkutil"
	petname "github.com/dustinkirkland/golang-petname"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/urfave/cli"
	"golang.org/x/exp/slices"
)

type ToolKeygenConfig struct {
	Alg               string `cli:"alg" validate:"required"`
	KeyID             string `cli:"key-id"`
	PrivateKeySetFile string `cli:"private-keyset-file" normalize:"filepath"`
	PublicKeysetFile  string `cli:"public-keyset-file" normalize:"filepath"`

	NoColor     bool     `cli:"no-color"`
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	Experiments []string `cli:"experiment"`
	Profile     string   `cli:"profile"`
}

// TODO: Add docs link when there is one.
var ToolKeygenCommand = cli.Command{
	Name:  "keygen",
	Usage: "Generate a new JWS key pair, used for signing and verifying jobs in Buildkite",
	Description: `Usage:

    buildkite-agent tool keygen [options...]

Description:

This (experimental!) command generates a new JWS key pair, used for signing and verifying jobs in Buildkite.
The key pair is written to two files, a private keyset and a public keyset. The private keyset should be used
as for signing, and the public for verification. The keysets are written in JWKS format.

For more information about JWS, see https://tools.ietf.org/html/rfc7515 and for information about JWKS, see https://tools.ietf.org/html/rfc7517`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:     "alg",
			EnvVar:   "BUILDKITE_AGENT_KEYGEN_ALG",
			Usage:    fmt.Sprintf("The JWS signing algorithm to use for the key pair. Valid algorithms are: %v", jwkutil.ValidSigningAlgorithms),
			Required: true,
		},
		cli.StringFlag{
			Name:   "key-id",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_KEY_ID",
			Usage:  "The ID to use for the keys generated. If none is provided, a random one will be generated",
		},
		cli.StringFlag{
			Name:   "private-keyset-file",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_PRIVATE_KEYSET_FILE",
			Usage:  "The filename to write the private key to. Defaults to a name based on the key id in the current directory",
		},
		cli.StringFlag{
			Name:   "public-keyset-file",
			EnvVar: "BUILDKITE_AGENT_KEYGEN_PUBLIC_KEYSET_FILE",
			Usage:  "The filename to write the public keyset to. Defaults to a name based on the key id in the current directory",
		},

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
	},
	Action: func(c *cli.Context) {
		_, cfg, l, _, done := setupLoggerAndConfig[ToolKeygenConfig](context.Background(), c)
		defer done()

		l.Warn("Pipeline signing is experimental and the user interface might change!")

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

		if cfg.PrivateKeySetFile == "" {
			cfg.PrivateKeySetFile = fmt.Sprintf("./%s-%s-private.json", cfg.Alg, cfg.KeyID)
		}

		if cfg.PublicKeysetFile == "" {
			cfg.PublicKeysetFile = fmt.Sprintf("./%s-%s-public.json", cfg.Alg, cfg.KeyID)
		}

		l.Info("Writing private key set to %s...", cfg.PrivateKeySetFile)
		pKey, err := json.Marshal(priv)
		if err != nil {
			l.Fatal("Failed to marshal private key: %v", err)
		}

		err = writeIfNotExists(cfg.PrivateKeySetFile, pKey)
		if err != nil {
			l.Fatal("Failed to write private key file: %v", err)
		}

		l.Info("Writing public key set to %s...", cfg.PublicKeysetFile)
		pubKey, err := json.Marshal(pub)
		if err != nil {
			l.Fatal("Failed to marshal private key: %v", err)
		}

		err = writeIfNotExists(cfg.PublicKeysetFile, pubKey)
		if err != nil {
			l.Fatal("Failed to write private key file: %v", err)
		}

		l.Info("Done! Enjoy your new keys ^_^")

		if slices.Contains(jwkutil.ValidOctetAlgorithms, sigAlg) {
			l.Info("Note: Because you're using the %s algorithm, which is symmetric, the public and private keys are identical", sigAlg)
		}
	},
}

func writeIfNotExists(filename string, data []byte) error {
	if _, err := os.Stat(filename); err == nil {
		return fmt.Errorf("file %s already exists", filename)
	}

	return os.WriteFile(filename, data, 0o600)
}
