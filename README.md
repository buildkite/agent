# Buildkite Agent ![Build status](https://badge.buildkite.com/08e4e12a0a1e478f0994eb1e8d51822c5c74d395.svg?branch=main)

_Note: This is the development branch of the buildkite-agent, and may not contain files or code in the current stable release._

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
  bootstrap  Run a Buildkite job locally
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

- 3-ubuntu-20.04, tracks minor updates in version 3 installed in Ubuntu 20.04
- 3.30-ubuntu-20.04, tracks bugfix updates in version 3.30 installed in Ubuntu 20.04
- 3.30.0-ubuntu-20.04, tracks the exact version installed in Ubuntu 20.04

#### Tier 1, guaranteed to work

- Alpine 3.12
- Ubuntu 18.04 LTS (x86_64), supported to end of life for 18.04
- Ubuntu 20.04 LTS (x86_64), supported to end of life for 20.04

#### Tier 2, guaranteed to build

- Centos 8

## Starting

To start an agent all you need is your agent token, which you can find on your Agents page within Buildkite.

```bash
buildkite-agent start --token
```

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
go build -i -o /usr/local/bin/buildkite-agent .
buildkite-agent start --debug --build-path=/tmp/buildkite-builds --token "abc"

# Or, run the agent directly and skip the build step
go run *.go start --debug --build-path=/tmp/buildkite-builds --token "abc"
```

### Dependency management

We're using Go 1.14+ and [Go Modules](https://github.com/golang/go/wiki/Modules) to manage our Go dependencies.

If you are using Go 1.11+ and have the agent in your `GOPATH`, you will need to enable modules via the environment variable:

```bash
export GO111MODULE=on
```

Dependencies are no longer committed to the repository, so compiling on Go <= 1.10 is not supported.

## Platform Support

We provide support for security and bug fixes on the current major release only.

### Architecture Support

We offer support for the following machine architectures (inspired by the Rust language platform
support guidance):

#### Tier 1, guaranteed to work

- linux x86_64
- windows x86_64

#### Tier 2, guaranteed to build

- linux x86
- linux arm64
- windows x86
- darwin x86_64
- darwin arm64

#### Tier 3, community supported

- linux arm
- linux armf
- linux ppc64
- linux mips64
- linux s390x
- netbsd x86_64
- freebsd x86
- freebsd x86_64
- openbsd x86
- openbsd x84_64
- dragonfly x86_64

### Operating System Support

We provide support for running the Buildkite Agent on the following operating systems.
The agent binary is fairly portable and should run out of the box on most UNIX like
systems.

- Ubuntu [min version TBD]
- Debian [min version TBD]
- RedHat [min version TBD]
- Amazon Linux [min version TBD]
- macOS
  - 10.11
  - 10.12
  - 10.13
  - 10.14
  - 10.15
  - 11
- Windows Server
  - 2016
  - 2019

## Contributing

1. Fork it
1. Create your feature branch (`git checkout -b my-new-feature`)
1. Commit your changes (`git commit -am 'Add some feature'`)
1. Push to the branch (`git push origin my-new-feature`)
1. Create new Pull Request

## Contributors

Many thanks to our fine contributors! @adill, @airhorns, @alexjurkiewicz, @bendrucker, @bradfeehan, @byroot, @cab, @caiofbpa, @colinrymer, @cysp, @daveoflynn, @daveoxley, @daveslutzkin, @davidk-zenefits, @DazWorrall, @dch, @deoxxa, @dgoodlad, @donpinkster, @essen, @grosskur, @jgavris, @joelmoss, @jules2689, @julianwa, @kouky, @marius92mc, @mirdhyn, @mousavian, @nikyoudale, @pda, @rprieto, @samritchie, @silarsis, @skevy, @stefanmb, @tekacs, @theojulienne, @tommeier, @underscorediscovery, and @wolfeidau.

## Copyright

Copyright (c) 2014-2019 Buildkite Pty Ltd. See [LICENSE](./LICENSE.txt) for details.
