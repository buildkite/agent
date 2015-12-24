# Buildkite Agent ![Build status](https://badge.buildkite.com/08e4e12a0a1e478f0994eb1e8d51822c5c74d395.svg?branch=master)

The buildkite-agent is a small, reliable, and cross-platform build runner that makes it easy to run automated builds on your own infrastructure. It’s main responsibilities are polling [buildkite.com](https://buildkite.com/) for work, running build jobs, reporting back the status code and output log of the job, and uploading the job's artifacts.

```
$ buildkite-agent --help
Usage:

  buildkite-agent <command> [arguments...]

Available commands are:

  start		Starts a Buildkite agent
  artifact	Upload/download artifacts from Buildkite jobs
  meta-data	Get/set data from Buildkite jobs
  pipeline	Make changes to the pipeline of the currently running build
  help, h	Shows a list of commands or help for one command

Use "buildkite-agent <command> --help" for more information about a command.
```

## Installing

The agents page on Buildkite has personalised instructions for installing the agent with Ubuntu (via apt), Debian (via apt), Mac OS X (via homebrew), Windows and Linux. You can also run the agent [via Docker](https://hub.docker.com/r/buildkite/agent).

## Starting

To start an agent all you need is your agent token, which you an find on your Agents page within Buildkite.

```
$ buildkite-agent start --token
```

## Development

### With Docker

```bash
$ docker-compose run agent bash
root@d854f845511a:/go/src/github.com/buildkite/agent# go run *.go start --token xxx --debug
```

### Without Docker

```bash
# Make sure you have go installed.
brew install go --cross-compile-common
brew install mercurial

# Setup your GOPATH
export GOPATH="$HOME/Code/go"
export PATH="$HOME/Code/go/bin:$PATH"

# Checkout the code
mkdir -p $GOPATH/src/github.com/buildkite/agent
git clone git@github.com:buildkite/agent.git $GOPATH/src/github.com/buildkite/agent
cd $GOPATH/src/github.com/buildkite/agent
```

To test the commands locally:

```bash
go run *.go start --debug --token "abc123"
```

## Contributing

1. Fork it
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request

## Copyright

Copyright (c) 2014-2015 Keith Pitt, Buildkite Pty Ltd. See LICENSE for details.
