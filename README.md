# Buildkite Agent ![Build status](https://badge.buildkite.com/08e4e12a0a1e478f0994eb1e8d51822c5c74d395.svg?branch=master)

_Note: This is the development branch of the buildkite-agent, and may not contain files or code in the current stable release._

The buildkite-agent is a small, reliable, and cross-platform build runner that makes it easy to run automated builds on your own infrastructure. It’s main responsibilities are polling [buildkite.com](https://buildkite.com/) for work, running build jobs, reporting back the status code and output log of the job, and uploading the job's artifacts.

Full documentation is available at [buildkite.com/docs/agent](https://buildkite.com/docs/agent)

```
$ buildkite-agent --help
Usage:

  buildkite-agent <command> [arguments...]

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

## Installing

The agents page on Buildkite has personalised instructions for installing the agent with Ubuntu (via apt), Debian (via apt), macOS (via homebrew), Windows and Linux. You can also run the agent [via Docker](https://hub.docker.com/r/buildkite/agent).

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

We're using Go 1.12+ and [Go Modules](https://github.com/golang/go/wiki/Modules) to manage our Go dependencies. We are keeping the dependencies vendored to remain backwards compatible with older go versions.

If you are using Go 1.11+ and have the agent in your `GOPATH`, you will need to enable modules via the environment variable:

```bash
export GO111MODULE=on
```

If you introduce a new package:

```bash
go get github.com/my/new/package
```

Then you can write that package to the `vendor/` with:

```bash
go mod vendor
```

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
