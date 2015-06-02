package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/config"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

type ArtifactDownloadConfig struct {
	Query            string `cli:"arg:0" label:"artifact search query" validate:"required"`
	Destination      string `cli:"arg:1" label:"artifact download path" validate:"required"`
	Build            string `cli:"build" validate:"required"`
	Step             string `cli:"step"`
	Job              string `cli:"job" deprecated:"--job is deprecated. Please use --step"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

func ArtifactDownloadCommandAction(c *cli.Context) {
	// The configuration will be loaded into this struct
	cfg := ArtifactDownloadConfig{}

	// Load the configuration
	if err := config.Load(c, &cfg); err != nil {
		logger.Fatal("%s", err)
	}

	// Setup the any global configuration options
	SetupGlobalConfiguration(cfg)

	// Setup the downloader
	downloader := buildkite.ArtifactDownloader{
		API: buildkite.API{
			Endpoint: cfg.Endpoint,
			Token:    cfg.AgentAccessToken,
		},
		Query:       cfg.Query,
		Destination: cfg.Destination,
		BuildID:     cfg.Build,
		Step:        cfg.Step,
	}

	// Download the artifacts
	if err := downloader.Download(); err != nil {
		logger.Fatal("Failed to download artifacts: %s", err)
	}
}
