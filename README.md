# Buildkite Agent ![Build status](https://badge.buildkite.com/08e4e12a0a1e478f0994eb1e8d51822c5c74d395.svg?branch=master)

> **RENAME NOTICE**
>
> We’ve just changed our company name from Buildbox to Buildkite, so don’t be confused if you see the word “buildbox” in the instructions below.
> The next version of buildbox-agent will be renamed to buildkite-agent, and we’ll be releasing upgrade instructions when it’s released. In the mean time, just use the instructions below.
> You can read more about the rename on the blog.
> https://buildkite.com/blog/introducing-our-new-name

There are three commands included in the buildbox-agent package:

* buildbox-agent - the main job runner, which polls Buildkite for build steps to execute
* buildbox-data - reads and writes to your build-wide key/value store
* [buildbox-artifact](https://buildkite.com/docs/agent/artifacts) - uploads and downloads files to your build-wide file store

## Installing

Simply run the following command ([see the source](https://raw.githubusercontent.com/buildkite/agent/master/install.sh)), which will automatically download the correct binaries for your platform (or if you'd prefer not to run this install script see the [manual installation guide](#manual-installation)):

```bash
bash -c "`curl -sL https://raw.githubusercontent.com/buildkite/agent/master/install.sh`"
```

Copy the access token from the agent settings page on Buildkite:

![image](https://cloud.githubusercontent.com/assets/153/3960325/55662f70-273d-11e4-82c0-75e09d7ee6e6.png)

And then start the agent with the token:

```bash
~/.buildbox/buildbox-agent start --access-token b9c784528b92d7e904cfa238e68701f1
```

Now you're all set to run your first build!

If you're using Windows, get in touch and we'll get you onto our Private Beta.

### Launching on system startup

We've some templates for the default process manageers for various platforms. Using a process manager will allow to you ensure `buildbox-agent` is running on system boot, and will allow for easy upgrades and restarts because you can simply kill the process and the process manager to start it up again.

* [Upstart (Ubuntu)](/templates/0.2/upstart.conf)
* [Launchd (OSX)](/templates/0.2/launchd.plist)

If you have another to contribute, or need a hand, let us know! (pull requests also welcome)

## Upgrading

Upgrading the agent is simply a matter of re-running the install script and then restarting the agent (with a `USR2`).

1. Run the install script again. This will download new copies of the binaries. Dont' worry, **it won't** override your bootstrap.sh file.

   ```bash
   bash -c "`curl -sL https://raw.githubusercontent.com/buildkite/agent/master/install.sh`"
   ```

2. Tell the `buildbox-agent` process to restart by sending it an `USR2` signal

   ```bash
   killall -USR2 buildbox-agent
   ```

   This will tell it to finish off any current job, and then shut itself down.

3. If you use a process monitor such as `upstart` or `launchd` it will startup again automatically, with no more work required. If you don't, just start the `buildbox-agent` like you did initially.

4. Check your `buildbox-agent` logs to make sure it has started up the agent again.

If you're running a **really long** job, and just want to kill it, send the `USR2` signal twice. That'll cause the `buildbox-agent` process to cancel the current job, and then shutdown.

## How it works

After starting, `buildbox-agent` polls Buildkite over HTTPS looking for work.

When a new job is found it executes [bootstrap.sh](templates/bootstrap.sh) with the [standard Buildkite environment variables](https://buildkite.com/docs/guides/environment-variables) and any extra environment variables configured in your build pipeline's steps.

As the build is running the output stream is continously sent to Buildkite, and when the build finishes it reports the exit status and then returns to looking for new jobs to execute.

Using a `bootsrap.sh` ensures that Buildkite web can't be configured to run arbitrary commands on your server, and it also allows you to configure `bootsrap.sh` to do [anything you wish](#customizing-bootstrapsh) (although it works out-of-the-box with `git`-based projects).

## Artifact uploads

Uploading artifacts is handled by a seperate tool `buildbox-artifact` which is bundled with the agent. You can see
it's general usage in `templates/bootstrap.sh`.

### Uploading Artifacts to your own S3

If you'd like to upload artifacts to your own Amazon S3 bucket, edit your `bootstrap.sh` file, and replace the `buildbox-artifact`
call with something like this:

```bash
export AWS_SECRET_ACCESS_KEY=yyy
export AWS_ACCESS_KEY_ID=xxx
# export AWS_DEFAULT_REGION=ap-southeast-2 (Optionally set the region. Defaults to us-east-1)
buildbox-artifact upload "$BUILDBOX_ARTIFACT_PATHS" "s3://name-of-your-s3-bucket/$BUILDBOX_JOB_ID" --endpoint $BUILDBOX_AGENT_ENDPOINT
```

We'll recognise the `s3://` prefix and upload the artifacts to S3 to the bucket name `name-of-your-s3-bucket` (obviously you'll want to change it to the name of your own S3 bucket)

If you upload artifacts to your own S3 Bucket, you can further secure your artifacts by [Restricting Access to Specific IP Addresses](https://docs.aws.amazon.com/AmazonS3/latest/dev/AccessPolicyLanguage_UseCases_s3_a.html)

## Customizing bootstrap.sh

You can think of the `bootstrap.sh` file as a pre-script for your build. The template that we provide will checkout
a Git project, and run your build script, but that's just the default. You can make it do what ever you like. If you don't use Git, you can edit the script to use what ever version control software you like.

You can also use `bootstrap.sh` to control what repositores are checked out on your CI server. For example, you could
add something like this to the top of your `bootstrap.sh` file:

```bash
if [ "$BUILDBOX_REPO" != *github.com/keithpitt/my-app* ]
then
  echo "Unrecognised repo: $BUILDBOX_REPO"
  exit 1
fi
```

The benefit of the `bootstrap.sh` is that it's written in bash. You can change it how ever you like and customize how
builds get run on your servers.

## Manual Installation

Here we'll show you how to manually install the Buildkite agent.

1. Create a folder at `~/.buildbox`

   ```bash
   mkdir -p ~/.buildbox
   ```

2. Download the correct binaries for your platform. See: https://github.com/buildkite/agent/releases/tag/v0.2 for a list for binaries.

   ```bash
   wget https://github.com/buildkite/agent/releases/download/v0.2/buildbox-agent-linux-amd64.tar.gz
   ```

3. Extract the tar. This should extract `buildbox-agent` and `buildbox-artifact` to the `~/.buildbox` folder.

   ```bash
   tar -C ~/.buildbox -zvxf buildbox-agent-linux-amd64.tar.gz
   ```

4. Download our example `bootstrap.sh` and put it in `~/.buildbox`

   ```bash
   wget -q https://raw.githubusercontent.com/buildkite/agent/master/templates/bootstrap.sh -O ~/.buildbox/bootstrap.sh
   ```

5. (Optional) Add `~/.buildbox` to your `$PATH` so you can access the binaries eaiser.

   ```bash
   echo 'export PATH="$HOME/.buildbox:$PATH"' >> ~/.bash_profile
   ```

## Development

Some basic instructions on setting up your Go environment and the codebase for running.

```bash
# Make sure you have go installed.
brew install go --cross-compile-common
brew install mercurial

# Setup your GOPATH
export GOPATH="$HOME/Code/go"
export PATH="$HOME/Code/go/bin:$PATH"

# Install godep
go get github.com/kr/godep

# Checkout the code
mkdir -p $GOPATH/src/github.com/buildkite/agent
git clone git@github.com:buildkite/agent.git $GOPATH/src/github.com/buildkite/agent
cd $GOPATH/src/github.com/buildkite/agent
godep get
go run *.go start --token xxx --debug
```

To test the commands locally:

```bash
go run cmd/artifact/artifact.go upload "buildkite/*.go" --agent-access-token=[..] --job [...] --debug
go run cmd/artifact/artifact.go download "buildkite/*.go" . --agent-access-token=[..] --job [...] --debug
```

## Contributing

1. Fork it
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request

## Copyright

Copyright (c) 2014-2015 Keith Pitt, Buildkite Pty Ltd. See LICENSE for details.
