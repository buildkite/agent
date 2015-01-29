#!/bin/bash

#
#  _           _ _     _ _    _ _          _                 _       _
# | |         (_) |   | | |  (_) |        | |               | |     | |
# | |__  _   _ _| | __| | | ___| |_ ___   | |__   ___   ___ | |_ ___| |_ _ __ __ _ _ __
# | '_ \| | | | | |/ _` | |/ / | __/ _ \  | '_ \ / _ \ / _ \| __/ __| __| '__/ _` | '_ \
# | |_) | |_| | | | (_| |   <| | ||  __/  | |_) | (_) | (_) | |_\__ \ |_| | | (_| | |_) |
# |_.__/ \__,_|_|_|\__,_|_|\_\_|\__\___|  |_.__/ \___/ \___/ \__|___/\__|_|  \__,_| .__/
#                                                                                 | |
#                                                                                 |_|
# https://github.com/buildkite/agent/blob/master/templates/bootstrap.sh

# Causes this script to exit if a variable isn't present
set -u

##############################################################
#
# BOOTSTRAP FUNCTIONS
# These functions are used throughout the bootstrap.sh file
#
##############################################################

BUILDKITE_PROMPT="\033[90m$\033[0m"

function buildkite-prompt-and-run {
  echo -e "$BUILDKITE_PROMPT $1"
  eval $1
}

function buildkite-run {
  echo -e "$BUILDKITE_PROMPT $1"
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

# Add the $BUILDKITE_BIN_PATH to the $PATH
export PATH="$BUILDKITE_BIN_PATH:$PATH"

# Come up with the place that the repository will be checked out to
SANITIZED_AGENT_NAME=$(echo $BUILDKITE_AGENT_NAME | tr -d '"')
PROJECT_FOLDER_NAME="$SANITIZED_AGENT_NAME/$BUILDKITE_PROJECT_SLUG"
BUILDKITE_BUILD_CHECKOUT_PATH="$BUILDKITE_BUILD_PATH/$PROJECT_FOLDER_NAME"

if [[ "$BUILDKITE_AGENT_DEBUG" == "true" ]]; then
  echo '--- Build environment variables'

  buildkite-run "env | grep BUILDKITE"
fi

##############################################################
#
# REPOSITORY HANDLING
# Creates the build folder and makes sure we're running the
# build at the right commit.
#
##############################################################

# Remove the checkout folder if BUILDKITE_CLEAN_CHECKOUT is present
if [[ ! -z "${BUILDKITE_CLEAN_CHECKOUT:-}" ]] && [[ "$BUILDKITE_CLEAN_CHECKOUT" == "true" ]]; then
  echo '--- Cleaning project checkout'

  buildkite-run "rm -rf \"$BUILDKITE_BUILD_CHECKOUT_PATH\""
fi

echo '--- Preparing build folder'

buildkite-run "mkdir -p \"$BUILDKITE_BUILD_CHECKOUT_PATH\""
buildkite-run "cd \"$BUILDKITE_BUILD_CHECKOUT_PATH\""

# If enabled, automatically run an ssh-keyscan on the git ssh host, to prevent
# a yes/no promp from appearing when cloning/fetching
if [[ ! -z "${BUILDKITE_ENABLE_SSH_KEYSCAN:-}" ]] && [[ "$BUILDKITE_ENABLE_SSH_KEYSCAN" == "true" ]]; then
  if [[ ! -z "${BUILDKITE_ENABLE_SSH_KEYSCAN:-}" ]]; then
    : ${BUILDKITE_SSH_DIRECTORY:="$HOME/.ssh"}
    : ${BUILDKITE_SSH_KNOWN_HOST_PATH:="$BUILDKITE_SSH_DIRECTORY/known_hosts"}

    # Ensure the known_hosts file exists
    mkdir -p $BUILDKITE_SSH_DIRECTORY
    touch $BUILDKITE_SSH_KNOWN_HOST_PATH

    # Only add the output from ssh-keyscan if it doesn't already exist in the
    # known_hosts file
    if ! ssh-keygen -H -F "$BUILDKITE_REPO_SSH_HOST" | grep --quiet "$BUILDKITE_REPO_SSH_HOST"; then
      buildkite-run "ssh-keyscan \"$BUILDKITE_REPO_SSH_HOST\" >> \"$BUILDKITE_SSH_KNOWN_HOST_PATH\""
    fi
  fi
fi

# Do we need to do a git checkout?
if [[ ! -d ".git" ]]; then
  buildkite-run "git clone \"$BUILDKITE_REPO\" . -qv"
fi

# Calling `git clean` with the -x will also remove all ignored files, to create
# a pristine working directory
buildkite-run "git clean -fdqx"
buildkite-run "git submodule foreach --recursive git clean -fdqx"

buildkite-run "git fetch -q"

# Allow checkouts of forked pull requests on GitHub only. See:
# https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
if [[ "$BUILDKITE_PULL_REQUEST" != "false" ]] && [[ "$BUILDKITE_PROJECT_PROVIDER" == *"github"* ]]; then
  buildkite-run "git fetch origin \"+refs/pull/$BUILDKITE_PULL_REQUEST/head:\""
elif [[ "$BUILDKITE_TAG" == "" ]]; then
  # Default empty branch names
  : ${BUILDKITE_BRANCH:=master}

  buildkite-run "git reset --hard origin/$BUILDKITE_BRANCH"
fi

buildkite-run "git checkout -qf \"$BUILDKITE_COMMIT\""

# `submodule sync` will ensure the .git/config matches the .gitmodules file
buildkite-run "git submodule sync"
buildkite-run "git submodule update --init --recursive"
buildkite-run "git submodule foreach --recursive git reset --hard"

# Grab author and commit information and send it back to Buildkite
buildkite-agent build-data set "buildkite:git:commit" "`git show "$BUILDKITE_COMMIT" -s --format=fuller --no-color`"
buildkite-agent build-data set "buildkite:git:branch" "`git branch --contains "$BUILDKITE_COMMIT" --no-color`"

##############################################################
#
# RUN THE BUILD
# Determines how to run the build, and then runs it
#
##############################################################

if [[ "$BUILDKITE_SCRIPT_PATH" == "" ]]; then
  echo "ERROR: No script to run. Please go to \"Project Settings\" and configure your build step's \"Script to Run\""
  exit 1
fi

## Docker
if [[ ! -z "${BUILDKITE_DOCKER:-}" ]] && [[ "$BUILDKITE_DOCKER" != "" ]]; then
  DOCKER_CONTAINER="buildkite_"$BUILDKITE_JOB_ID"_container"
  DOCKER_IMAGE="buildkite_"$BUILDKITE_JOB_ID"_image"

  function docker-cleanup {
    docker rm -f $DOCKER_CONTAINER

    # Enabling the following line will prevent your build server from filling up,
    # but will slow down your builds because it'll be built from scratch each time.
    #
    # docker rmi -f $DOCKER_IMAGE
  }

  trap docker-cleanup EXIT

  echo "--- Building Docker image $DOCKER_IMAGE"

  # Build the Docker image, namespaced to the job
  buildkite-run "docker build -t $DOCKER_IMAGE ."

  echo "+++ Running $BUILDKITE_SCRIPT_PATH (in Docker container $DOCKER_IMAGE)"

  # Run the build script command in a one-off container
  buildkite-prompt-and-run "docker run --name $DOCKER_CONTAINER $DOCKER_IMAGE ./$BUILDKITE_SCRIPT_PATH"

## Fig
elif [[ ! -z "${BUILDKITE_FIG_CONTAINER:-}" ]] && [[ "$BUILDKITE_FIG_CONTAINER" != "" ]]; then
  # Fig strips dashes and underscores, so we'll remove them to match the docker container names
  FIG_PROJ_NAME="buildkite"${BUILDKITE_JOB_ID//-}
  # The name of the docker container fig creates when it creates the adhoc run
  FIG_CONTAINER_NAME=$FIG_PROJ_NAME"_"$BUILDKITE_FIG_CONTAINER

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

  echo "--- Building Fig Docker images"

  # Build the Docker images using Fig, namespaced to the job
  buildkite-run "fig -p $FIG_PROJ_NAME build"

  echo "+++ Running $BUILDKITE_SCRIPT_PATH (in Fig container '$BUILDKITE_FIG_CONTAINER')"

  # Run the build script command in the service specified in BUILDKITE_FIG_CONTAINER
  buildkite-prompt-and-run "fig -p $FIG_PROJ_NAME run $BUILDKITE_FIG_CONTAINER ./$BUILDKITE_SCRIPT_PATH"

## Standard
else
  echo "+++ Running build script"
  echo -e "$BUILDKITE_PROMPT ./$BUILDKITE_SCRIPT_PATH"

  ."/$BUILDKITE_SCRIPT_PATH"
fi

# Capture the exit status for the end
EXIT_STATUS=$?

##############################################################
#
# ARTIFACTS
# Uploads and build artifacts associated with this build
#
##############################################################

if [[ "$BUILDKITE_ARTIFACT_PATHS" != "" ]]; then
  # If you want to upload artifacts to your own server, uncomment the lines below
  # and replace the AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY with keys to your
  # own bucket.
  #
  # export AWS_SECRET_ACCESS_KEY=yyy
  # export AWS_ACCESS_KEY_ID=xxx
  # export AWS_S3_ACL=private
  # buildkite-run "buildkite-agent build-artifact upload \"$BUILDKITE_ARTIFACT_PATHS\" \"s3://name-of-your-s3-bucket/$BUILDKITE_JOB_ID\""

  echo '--- Uploading artifacts'
  buildkite-run "buildkite-agent build-artifact upload \"$BUILDKITE_ARTIFACT_PATHS\""
fi

# Be sure to exit this script with the same exit status that the users build
# script exited with.
exit $EXIT_STATUS
