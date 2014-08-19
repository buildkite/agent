# buildbox-agent

Your buildbox agent's are responsible for running build jobs on your own machine or server.

## How it works

The agent polls Buildbox asking for work, and when a new job is given the agent execute [bootstrap.sh](templates/bootstrap.sh) passing all the [standard environment variables](https://buildbox.io/docs/guides/environment-variables) and any configured in your build pipeline's steps.

This way buildbox web can't be configured to run arbitrary commands on your server, it is all down through your local `bootstrap.sh` file. By default it assumes `git`, but your `bootstrap.sh` could do anything you wish.

## Installation

Installing the agent is super easy. All you need to do is run this command ([see the source](https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/install.sh)):

```bash
bash -c "`curl -sL https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/install.sh`"
```

If you'd prefer not to run this install script, you can read the [manual installation guide](#manual-installation)

Once installed, start the agent using the secret access key from your agent settings page:

![image](https://cloud.githubusercontent.com/assets/153/3960325/55662f70-273d-11e4-82c0-75e09d7ee6e6.png)

```bash
buildbox-agent start --access-token b9c784528b92d7e904cfa238e68701f1
```

For more help with the command line interface:

```bash
buildbox-agent --help
```

### Launching on system startup

Follow the instructions for your platform:

* [Upstart (Ubuntu)](/templates/upstart.conf)
* [Launchd (OSX)](/templates/launchd.plist)
* Need another? Send a pull request or let us know!

## Upgrading

Upgrading the agent is pretty straightforward.

1. Run the install script again. This will download new copies of the binaries. **It won't** override the bootstrap.sh file.**

   ```bash
   bash -c "`curl -sL https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/install.sh`"
   ```

2. Tell the `buildbox-agent` process to restart by sending it an `USR2` signal

   ```bash
   killall -USR2 buildbox-agent
   ```

   This will tell it to finish off any current job, and then shut itself down.
   
3. If you use a process monitor such as `upstart` or `launchd` it will startup again automatically, with no more work required. If you don't, just start the buildbox-agent like you did initially.

4. Check your `buildbox-agent` logs to make sure it has started up the agent again.

If you're running a **really long** job, and just want to kill it, send the `USR2` signal twice. That'll cause the `buildbox-agent` process to cancel the current job, and then shutdown.

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
buildbox-artifact upload "$BUILDBOX_ARTIFACT_PATHS" "s3://name-of-your-s3-bucket/$BUILDBOX_JOB_ID" --url $BUILDBOX_AGENT_API_URL
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

## Windows Support

Windows support is coming soon. In the meantime, you can use our [Ruby agent](https://github.com/buildboxhq/buildbox-agent-ruby)

## Manual Installation

Here we'll show you how to manually install the buildbox agent.

1. Create a folder at `~/.buildbox`

   ```bash
   mkdir -p ~/.buildbox
   ```

2. Download the correct binaries for your platform. See: https://github.com/buildboxhq/buildbox-agent/releases/tag/v0.2 for a list for binaries.

   ```bash
   wget https://github.com/buildboxhq/buildbox-agent/releases/download/v0.2/buildbox-agent-linux-amd64.tar.gz
   ```

3. Extract the tar. This should extract `buildbox-agent` and `buildbox-artifact` to the `~/.buildbox` folder.

   ```bash
   tar -C ~/.buildbox -zvxf buildbox-agent-linux-amd64.tar.gz
   ```

4. Download our example `bootstrap.sh` and put it in `~/.buildbox`

   ```bash
   wget -q https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/templates/bootstrap.sh -O ~/.buildbox/bootstrap.sh
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

To test the commands locally:

```bash
go run cmd/artifact/artifact.go upload "buildbox/*.go" --agent-access-token=[..] --job [...] --debug
go run cmd/artifact/artifact.go download "buildbox/*.go" . --agent-access-token=[..] --job [...] --debug
```

## Contributing

1. Fork it
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request

## Copyright

Copyright (c) 2014 Keith Pitt. See LICENSE for details.
