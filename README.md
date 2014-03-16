# buildbox-agent

The Buildbox Agent is responsible for running jobs on your own server.

The agent polls Buildbox looking for work. When a new job is ready to run, the agent will run the `bootstrap.sh` script with all the environment variables required to run the job.

This script is responsible for creating the build directory, cloning the repo, running the build script, and uploading artifacts.

### Installation

Installing the agent is super easy. All you need to do is run this on your command line:

```bash
bash -c "`curl -sL https://raw.github.com/buildboxhq/buildbox-agent/master/install.sh`"
```

If you'd prefer not to run this install script, you can read the [manual installation guide](https://github.com/buildboxhq/buildbox-agent#manual-installation)

The bootstrap script is by default installed to: `$HOME/.buildbox/bootstrap.sh`

Once installed, you should now be able to run the agent

```bash
buildbox-agent start --access-token token123
# or
export BUILDBOX_AGENT_ACCESS_TOKEN=token123
buildbox-agent start
```

For more help with the command line interface

```bash
buildbox-agent --help
```

### Upgrading from the Ruby agent

The Buildbox agent was previously written [in Ruby](https://github.com/buildboxhq/buildbox-agent-ruby), however due to installation and performance issues, we've switched to something
a bit more light-weight and universal. Golang fit the bill the best with it's support for compiling to single binaries.

The biggest change you'll notice is that you no longer define your build scripts on Buildbox. You instead should write these scripts inside
your projects source control.

To migrate to the new agent, the first step is creating a file in your project (for example `scripts/buildbox.sh`) and fill it with something like this:

```bash
#!/bin/bash
set -e # exit if any command fails

echo '--- bundling'
bundle install

echo '--- preparing database'
./bin/rake db:test:prepare

echo '--- running specs'
./bin/rspec
```

You'll obviously want to edit it to match your own build configuration. You should already have something like this in your
existing build scripts on Buildbox. Once you've created this file, commit it to your source control and push. Next, go to your
Project Settings on Buildbox and update the "Script Path" field with the path to your new build script (here we used `scripts/buildbox.sh`).

Now you can install the new agent and trigger some builds. You can use your exising agent tokens with the new agents.

### Artifacts

Uploading artifacts is handling by a seperate tool `buildbox-artifact` which is bundled with the agent. You can see
it's general usage in `templates/bootstrap.sh`.

If you'd like to upload artifacts to your own Amazon S3 bucket, edit your `bootstrap.sh` file, and replace the `buildbox-artifact`
call with something like this:

```bash
export AWS_SECRET_ACCESS_KEY=yyy
export AWS_ACCESS_KEY_ID=xxx
buildbox-artifact upload "$BUILDBOX_ARTIFACT_PATHS" s3://bucket-name/foo/bar
```

If you upload artifacts to your own S3 Bucket, you can further secure your artifacts by [Restricting Access to Specific IP Addresses](https://docs.aws.amazon.com/AmazonS3/latest/dev/AccessPolicyLanguage_UseCases_s3_a.html)

### Manual Installation

Here we'll show you how to manually install the buildbox agent.

1. Create a folder at `~/.buildbox`

   ```bash
   mkdir -p ~/.buildbox
   ```

2. Download the correct binaries for your platform. See: https://github.com/buildboxhq/buildbox-agent/releases/tag/v0.1-beta1 for a list for binaries.

   ```bash
   wget https://github.com/buildboxhq/buildbox-agent/releases/download/v0.1-beta1/buildbox-agent-linux-amd64.tar.gz
   ```

3. Extract the tar. This should extract `buildbox-agent` and `buildbox-artifact` to the `~/.buildbox` folder.

   ```bash
   tar -C ~/.buildbox -zvxf buildbox-agent-linux-amd64.tar.gz
   ```

4. Download our example `bootstrap.sh` and put it in `~/.buildbox`

   ```bash
   wget -q https://raw.github.com/buildboxhq/buildbox-agent/master/templates/bootstrap.sh -O ~/.buildbox/bootstrap.sh
   ```

5. Add `~/.buildbox` to your `$PATH` so you can access the binaries.

   ```bash
   echo 'export PATH="$HOME/.buildbox:$PATH"' >> ~/.bash_profile
   ```

### Customizing bootstrap.sh

You can think of the `bootstrap.sh` file as a pre-script for your build. The template that we provide will checkout
a Git project, and run your build script - but it can do what ever you like. If you don't use Git, you can edit the script
to use what ever version control software you like.

You can also use `bootstrap.sh` to control what repositores are checked out on your CI server. For example, you could
add something like this to the top of your `bootstrap.sh` file:

```bash
if [ "$BUILDBOX_REPO" != "http://github.com/my/project" ]
then
  echo "Unrecognised repo: $BUILDBOX_REPO"
fi
```

The benefit of the `bootstrap.sh` is that it's written in bash. You can change it how ever you like and customize how
builds get run on your servers.

### Windows Support

Windows support is coming soon. In the meantime, you can use our [ruby agent](https://github.com/buildboxhq/buildbox-agent-ruby)

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
go run agent.go
```

### Contributing

1. Fork it
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request

### Copyright

Copyright (c) 2014 Keith Pitt. See LICENSE for details.
