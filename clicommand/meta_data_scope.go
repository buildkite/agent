package clicommand

import (
	"errors"

	"github.com/urfave/cli/v3"
)

func metaDataScopeFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "scope",
		Value:   "build",
		Usage:   "Store or read meta-data on the 'build' or 'job'. Job scope requires --job and cannot be combined with --build",
		Sources: cli.EnvVars("BUILDKITE_METADATA_SCOPE"),
	}
}

func metaDataRequestScope(scope, build string) (string, error) {
	switch scope {
	case "build":
		// Omit build scope to preserve compatibility with older Agent APIs.
		return "", nil
	case "job":
		if build != "" {
			return "", errors.New("--scope job cannot be used with --build (including BUILDKITE_METADATA_BUILD_ID); unset the build selector to select a job")
		}
		return "job", nil
	default:
		return "", errors.New("scope must be build or job")
	}
}
