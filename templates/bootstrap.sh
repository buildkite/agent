#!/bin/bash

#
#  _           _ _     _ _    _ _              _                 _       _
# | |         (_) |   | | |  (_) |            | |               | |     | |
# | |__  _   _ _| | __| | | ___| |_ ___ ______| |__   ___   ___ | |_ ___| |_ _ __ __ _ _ __
# | '_ \| | | | | |/ _` | |/ / | __/ _ \______| '_ \ / _ \ / _ \| __/ __| __| '__/ _` | '_ \
# | |_) | |_| | | | (_| |   <| | ||  __/      | |_) | (_) | (_) | |_\__ \ |_| | | (_| | |_) |
# |_.__/ \__,_|_|_|\__,_|_|\_\_|\__\___|      |_.__/ \___/ \___/ \__|___/\__|_|  \__,_| .__/
#                                                                                     | |
#                                                                                     |_|
# https://github.com/buildbox/agent/blob/master/templates/bootstrap.sh

BUILDBOX_BOOTSTRAP_VERSION="buildkite-bootstrap version 1.0-beta.1"

##############################################################
#
# BOOTSTRAP FUNCTIONS
# These functions are used throughout the bootstrap.sh file
#
##############################################################

BUILDBOX_PROMPT="\033[90m$\033[0m"

function buildbox-prompt-and-run {
  echo -e "$BUILDBOX_PROMPT $1"
  eval $1
}

function buildbox-run {
  echo -e "$BUILDBOX_PROMPT $1"
  eval $1
  EVAL_EXIT_STATUS=$?

  if [[ $EVAL_EXIT_STATUS -ne 0 ]]; then
    exit $EVAL_EXIT_STATUS
  fi
}

##############################################################
#
# PATH DEFAULTS
# Come up with the paths used throughout the bootstrap.sh file
#
##############################################################

: ${BUILDBOX_PATH:="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"}
: ${BUILDBOX_BIN_PATH:="$BUILDBOX_PATH/bin"}
: ${BUILDBOX_BUILD_PATH:="$BUILDBOX_PATH/builds"}

# Add the $BUILDBOX_BIN to the $PATH
export PATH="$BUILDBOX_BIN_PATH:$PATH"

# Send the bootstrap version back to Buildbox
buildbox-agent build-data set "buildkite:bootstrap:version" $BUILDBOX_BOOTSTRAP_VERSION

# Come up with the place that the repository will be checked out to
SANITIZED_AGENT_NAME=$(echo $BUILDBOX_AGENT_NAME | tr -d '"')
PROJECT_FOLDER_NAME="$SANITIZED_AGENT_NAME/$BUILDBOX_PROJECT_SLUG"
BUILDBOX_BUILD_CHECKOUT_PATH="$BUILDBOX_BUILD_PATH/$PROJECT_FOLDER_NAME"

##############################################################
#
# REPOSITORY HANDLING
# Creates the build folder and makes sure we're running the
# build at the right commit.
#
##############################################################

# Remove the checkout folder if BUILDBOX_CLEAN_CHECKOUT is present
if [[ "$BUILDBOX_CLEAN_CHECKOUT" == "true" ]]; then
  echo '--- Cleaning project checkout'

  buildbox-run "rm -rf \"$BUILDBOX_BUILD_CHECKOUT_PATH\""
fi

echo '--- Preparing build folder'

buildbox-run "mkdir -p \"$BUILDBOX_BUILD_CHECKOUT_PATH\""
buildbox-run "cd \"$BUILDBOX_BUILD_CHECKOUT_PATH\""

# Do we need to do a git checkout?
if [[ ! -d ".git" ]]; then
  # If it's a first time SSH git clone it will prompt to accept the host's
  # fingerprint. To avoid this add the host's key to ~/.ssh/known_hosts ahead
  # of time:
  #   ssh-keyscan -H host.com >> ~/.ssh/known_hosts
  buildbox-run "git clone \"$BUILDBOX_REPO\" . -qv"
fi

buildbox-run "git clean -fdq"
buildbox-run "git fetch -q"

# Allow checkouts of forked pull requests on GitHub only. See:
# https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
if [[ "$BUILDBOX_PULL_REQUEST" != "false" ]] && [[ "$BUILDBOX_PROJECT_PROVIDER" == *"github"* ]]; then
  buildbox-run "git fetch origin \"+refs/pull/$BUILDBOX_PULL_REQUEST/head:\""
elif [[ "$BUILDBOX_TAG" == "" ]]; then
  # Default empty branch names
  : ${BUILDBOX_BRANCH:=master}

  buildbox-run "git reset --hard origin/$BUILDBOX_BRANCH"
fi

buildbox-run "git checkout -qf \"$BUILDBOX_COMMIT\""

# Grab author and commit information and send it back to Buildbox
buildbox-agent build-data set "buildkite:git:commit" "`git show "$BUILDBOX_COMMIT" -s --format=fuller --no-color`"
buildbox-agent build-data set "buildkite:git:branch" "`git branch --contains "$BUILDBOX_COMMIT" --no-color`"

##############################################################
#
# RUN THE BUILD
# Determines how to run the build, and then runs it
#
##############################################################

if [[ "$BUILDBOX_SCRIPT_PATH" == "" ]]; then
  echo "ERROR: No script to run. Please go to \"Project Settings\" and configure your build step's \"Script to Run\""
  exit 1
fi

## Docker
if [[ "$BUILDBOX_DOCKER" != "" ]]; then
  DOCKER_CONTAINER="buildbox_"$BUILDBOX_JOB_ID"_container"
  DOCKER_IMAGE="buildbox_"$BUILDBOX_JOB_ID"_image"

  function docker-cleanup {
    docker rm -f $DOCKER_CONTAINER

    # Enabling the following line will prevent your build server from filling up,
    # but will slow down your builds because it'll be built from scratch each time.
    #
    # docker rmi -f $DOCKER_IMAGE
  }

  trap docker-cleanup EXIT

  # Build the Docker image, namespaced to the job
  buildbox-run "docker build -t $DOCKER_IMAGE ."

  echo "--- Running $BUILDBOX_SCRIPT_PATH (in Docker container $DOCKER_IMAGE)"

  # Run the build script command in a one-off container
  buildbox-prompt-and-run "docker run --name $DOCKER_CONTAINER $DOCKER_IMAGE ./$BUILDBOX_SCRIPT_PATH"

## Fig
elif [[ "$BUILDBOX_FIG_CONTAINER" != "" ]]; then
  # Fig strips dashes and underscores, so we'll remove them to match the docker container names
  FIG_PROJ_NAME="buildbox"${BUILDBOX_JOB_ID//-}
  # The name of the docker container fig creates when it creates the adhoc run
  FIG_CONTAINER_NAME=$FIG_PROJ_NAME"_"$BUILDBOX_FIG_CONTAINER

  function fig-cleanup {
    fig -p $FIG_PROJ_NAME kill
    fig -p $FIG_PROJ_NAME rm --force

    # The adhoc run container isn't cleaned up by fig, so we have to do it ourselves
    echo "Killing "$FIG_CONTAINER_NAME"_run_1..."
    docker rm -f $FIG_CONTAINER_NAME"_run_1"

    # Enabling the following line will prevent your build server from filling up,
    # but will slow down your builds because it'll be built from scratch each time.
    #
    # docker rmi -f $FIG_CONTAINER_NAME
  }

  trap fig-cleanup EXIT

  # Build the Docker images using Fig, namespaced to the job
  buildbox-run "fig -p $FIG_PROJ_NAME build"

  echo "--- Running $BUILDBOX_SCRIPT_PATH (in Fig container '$BUILDBOX_FIG_CONTAINER')"

  # Run the build script command in the service specified in BUILDBOX_FIG_CONTAINER
  buildbox-prompt-and-run "fig -p $FIG_PROJ_NAME run $BUILDBOX_FIG_CONTAINER ./$BUILDBOX_SCRIPT_PATH"

## Standard
else
  echo "+++ Running build script"
  echo -e "$BUILDBOX_PROMPT ./$BUILDBOX_SCRIPT_PATH"

  ."/$BUILDBOX_SCRIPT_PATH"
fi

# Capture the exit status for the end
EXIT_STATUS=$?

##############################################################
#
# ARTIFACTS
# Uploads and build artifacts associated with this build
#
##############################################################

if [[ "$BUILDBOX_ARTIFACT_PATHS" != "" ]]; then
  # If you want to upload artifacts to your own server, uncomment the lines below
  # and replace the AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY with keys to your
  # own bucket.
  #
  # export AWS_SECRET_ACCESS_KEY=yyy
  # export AWS_ACCESS_KEY_ID=xxx
  # export AWS_S3_ACL=private
  # buildbox-run "buildbox-agent build-artifact upload \"$BUILDBOX_ARTIFACT_PATHS\" \"s3://name-of-your-s3-bucket/$BUILDBOX_JOB_ID\""

  # Show the output of the artifact uploder when in debug mode
  if [[ "$BUILDBOX_AGENT_DEBUG" == "true" ]]; then
    echo '--- Uploading artifacts'
    buildbox-run "buildbox-agent build-artifact upload \"$BUILDBOX_ARTIFACT_PATHS\""
  else
    buildbox-run "buildbox-agent build-artifact upload \"$BUILDBOX_ARTIFACT_PATHS\" > /dev/null 2>&1"
  fi
fi

# Be sure to exit this script with the same exit status that the users build
# script exited with.
exit $EXIT_STATUS
