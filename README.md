# Buildkite Agent

[![Build status](https://badge.buildkite.com/08e4e12a0a1e478f0994eb1e8d51822c5c74d395.svg?branch=main)]()
[![Go Reference](https://pkg.go.dev/badge/github.com/buildkite/agent/v3.svg)](https://pkg.go.dev/github.com/buildkite/agent/v3)

The buildkite-agent is a small, reliable, and cross-platform build runner that makes it easy to run automated builds on your own infrastructure. It’s main responsibilities are polling [buildkite.com](https://buildkite.com/) for work, running build jobs, reporting back the status code and output log of the job, and uploading the job's artifacts.

Full documentation is available at [buildkite.com/docs/agent](https://buildkite.com/docs/agent)

```
$ buildkite-agent --help
Usage:

  buildkite-agent <command> [options...]

Available commands are:

  start      Starts a Buildkite agent
  annotate   Annotate the build page within the Buildkite UI with text from within a Buildkite job
  artifact   Upload/download artifacts from Buildkite jobs
  meta-data  Get/set data from Buildkite jobs
  pipeline   Make changes to the pipeline of the currently running build
  step       Make changes to a step (this includes any jobs that were created from the step)
  bootstrap  [DEPRECATED] Run a Buildkite job locally
  job        Interact with Buildkite jobs
  help       Shows a list of commands or help for one command

Use "buildkite-agent <command> --help" for more information about a command.
```

## Dependencies

The agent is fairly portable and should run out of the box on most supported platforms without extras. On Linux hosts it requires `dbus`.

## Installing

[The agents page](https://buildkite.com/organizations/-/agents) on Buildkite has personalised instructions, or you can refer to [the Buildkite docs](https://buildkite.com/docs/agent/v3/installation). Both cover installing the agent with Ubuntu (via apt), Debian (via apt), macOS (via homebrew), Windows and Linux.

### Docker

We also support and publish [Docker Images](https://hub.docker.com/r/buildkite/agent) for the
following operating systems. Docker images are tagged using the agent SemVer components followed
by the operating system.

For example, agent version 3.30.0 is published as:

- 3-ubuntu-20.04, tracks minor and bugfix updates in version 3 installed in Ubuntu 20.04
- 3.30-ubuntu-20.04, tracks bugfix updates in version 3.30 installed in Ubuntu 20.04
- 3.30.0-ubuntu-20.04, tracks the exact version installed in Ubuntu 20.04

#### Supported operating systems

- Alpine 3.12
- Ubuntu 18.04 LTS (x86_64), supported to end of life for 18.04
- Ubuntu 20.04 LTS (x86_64), supported to end of life for 20.04
- Ubuntu 22.04 LTS (x86_64), supported to end of life for 22.04

## Starting

To start an agent all you need is your agent token, which you can find on your Agents page within Buildkite.

```bash
buildkite-agent start --token
```

### Telemetry

By default, the agent sends some information back to the Buildkite mothership on what features are in use on that agent. Nothing sensitive or identifying is sent back to Buildkite, but if you want, you can disable this feature reporting by adding the `--no-feature-reporting` flag to your `buildkite-agent start` call. A full list of the features that we track can be found [here](https://github.com/buildkite/agent/blob/03aec39f97fe7d20936e6af63cd793a87fac2c19/clicommand/agent_start.go#L135).

## Development

These instructions assume you are running a recent macOS, but could easily be adapted to Linux and Windows.

```bash
# Make sure you have go 1.11+ installed.
brew install go

# Download the code somewhere, no GOPATH required
git clone https://github.com/buildkite/agent.git
cd agent

# Create a temporary builds directory
mkdir /tmp/buildkite-builds

# Build an agent binary and start the agent
go build -o /usr/local/bin/buildkite-agent .
buildkite-agent start --debug --build-path=/tmp/buildkite-builds --token "abc"

# Or, run the agent directly and skip the build step
go run *.go start --debug --build-path=/tmp/buildkite-builds --token "abc"
```

### Dependency management

We're using Go 1.18+ and [Go Modules](https://github.com/golang/go/wiki/Modules) to manage our Go dependencies.

Dependencies are no longer committed to the repository, so compiling on Go <= 1.10 is not supported.

### Go Version

The agent is compiled using Go 1.18. Previous go versions may work, but are not guaranteed to.

## Platform Support

We provide support for security and bug fixes on the current major release only.

Our architecture and operating system support is primarily limited by what golang
itself [supports](https://github.com/golang/go/wiki/MinimumRequirements).

### Architecture Support

We offer support for the following machine architectures (inspired by the Rust language platform
support guidance).

#### Tier 1, guaranteed to work

- linux x86_64
- linux arm64
- windows x86_64

#### Tier 2, guaranteed to build

- linux x86
- windows x86
- darwin x86_64
- darwin arm64

#### Tier 3, community supported

We release binaries for various other platforms, and it should be possible to build it anywhere Go compiles, but official support is not provided for these Tier 3 platforms.

### Operating System Support

We currently provide support for running the Buildkite Agent on the following operating
systems. Future major releases may drop support for old operating systems. The agent
binary is fairly portable and should run out of the box on most UNIX like systems.

- Ubuntu 18.04 and newer
- Debian 8 and newer
- Red Hat RHEL 7 and newer
- CentOS
  - CentOS 7
  - CentOS 8
- Amazon Linux 2
- macOS [1]
  - 10.12
  - 10.13
  - 10.14
  - 10.15
  - 11
- Windows Server
  - 2012
  - 2016
  - 2019

[1] See https://github.com/golang/go/issues/23011 for macOS / golang support and
[Supported macOS Versions](./docs/macos.md) for the last supported version of the
Buildkite Agent for versions of macOS prior to those listed above.

## Contributing

See [./CONTRIBUTING.md](./CONTRIBUTING.md)

## Contributors

Many thanks to our fine contributors! A full list can be found [here](https://github.com/buildkite/agent/graphs/contributors), but you're all amazing, and we greatly appreciate your input ❤️

## Copyright

Copyright (c) 2014-2019 Buildkite Pty Ltd. See [LICENSE](./LICENSE.txt) for details.
