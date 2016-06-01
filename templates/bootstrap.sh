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

# It's possible for a hook or a build script to change things like `set -eou
# pipefail`, causing our bootstrap.sh to misbehave, so this function will set
# them back to what we expect them to be.
function buildkite-flags-reset {
  # Causes this script to exit if a variable isn't present
  set -u

  # Ensure command pipes fail if any command fails (e.g. fail-cmd | success-cmd == fail)
  set -o pipefail

  # Turn off debugging
  set +x

  # If a command fails, don't exit, just keep on truckin'
  set +e
}

buildkite-flags-reset

##############################################################
#
# BOOTSTRAP FUNCTIONS
# These functions are used throughout the bootstrap.sh file
#
##############################################################

BUILDKITE_PROMPT="\033[90m$\033[0m"

function buildkite-prompt {
  echo -ne "$BUILDKITE_PROMPT "
  echo "$@"
}

# Shows the command being run, and runs it
function buildkite-prompt-and-run {
  buildkite-prompt "$@"
  "$@"
}

# Shows the command about to be run, and exits if it fails
function buildkite-run {
  buildkite-prompt-and-run "$@" || exit $?
}

function buildkite-debug {
  if [[ "$BUILDKITE_AGENT_DEBUG" == "true" ]]; then
    echo "$@"
  fi
}

# Runs the command, but only output what it's doing if we're in DEBUG mode
function buildkite-run-debug {
  if [[ "$BUILDKITE_AGENT_DEBUG" == "true" ]]; then
    buildkite-prompt "$@"
  fi

  "$@"
}

# Show an error and exit
function buildkite-error {
  echo -e "~~~ :rotating_light: \033[31mBuildkite Error\033[0m"
  echo "$@"
  exit 1
}

# Show a warning
function buildkite-warning {
  echo -ne "\033[33m⚠️ Buildkite Warning:\033[0m "
  echo "$@"
  echo "^^^ +++"
}

# Run a hook script. It won't exit on failure. It will store the hooks exit
# status in BUILDKITE_LAST_HOOK_EXIT_STATUS
export BUILDKITE_LAST_HOOK_EXIT_STATUS=""
function buildkite-hook {
  HOOK_LABEL="$1"
  HOOK_SCRIPT_PATH="$2"

  if [[ -e "$HOOK_SCRIPT_PATH" ]]; then
    # Make sure the script path is executable
    chmod +x "$HOOK_SCRIPT_PATH"

    # Print to the screen we're going to run the hook
    echo "~~~ Running $HOOK_LABEL hook"
    echo -ne "$BUILDKITE_PROMPT "
    echo ".\"$HOOK_SCRIPT_PATH\""

    # Run the script and store it's exit status. We don't run the hook in a
    # subshell because we want the hook scripts to be able to modify the
    # bootstrap's ENV variables. The only downside with this approach, is if
    # they call `exit`, the bootstrap script will exit as well. We this is an
    # acceptable tradeoff.
    . "$HOOK_SCRIPT_PATH"
    BUILDKITE_LAST_HOOK_EXIT_STATUS=$?

    # Reset the bootstrap.sh flags
    buildkite-flags-reset
  elif [[ "$BUILDKITE_AGENT_DEBUG" == "true" ]]; then
    # When in debug mode, show that we've skipped a hook
    echo "~~~ Running $HOOK_LABEL hook"
    echo "Skipping, no hook script found at: $HOOK_SCRIPT_PATH"
  fi
}

# Exit from the bootstrap.sh script if the hook exits with a non-0 exit status
function buildkite-hook-exit-on-error {
  if [[ $BUILDKITE_LAST_HOOK_EXIT_STATUS != "" ]] && [[ $BUILDKITE_LAST_HOOK_EXIT_STATUS -ne 0 ]]; then
    echo "Hook returned an exit status of $BUILDKITE_LAST_HOOK_EXIT_STATUS, exiting..."
    exit $BUILDKITE_LAST_HOOK_EXIT_STATUS
  fi
}

function buildkite-global-hook {
  buildkite-hook "global $1" "$BUILDKITE_HOOKS_PATH/$1"
  buildkite-hook-exit-on-error
}

function buildkite-local-hook {
  buildkite-hook "local $1" ".buildkite/hooks/$1"
  buildkite-hook-exit-on-error
}

##############################################################
#
# PATH DEFAULTS
# Come up with the paths used throughout the bootstrap.sh file
#
##############################################################

# Add the $BUILDKITE_BIN_PATH to the $PATH
export PATH="$PATH:$BUILDKITE_BIN_PATH"

# Come up with the place that the repository will be checked out to
SANITIZED_AGENT_NAME=$(echo "$BUILDKITE_AGENT_NAME" | tr -d '"')
PROJECT_FOLDER_NAME="$SANITIZED_AGENT_NAME/$BUILDKITE_PROJECT_SLUG"
export BUILDKITE_BUILD_CHECKOUT_PATH="$BUILDKITE_BUILD_PATH/$PROJECT_FOLDER_NAME"

if [[ "$BUILDKITE_AGENT_DEBUG" == "true" ]]; then
  echo "~~~ Build environment variables"
  env | grep BUILDKITE | sort
fi

##############################################################
#
# ENVIRONMENT SETUP
# A place for people to set up environment variables that
# might be needed for their build scripts, such as secret
# tokens and other information.
#
##############################################################

buildkite-global-hook "environment"

##############################################################
#
# REPOSITORY HANDLING
# Creates the build folder and makes sure we're running the
# build at the right commit.
#
##############################################################

# Run the `pre-checkout` hook
buildkite-global-hook "pre-checkout"

# Remove the checkout folder if BUILDKITE_CLEAN_CHECKOUT is present
if [[ ! -z "${BUILDKITE_CLEAN_CHECKOUT:-}" ]] && [[ "$BUILDKITE_CLEAN_CHECKOUT" == "true" ]]; then
  echo "~~~ Cleaning project checkout"

  buildkite-run rm -rf "$BUILDKITE_BUILD_CHECKOUT_PATH"
fi

echo "~~~ Preparing build folder"

buildkite-run mkdir -p "$BUILDKITE_BUILD_CHECKOUT_PATH"
buildkite-run cd "$BUILDKITE_BUILD_CHECKOUT_PATH"

# If the user has specificed their own checkout hook
if [[ -e "$BUILDKITE_HOOKS_PATH/checkout" ]]; then
  buildkite-global-hook "checkout"
else
  # If enabled, automatically run an ssh-keyscan on the git ssh host, to prevent
  # a yes/no promp from appearing when cloning/fetching
  if [[ ! -z "${BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION:-}" ]] && [[ "$BUILDKITE_AUTO_SSH_FINGERPRINT_VERIFICATION" == "true" ]]; then
    # Only bother running the keyscan if the SSH host has been provided by
    # Buildkite. It won't be present if the host isn't using the SSH protocol
    if [[ ! -z "${BUILDKITE_REPO_SSH_HOST:-}" ]]; then
      : "${BUILDKITE_SSH_DIRECTORY:="$HOME/.ssh"}"
      : "${BUILDKITE_SSH_KNOWN_HOST_PATH:="$BUILDKITE_SSH_DIRECTORY/known_hosts"}"

      # Ensure the known_hosts file exists
      mkdir -p "$BUILDKITE_SSH_DIRECTORY"
      touch "$BUILDKITE_SSH_KNOWN_HOST_PATH"

      # Only add the output from ssh-keyscan if it doesn't already exist in the
      # known_hosts file (unhashed or hashed).
      ssh-keygen -f "$BUILDKITE_SSH_KNOWN_HOST_PATH" -F "$BUILDKITE_REPO_SSH_HOST" > /dev/null ||
        ssh-keygen -f "$BUILDKITE_SSH_KNOWN_HOST_PATH" -F "$BUILDKITE_REPO_SSH_HOST" -H > /dev/null ||
        buildkite-run ssh-keyscan "$BUILDKITE_REPO_SSH_HOST" >> "$BUILDKITE_SSH_KNOWN_HOST_PATH"
    fi
  fi

  # Disable any interactive Git/SSH prompting
  export GIT_TERMINAL_PROMPT=0

  # Do we need to do a git checkout?
  if [[ -d ".git" ]]; then
    buildkite-run git remote set-url origin "$BUILDKITE_REPO"
  else
    BUILDKITE_GIT_CLONE_FLAGS=${BUILDKITE_GIT_CLONE_FLAGS:--v}
    buildkite-run git clone "$BUILDKITE_GIT_CLONE_FLAGS" -- "$BUILDKITE_REPO" .
  fi

  BUILDKITE_GIT_CLEAN_FLAGS=${BUILDKITE_GIT_CLEAN_FLAGS:--fdq}
  buildkite-run git clean "$BUILDKITE_GIT_CLEAN_FLAGS"

  if [[ -z "${BUILDKITE_DISABLE_GIT_SUBMODULES:-}" ]]; then
    buildkite-run git submodule foreach --recursive git clean "$BUILDKITE_GIT_CLEAN_FLAGS"
  fi

  # If a refspec is provided then use it instead.
  # i.e. `refs/not/a/head`
  if [[ -n "${BUILDKITE_REFSPEC:-}" ]]; then
    buildkite-run git fetch -v origin "$BUILDKITE_REFSPEC"
    buildkite-run git checkout -f "$BUILDKITE_COMMIT"

  # GitHub has a special ref which lets us fetch a pull request head, whether
  # or not there is a current head in this repository or another which
  # references the commit. We presume a commit sha is provided. See:
  # https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
  elif [[ "$BUILDKITE_PULL_REQUEST" != "false" ]] && [[ "$BUILDKITE_PROJECT_PROVIDER" == *"github"* ]]; then
    buildkite-run git fetch -v origin "refs/pull/$BUILDKITE_PULL_REQUEST/head"
    buildkite-run git checkout -f "$BUILDKITE_COMMIT"

  # If the commit is "HEAD" then we can't do a commit-specific fetch and will
  # need to fetch the remote head and checkout the fetched head explicitly.
  elif [[ "$BUILDKITE_COMMIT" == "HEAD" ]]; then
    buildkite-run git fetch -v origin "$BUILDKITE_BRANCH"
    buildkite-run git checkout -f FETCH_HEAD

  # Otherwise fetch and checkout the commit directly. Some repositories don't
  # support fetching a specific commit so we fall back to fetching all heads
  # and tags, hoping that the commit is included.
  else
    # By default `git fetch origin` will only fetch tags which are reachable
    # from a fetches branch. git 1.9.0+ changed `--tags` to fetch all tags in
    # addition to the default refspec, but pre 1.9.0 it excludes the default
    # refspec.
    buildkite-prompt-and-run git fetch -v origin "$BUILDKITE_COMMIT" ||
      buildkite-run git fetch -v origin "$(git config remote.origin.fetch)" "+refs/tags/*:refs/tags/*"
    buildkite-run git checkout -f "$BUILDKITE_COMMIT"
  fi

  if [[ -z "${BUILDKITE_DISABLE_GIT_SUBMODULES:-}" ]]; then
    # `submodule sync` will ensure the .git/config matches the .gitmodules file.
    # The command is only available in git version 1.8.1, so if the call fails,
    # continue the bootstrap script, and show an informative error.
    buildkite-prompt-and-run git submodule sync --recursive
    if [[ $? -ne 0 ]]; then
      buildkite-warning "Failed to recursively sync git submodules. This is most likely because you have an older version of git installed ($(git --version)) and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can."
    fi

    buildkite-run git submodule update --init --recursive
    buildkite-run git submodule foreach --recursive git reset --hard
  fi

  # Grab author and commit information and send it back to Buildkite
  buildkite-debug "~~~ Saving Git information"

  # Check to see if the meta data exists before setting it
  buildkite-run-debug buildkite-agent meta-data exists "buildkite:git:commit"
  if [[ $? -ne 0 ]]; then
    buildkite-run-debug buildkite-agent meta-data set "buildkite:git:commit" "$(git show HEAD -s --format=fuller --no-color)"
    buildkite-run-debug buildkite-agent meta-data set "buildkite:git:branch" "$(git branch --contains HEAD --no-color)"
  fi
fi

# Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
# one of the post-checkout hooks changed it.
PREVIOUS_BUILDKITE_BUILD_CHECKOUT_PATH=$BUILDKITE_BUILD_CHECKOUT_PATH

# Run the `post-checkout` hook
buildkite-global-hook "post-checkout"

# Now that we have a repo, we can perform a `post-checkout` local hook
buildkite-local-hook "post-checkout"

# If the working directory has been changed by a hook, log and switch to it
if [[ "$BUILDKITE_BUILD_CHECKOUT_PATH" != "$PREVIOUS_BUILDKITE_BUILD_CHECKOUT_PATH" ]]; then
  echo "~~~ A post-checkout hook has changed the working directory to $PREVIOUS_BUILDKITE_BUILD_CHECKOUT_PATH"

  if [ -d "$BUILDKITE_BUILD_CHECKOUT_PATH" ]; then
    buildkite-run cd "$BUILDKITE_BUILD_CHECKOUT_PATH"
  else
    buildkite-error "Failed to switch to \"$BUILDKITE_BUILD_CHECKOUT_PATH\" as it doesn't exist"
  fi
fi

##############################################################
#
# RUN THE BUILD
# Determines how to run the build, and then runs it
#
##############################################################

# Run the global `pre-command` hook
buildkite-global-hook "pre-command"

# Run the per-checkout `pre-command` hook
buildkite-local-hook "pre-command"

# If the user has specificed a local `command` hook
if [[ -e ".buildkite/hooks/command" ]]; then
  # Manually run the hook to avoid it from exiting on failure
  buildkite-hook "local command" ".buildkite/hooks/command"

  # Capture the exit status from the build script
  export BUILDKITE_COMMAND_EXIT_STATUS=$BUILDKITE_LAST_HOOK_EXIT_STATUS
# Then check for a global hook path
elif [[ -e "$BUILDKITE_HOOKS_PATH/command" ]]; then
  # Manually run the hook to avoid it from exiting on failure
  buildkite-hook "global command" "$BUILDKITE_HOOKS_PATH/command"

  # Capture the exit status from the build script
  export BUILDKITE_COMMAND_EXIT_STATUS=$BUILDKITE_LAST_HOOK_EXIT_STATUS
else
  # Make sure we actually have a command to run
  if [[ "$BUILDKITE_COMMAND" == "" ]]; then
    buildkite-error "No command has been defined. Please go to \"Project Settings\" and configure your build step's \"Command\""
  fi

  # Generate a temporary build script containing what to actually run.
  buildkite-debug "~~~ Preparing build script"
  BUILDKITE_SCRIPT_PATH="buildkite-script-$BUILDKITE_JOB_ID"

  # Generate a different script depending on whether or not it's a script to
  # execute
  if [[ -f "$BUILDKITE_COMMAND" ]]; then
    # Make sure the script they're trying to execute has chmod +x. We can't do
    # this inside the script we generate because it fails within Docker:
    # https://github.com/docker/docker/issues/9547
    buildkite-run-debug chmod +x "$BUILDKITE_COMMAND"
    echo -e '#!/bin/bash'"\nset -eo pipefail\n./\"$BUILDKITE_COMMAND\"" > "$BUILDKITE_SCRIPT_PATH"
  else
    echo -e '#!/bin/bash'"\nset -eo pipefail\n$BUILDKITE_COMMAND" > "$BUILDKITE_SCRIPT_PATH"
  fi

  if [[ "$BUILDKITE_AGENT_DEBUG" == "true" ]]; then
    buildkite-run cat "$BUILDKITE_SCRIPT_PATH"
  fi

  # Ensure the temporary build script can be executed
  chmod +x "$BUILDKITE_SCRIPT_PATH"

  # If the command isn't a file on the filesystem, then it's something we need to
  # eval. But before we even try running it, we should double check that the
  # agent is allowed to eval commands.
  #
  # NOTE: There is a slight problem with this check - and it's with usage with
  # Docker. If you specify a script to run inside the docker container, and that
  # isn't on the file system at the same path, then it won't match, and it'll be
  # treated as an eval. For example, you mount your repository at /app, and tell
  # the agent run `app/ci.sh`, ci.sh won't exist on the filesytem at this point
  # at app/ci.sh. The soltion is to make sure the `workdir` directroy of the
  # docker container is at /app in that case.
  if [[ ! -f "$BUILDKITE_COMMAND" ]]; then
    # Make sure the agent is even allowed to eval commands
    if [[ "$BUILDKITE_COMMAND_EVAL" != "true" ]]; then
      buildkite-error "This agent is not allowed to evaluate console commands. To allow this, re-run this agent without the \`--no-command-eval\` option, or specify a script within your repository to run instead (such as scripts/test.sh)."
    fi

    BUILDKITE_COMMAND_ACTION="Running command"
    BUILDKITE_COMMAND_DISPLAY=$BUILDKITE_COMMAND
  else
    BUILDKITE_COMMAND_ACTION="Running build script"
    BUILDKITE_COMMAND_DISPLAY="./\"$BUILDKITE_COMMAND\""
  fi

  ## Docker
  if [[ ! -z "${BUILDKITE_DOCKER:-}" ]] && [[ "$BUILDKITE_DOCKER" != "" ]]; then
    DOCKER_CONTAINER="buildkite_${BUILDKITE_JOB_ID}_container"
    DOCKER_IMAGE="buildkite_${BUILDKITE_JOB_ID}_image"

    function docker-cleanup {
      echo "~~~ Cleaning up Docker containers"
      buildkite-prompt-and-run docker rm -f -v "$DOCKER_CONTAINER"
    }

    trap docker-cleanup EXIT

    # Build the Docker image, namespaced to the job
    echo "~~~ Building Docker image $DOCKER_IMAGE"
    buildkite-run docker build -f "${BUILDKITE_DOCKER_FILE:-Dockerfile}" -t "$DOCKER_IMAGE" .

    # Run the build script command in a one-off container
    echo "~~~ $BUILDKITE_COMMAND_ACTION (in Docker container)"
    buildkite-prompt-and-run docker run --name "$DOCKER_CONTAINER" "$DOCKER_IMAGE" "./$BUILDKITE_SCRIPT_PATH"

    # Capture the exit status from the build script
    export BUILDKITE_COMMAND_EXIT_STATUS=$?

  ## Docker Compose
  elif [[ ! -z "${BUILDKITE_DOCKER_COMPOSE_CONTAINER:-}" ]] && [[ "$BUILDKITE_DOCKER_COMPOSE_CONTAINER" != "" ]]; then
    # Compose strips dashes and underscores, so we'll remove them to match the docker container names
    COMPOSE_PROJ_NAME="buildkite"${BUILDKITE_JOB_ID//-}
    COMPOSE_COMMAND=(docker-compose -f "${BUILDKITE_DOCKER_COMPOSE_FILE:-docker-compose.yml}" -p "$COMPOSE_PROJ_NAME")

    function compose-cleanup {
      if [[ "${BUILDKITE_DOCKER_COMPOSE_LEAVE_VOLUMES:-false}" == "true" ]]; then
        REMOVE_VOLUME_FLAG=""
      else
        REMOVE_VOLUME_FLAG="-v"
      fi

      echo "~~~ Cleaning up Docker containers"

      # Send them a friendly kill
      buildkite-prompt-and-run "${COMPOSE_COMMAND[@]}" kill

      if [[ $(docker-compose --version) == *1.6* ]]; then
        # 1.6

        # There's no --all flag to remove adhoc containers
        buildkite-prompt-and-run "${COMPOSE_COMMAND[@]}" rm --force "$REMOVE_VOLUME_FLAG"

        # So now we remove the adhoc container
        COMPOSE_CONTAINER_NAME="${COMPOSE_PROJ_NAME}_${BUILDKITE_DOCKER_COMPOSE_CONTAINER}"
        buildkite-prompt-and-run docker rm -f "$REMOVE_VOLUME_FLAG" "${COMPOSE_CONTAINER_NAME}_run_1"
      else
        # 1.7+

        # `compose down` doesn't support force removing images, so we use `rm --force`
        buildkite-prompt-and-run "${COMPOSE_COMMAND[@]}" rm --force --all "$REMOVE_VOLUME_FLAG"

        # Stop and remove all the linked services and network
        buildkite-prompt-and-run "${COMPOSE_COMMAND[@]}" down
      fi
    }

    trap compose-cleanup EXIT

    # Build the Docker images using Compose, namespaced to the job
    echo "~~~ Building Docker images"

    if [[ "${BUILDKITE_DOCKER_COMPOSE_BUILD_ALL:-false}" == "true" ]]; then
      buildkite-run "${COMPOSE_COMMAND[@]}" build --pull
    else
      buildkite-run "${COMPOSE_COMMAND[@]}" build --pull "$BUILDKITE_DOCKER_COMPOSE_CONTAINER"
    fi

    # Run the build script command in the service specified in BUILDKITE_DOCKER_COMPOSE_CONTAINER
    echo "+++ $BUILDKITE_COMMAND_ACTION (in Docker Compose container)"
    buildkite-prompt-and-run "${COMPOSE_COMMAND[@]}" run "$BUILDKITE_DOCKER_COMPOSE_CONTAINER" "./$BUILDKITE_SCRIPT_PATH"

    # Capture the exit status from the build script
    export BUILDKITE_COMMAND_EXIT_STATUS=$?

  ## Standard
  else
    echo "~~~ $BUILDKITE_COMMAND_ACTION"
    echo -ne "$BUILDKITE_PROMPT "
    echo "$BUILDKITE_COMMAND_DISPLAY"
    ."/$BUILDKITE_SCRIPT_PATH"

    # Capture the exit status from the build script
    export BUILDKITE_COMMAND_EXIT_STATUS=$?

    # Reset the bootstrap.sh flags
    buildkite-flags-reset
  fi
fi

# Run the per-checkout `post-command` hook
buildkite-local-hook "post-command"

# Run the global `post-command` hook
buildkite-global-hook "post-command"

##############################################################
#
# ARTIFACTS
# Uploads and build artifacts associated with this build
#
##############################################################

if [[ "$BUILDKITE_ARTIFACT_PATHS" != "" ]]; then
  # Run the per-checkout `pre-artifact` hook
  buildkite-local-hook "pre-artifact"

  # Run the global `pre-artifact` hook
  buildkite-global-hook "pre-artifact"

  echo "~~~ Uploading artifacts"
  if [[ ! -z "${BUILDKITE_ARTIFACT_UPLOAD_DESTINATION:-}" ]] && [[ "$BUILDKITE_ARTIFACT_UPLOAD_DESTINATION" != "" ]]; then
    buildkite-prompt-and-run buildkite-agent artifact upload "$BUILDKITE_ARTIFACT_PATHS" "$BUILDKITE_ARTIFACT_UPLOAD_DESTINATION"
  else
    buildkite-prompt-and-run buildkite-agent artifact upload "$BUILDKITE_ARTIFACT_PATHS"
  fi

  # If the artifact upload fails, open the current group and exit with an error
  if [[ $? -ne 0 ]]; then
    echo "^^^ +++"
    exit 1
  fi

  # Run the per-checkout `post-artifact` hook
  buildkite-local-hook "post-artifact"

  # Run the global `post-artifact` hook
  buildkite-global-hook "post-artifact"
fi

# Be sure to exit this script with the same exit status that the users build
# script exited with.
exit $BUILDKITE_COMMAND_EXIT_STATUS
