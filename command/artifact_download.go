package command

import (
	"github.com/buildkite/agent/buildkite"
	"github.com/buildkite/agent/buildkite/config"
	"github.com/buildkite/agent/buildkite/logger"
	"github.com/codegangsta/cli"
)

type ArtifactDownloadConfiguration struct {
	Query            string `cli:"arg:0" label:"query" validate:"required"`
	Destination      string `cli:"arg:1" label:"download path" validate:"required"`
	Build            string `cli:"build" validate:"required"`
	Step             string `cli:"step"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

func ArtifactDownloadCommandAction(c *cli.Context) {
	cfg := ArtifactDownloadConfiguration{}
	config.Loader{Context: c, Configuration: &cfg}.Load()

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

	err := downloader.Download()
	if err != nil {
		logger.Fatal("Failed to download artifacts: %s", err)
	}
}
