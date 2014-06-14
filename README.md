# buildbox-agent

The Buildbox Agent is responsible for running jobs on your own CI server.

The agent polls Buildbox looking for work. When a new job is ready to run, the agent will run the `bootstrap.sh` script with all the environment variables required to run the job.

This script is responsible for creating the build directory, cloning the repo, running the build script, and uploading artifacts.

### Installation

Installing the agent is super easy. All you need to do is run this on your CI server:

```bash
bash -c "`curl -sL https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/install.sh`"
```

If you'd prefer not to run this install script, you can read the [manual installation guide](https://github.com/buildboxhq/buildbox-agent#manual-installation)

By default, it sets up a `~/.buildbox` folder. Inside this folder, is the `bootstrap.sh` file, and 2 binaries `buildbox-agent` and `buildbox-artifact`.

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

### Upgrading

Upgrading the agent is pretty straightforward. The general idea is:

1. Download new binaries replacing the old ones.

2. Restart the `buildbox-agent` process.

In practise, this can be quite tricky. But no problems! We've got everything you need to set that up here.

1. Make sure you've got your `buildbox-agent` process running through a process montior. If you're on Ubuntu, the easiest way of setting this up is using [upstart](https://github.com/buildboxhq/buildbox-agent#using-upstart)

2. Run the install script again. This will download new copies of the binaries. **It won't** override the bootstrap.sh file.**

   ```bash
   bash -c "`curl -sL https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/install.sh`"
   ```

3. Tell the `buildbox-agent` process to restart by sending it an `USR2` signal

   ```bash
   killall -USR2 buildbox-agent
   ```

   This will tell it to finish off any current job, and then shut itself down. The process monitor will notice the process has died, and start it back up again.

4. If you're running a **really long** job, and just want to kill it, send the `USR2` signal twice. That'll cause the `buildbox-agent` process to cancel the current job, and then shutdown.

5. Check your `buildbox-agent` logs to make sure it has started up the agent again.

### Upgrading from the Ruby agent

The Buildbox agent was previously written [in Ruby](https://github.com/buildboxhq/buildbox-agent-ruby), however due to installation and performance issues, we've switched to something
a bit more light-weight and universal. Golang fit the bill the best with it's support for compiling to single binaries.

The biggest change you'll notice is that you no longer define your build scripts on Buildbox. You instead should write these scripts and save them to your projects source control.

To migrate to the new agent, the first step is creating a file in your repository (for example `scripts/buildbox.sh`) and fill it with something like this:

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

Now you can install the new agent and trigger some builds. You can use your exising agent access tokens with the new agents.

### Artifacts

Uploading artifacts is handled by a seperate tool `buildbox-artifact` which is bundled with the agent. You can see
it's general usage in `templates/bootstrap.sh`.

#### Uploading Artifacts to S3

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

### Manual Installation

Here we'll show you how to manually install the buildbox agent.

1. Create a folder at `~/.buildbox`

   ```bash
   mkdir -p ~/.buildbox
   ```

2. Download the correct binaries for your platform. See: https://github.com/buildboxhq/buildbox-agent/releases/tag/v0.1 for a list for binaries.

   ```bash
   wget https://github.com/buildboxhq/buildbox-agent/releases/download/v0.1/buildbox-agent-linux-amd64.tar.gz
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

### Customizing bootstrap.sh

You can think of the `bootstrap.sh` file as a pre-script for your build. The template that we provide will checkout
a Git project, and run your build script, but that's just the default. You can make it do what ever you like. If you don't use Git, you can edit the script
to use what ever version control software you like.

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

### Running in the background

#### Using `upstart`

Upstart is a process monitoring tool built into Ubuntu. You can use it to run the `buildbox-agent` process. The benefit of using
upstart is that it will restart the process when you restart your server.

1. Download our upstart template and save it to: `/etc/init/buildbox-agent.conf`

   ```bash
   sudo wget https://raw.githubusercontent.com/buildboxhq/buildbox-agent/master/templates/buildbox-agent.conf -O /etc/init/buildbox-agent.conf
   ```

2. Edit the file and replace `your-build-user` with the user you'd like to run the builds as.

   ```bash
   sudo sed -i "s/your-build-user/`whoami`/g" /etc/init/buildbox-agent.conf
   ```

3. You'll also need to change the `access-token` it uses.

   ```bash
   sudo sed -i "s/your-agent-access-token/[insert your agent access token here]/g" /etc/init/buildbox-agent.conf
   ```

4. Type `sudo service buildbox-agent start` when you're ready to start the process.

5. Logs will be available here: `/var/log/upstart/buildbox-agent.log`

   ```bash
   sudo tail -f -n 200 /var/log/upstart/buildbox-agent.log
   ```

#### Using `screen`

1. The first step is installing screen

   ```bash
   sudo apt-get install screen
   ```

2. Then load up screen

   ```bash
   screen
   ```

3. Once you're in a new screen, start the `buildbox-agent` process

  ```bash
  buildbox-agent start --access-token token1234
  ```

4. Now that it's started, you can exit out of the screen by hitting `Ctrl-a` then `d` on your keyboard.

5. To resume the screen, type:

   ```bash
   screen -r
   ```

You can read more about how screen works over at the [Screen User's Manual](http://www.gnu.org/software/screen/manual/screen.html)

### Windows Support

Windows support is coming soon. In the meantime, you can use our [Ruby agent](https://github.com/buildboxhq/buildbox-agent-ruby)

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
