package clicommand

import (
  "io/ioutil"
  "os"
  "path"
  "path/filepath"
  "strings"

  "github.com/buildkite/agent/agent"
  "github.com/buildkite/agent/cliconfig"
  "github.com/buildkite/agent/logger"
  "github.com/buildkite/agent/stdin"
  "github.com/codegangsta/cli"
)

var PipelineValidateHelpDescription = `Usage:

   buildkite-agent pipeline validate <file> [arguments...]

Description:

   Allows you to validate the syntax of your YAML (recommended) or JSON
   configuration file. If no configuration file is provided, the command
   looks for the file in the following locations:

   - buildkite.yml
   - buildkite.yaml
   - buildkite.json
   - .buildkite/pipeline.yml
   - .buildkite/pipeline.yaml
   - .buildkite/pipeline.json

   You can also pipe build pipelines to the command allowing you to test
   scripts that generate dynamic pipelines.

Example:

   $ buildkite-agent pipeline validate
   $ buildkite-agent pipeline validate my-custom-pipeline.yml
   $ ./script/dynamic_step_generator | buildkite-agent pipeline validate`

type PipelineValidateConfig struct {
  FilePath         string `cli:"arg:0" label:"file paths"`
  NoColor          bool   `cli:"no-color"`
  Debug            bool   `cli:"debug"`
}

var PipelineValidateCommand = cli.Command{
  Name:        "validate",
  Usage:       "Validates the syntax of a build pipeline configuration.",
  Description: PipelineValidateHelpDescription,
  Flags: []cli.Flag{
    NoColorFlag,
    DebugFlag,
  },
  Action: func(c *cli.Context) {
    // The configuration will be loaded into this struct
    cfg := PipelineValidateConfig{}

    // Load the configuration
    loader := cliconfig.Loader{CLI: c, Config: &cfg}
    if err := loader.Load(); err != nil {
      logger.Fatal("%s", err)
    }

    // Setup the any global configuration options
    HandleGlobalFlags(cfg)

    // Find the pipeline file either from STDIN or the first
    // argument
    var input []byte
    var err error
    var filename string

    if cfg.FilePath != "" {
      logger.Info("Reading pipeline config from \"%s\"", cfg.FilePath)

      filename = filepath.Base(cfg.FilePath)
      input, err = ioutil.ReadFile(cfg.FilePath)
      if err != nil {
        logger.Fatal("Failed to read file: %s", err)
      }
    } else if stdin.IsPipe() {
      logger.Info("Reading pipeline config from STDIN")

      // Actually read the file from STDIN
      input, err = ioutil.ReadAll(os.Stdin)
      if err != nil {
        logger.Fatal("Failed to read from STDIN: %s", err)
      }
    } else {
      logger.Info("Searching for pipeline config...")

      paths := []string{
        "buildkite.yml",
        "buildkite.yaml",
        "buildkite.json",
        filepath.FromSlash(".buildkite/pipeline.yml"),
        filepath.FromSlash(".buildkite/pipeline.yaml"),
        filepath.FromSlash(".buildkite/pipeline.json"),
      }

      // Collect all the files that exist
      exists := []string{}
      for _, path := range paths {
        if _, err := os.Stat(path); err == nil {
          exists = append(exists, path)
        }
      }

      // If more than 1 of the config files exist, throw an
      // error. There can only be one!!
      if len(exists) > 1 {
        logger.Fatal("Found multiple configuration files: %s. Please only have 1 configuration file present.", strings.Join(exists, ", "))
      } else if len(exists) == 0 {
        logger.Fatal("Could not find a default pipeline configuration file. See `buildkite-agent pipeline upload --help` for more information.")
      }

      found := exists[0]

      logger.Info("Found config file \"%s\"", found)

      // Read the default file
      filename = path.Base(found)
      input, err = ioutil.ReadFile(found)
      if err != nil {
        logger.Fatal("Failed to read file \"%s\" (%s)", found, err)
      }
    }

    // Make sure the file actually has something in it
    if len(input) == 0 {
      logger.Fatal("Config file is empty")
    }

    // Parse the pipeline
    _, err = agent.PipelineParser{Filename: filename, Pipeline: input}.Parse()
    if err != nil {
      logger.Fatal("Pipeline parsing of \"%s\" failed (%s)", filename, err)
    }

    logger.Info("Successfully validated pipeline config")
  },
}
