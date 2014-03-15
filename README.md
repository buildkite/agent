# buildbox-agent

The Buildbox Agent written in Go

### What is it?

The Buildbox Agent is responsible for running jobs on your own server.

### How does it work?

The agent polls Buildbox looking for work. When a new job is ready to run, the agent will run the `bootstrap.sh` script with all the environment variables required to run the job.

This script is responsible for creating the build directory, cloning the repo, running the build script, and uploading artifacts.

### Installation

Installing the agent is super easy. All you need to do is run this on your command line:

```bash
bash -c "`curl -sL https://raw.github.com/buildboxhq/buildbox-agent/master/install.sh`"
```

The bootstrap script is by default installed to: `$HOME/.buildbox/bootstrap.sh`

Once installed, you should now be able to run the agent

```bash
buildbox-agent start --access-token token123
```

For more help with the command line interface

```bash
buildbox-agent --help
```

### Upgrading from the Ruby agent

The agent was previously writtenin Ruby, however due to constant installation and performance issues, we've switched to something
a bit more light-weight and universal.

The biggest change is that you no longer define your build scripts on Buildbox. You instead, should write thise scripts inside
your projects, and call them from the agent. So first thing you need to do, is create a file in your project, for example,
`scripts/buildbox.sh`, and fill it with something like this:

```bash
#!/bin/bash

echo '--- bundling'
bundle install

echo '--- preparing database'
./bin/rake db:test:prepare

echo '--- running specs'
./bin/rspec
```

You'll obviously want to edit it to match your own build configuration. You should already have something like this in your
existing build scripts on Buildbox. Once you've created this file, commit it to your source control. After this, edit your
project on Buildbox and you'll notice a new field "Script Path". Enter the path to your build script (here we used `scripts/buildbox.sh`).

Now you can install the new agent and trigger some builds.

### Artifacts

Uploading artifacts is handling by a seperate tool `buildbox-artifact` which is bundled with the agent. You can see
it's general usage in `templates/bootstrap.sh`.

If you'd like to upload artifacts to your own Amazon S3 bucket, edit your `bootstrap.sh` file, and replace the `buildbox-artifact`
call with something like this:

```bash
export AWS_SECRET_ACCESS_KEY=yyy
export AWS_ACCESS_KEY_ID=xxx
$BUILDBOX_DIR/buildbox-artifact upload "$BUILDBOX_ARTIFACT_PATHS" s3://bucket-name/foo/bar --url $BUILDBOX_AGENT_API_URL
```

If you upload artifacts to your own S3 Bucket, you can further secure your artifacts by [Restricting Access to Specific IP Addresses](https://docs.aws.amazon.com/AmazonS3/latest/dev/AccessPolicyLanguage_UseCases_s3_a.html)

### Development

Some basic instructions on setting up your Go environment and the codebase for running.

```bash
# Make sure you have go installed.
brew install go --cross-compile-common

# Setup your GOPATH
export GOPATH="$HOME/Code/go"
export PATH="$HOME/Code/go/bin:$PATH"

# Install godep
go get github.com/kr/godep

# Checkout the code
mkdir -p $GOPATH/src/github.com/buildboxhq/buildbox-agent
git clone git@github.com:buildboxhq/buildbox-agent.git $GOPATH/src/github.com/buildboxhq/buildbox-agent
cd $GOPATH/src/github.com/buildboxhq/buildbox-agent
godep get
go run main.go
```

### Windows Support

Windows support is coming soon. In the meantime, you can use our [ruby agent](https://github.com/buildboxhq/buildbox-agent-ruby)

### Contributing

1. Fork it
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request

### Copyright

Copyright (c) 2014 Keith Pitt. See LICENSE for details.
