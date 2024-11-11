# Buildkite Agent

![Build status](https://badge.buildkite.com/08e4e12a0a1e478f0994eb1e8d51822c5c74d395.svg?branch=main)
[![Go Reference](https://pkg.go.dev/badge/github.com/buildkite/agent/v3.svg)](https://pkg.go.dev/github.com/buildkite/agent/v3)

The buildkite-agent is a small, reliable, and cross-platform build runner that
makes it easy to run automated builds on your own infrastructure. It’s main
responsibilities are polling [buildkite.com](https://buildkite.com/) for work,
running build jobs, reporting back the status code and output log of the job,
and uploading the job's artifacts.

Full documentation is available at
[buildkite.com/docs/agent](https://buildkite.com/docs/agent).

```text
$ buildkite-agent --help
Usage:

  buildkite-agent <command> [options...]

Available commands are:

  acknowledgements  Prints the licenses and notices of open source software incorporated into this software.
  start             Starts a Buildkite agent
  annotate          Annotate the build page within the Buildkite UI with text from within a Buildkite job
  annotation        Make changes to an annotation on the currently running build
  artifact          Upload/download artifacts from Buildkite jobs
  env               Process environment subcommands
  lock              Process lock subcommands
  meta-data         Get/set data from Buildkite jobs
  oidc              Interact with Buildkite OpenID Connect (OIDC)
  pipeline          Make changes to the pipeline of the currently running build
  step              Get or update an attribute of a build step
  bootstrap         Run a Buildkite job locally
  help              Shows a list of commands or help for one command

Use "buildkite-agent <command> --help" for more information about a command.
```

## Dependencies

The agent is fairly portable and should run out of the box on most supported
platforms without extras. On Linux hosts it requires `dbus`.

## Installing

[The agents page](https://buildkite.com/organizations/-/agents) on Buildkite has
personalised instructions, or you can refer to
[the Buildkite docs](https://buildkite.com/docs/agent/v3/installation). Both
cover installing the agent with Ubuntu (via apt), Debian (via apt), macOS (via
homebrew), Windows and Linux.

### Docker

We also support and publish
[Docker Images](https://hub.docker.com/r/buildkite/agent) for the following
operating systems. Docker images are tagged using the agent SemVer components
followed by the operating system.

For example, agent version 3.45.6 is published as:

- 3-ubuntu-20.04, tracks minor and bugfix updates in version 3 installed in
  Ubuntu 20.04
- 3.45-ubuntu-20.04, tracks bugfix updates in version 3.45 installed in Ubuntu
  20.04
- 3.45.6-ubuntu-20.04, tracks the exact version installed in Ubuntu 20.04

#### Supported operating systems

- Alpine 3.18
- Ubuntu 20.04 LTS (x86_64), supported to end of standard support for 20.04
- Ubuntu 22.04 LTS (x86_64), supported to end of standard support for 22.04
- Ubuntu 24.04 LTS (x86_64), supported to end of standard support for 24.04

## Starting

To start an agent all you need is your agent token, which you can find on your
Agents page within Buildkite, and a build path. For example:

```bash
buildkite-agent start --token=<your token> --build-path=/tmp/buildkite-builds
```

### Telemetry

By default, the agent sends some information back to the Buildkite mothership on
what features are in use on that agent. Nothing sensitive or identifying is sent
back to Buildkite, but if you want, you can disable this feature reporting by
adding the `--no-feature-reporting` flag to your `buildkite-agent start` call.
Features that we track can be found inside
[AgentStartConfig.Features](https://github.com/search?q=repo%3Abuildkite%2Fagent+language%3Ago+symbol%3AAgentStartConfig.Features+&type=code).

## Development

These instructions assume you are running a recent macOS, but could easily be
adapted to Linux and Windows.

```bash
# Make sure you have Go installed.
brew install go

# Download the code somewhere - no GOPATH required.
git clone https://github.com/buildkite/agent.git
cd agent

# Create a temporary builds directory.
mkdir /tmp/buildkite-builds

# Build an agent binary and start the agent.
go build -o /usr/local/bin/buildkite-agent .
buildkite-agent start --debug --build-path=/tmp/buildkite-builds --token "abc"

# Or, run the agent directly and skip the build step.
go run *.go start --debug --build-path=/tmp/buildkite-builds --token "abc"
```

### Go Version and Dependency Management

The latest agent version is typically compiled with the highest-numbered stable
release of Go. Previous Go versions may work, but are not guaranteed to. We are
using newer language features such as generics, so compiling on Go < 1.18 will
fail.

We're using [Go Modules](https://github.com/golang/go/wiki/Modules) to manage
our Go dependencies. Dependencies are not
[vendored](https://go.dev/ref/mod#go-mod-vendor) into the repository unless
necessary.

The Go module published by this repo (i.e. the one you could use by adding `import "github.com/buildkite/agent/v3"` to your code)
is **not considered to be versioned using semantic versioning**. Breaking changes may be introduced in minor releases. Use
the agent as a runtime depedency of your Go app at your own risk.

## Platform Support

We provide support for security and bug fixes on the current major release
only.

Our architecture and operating system support is primarily limited by
[what Go itself supports](https://github.com/golang/go/wiki/MinimumRequirements).

### Architecture Support

We offer support for the following machine architectures (inspired by the Rust
language platform support guidance).

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

We release binaries for various other platforms, and it should be possible to
build the agent anywhere supported by Go, but official support is not provided
for these Tier 3 platforms.

### Operating System Support

We currently provide support for running the Buildkite Agent on the following
operating systems. Future _minor_ releases may drop support for end-of-life
operating systems (typically as they become unsupported by the latest stable Go
release).

The agent binary is fairly portable and should run out of the box on most UNIX
like systems, as well as Windows.

- Ubuntu 20.04 and newer
- Debian 8 and newer
- Red Hat RHEL 7 and newer
- CentOS
  - CentOS 7
  - CentOS 8
- Amazon Linux 2
- macOS [^1]
  - 10.15 (Catalina)
  - 11 (Big Sur)
  - 12 (Monterey)
  - 13 (Ventura)
  - 14 (Sonoma)
- Windows Server
  - 2016
  - 2019
  - 2022

[^1]: See https://github.com/golang/go/issues/23011 for macOS / Go support and
[Supported macOS Versions](./docs/macos.md) for the last supported version of the
Buildkite Agent for versions of macOS prior to those listed above.

## Contributing

See [./CONTRIBUTING.md](./CONTRIBUTING.md)

## Contributors

Many thanks to
[our fine contributors](https://github.com/buildkite/agent/graphs/contributors)!
You're all amazing, and we greatly appreciate your input ❤️

## Copyright

Copyright (c) 2014-2023 Buildkite Pty Ltd.
See [LICENSE](./LICENSE.txt) for details.
