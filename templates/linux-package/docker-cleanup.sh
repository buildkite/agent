#!/bin/bash

# Daily cron script for cleaning up old Docker images and containers to keep
# disk space under control.
#
# To set this up, add a daily crontab entry like so:
#
#   $ crontab -e
#
#   # m h dom mon dow command
#   0 0 * * * /etc/buildkite-agent/docker-cleanup.sh

# Delete all exited containers first
exited_containers=$(docker ps -aq --no-trunc --filter "status=exited")
if [[ -n "$exited_containers" ]]; then
  docker rm $exited_containers
fi

# Delete all buildkite-created images older than 1 day
old_buildkite_images=$(docker images -a | grep "buildkite.*\(days\|weeks\|months\)" | awk '{ print $1 }')
if [[ -n "$old_buildkite_images" ]]; then
  docker rmi $old_buildkite_images
fi

# Delete all dangling images
dangling_images=$(docker images --filter 'dangling=true' -q --no-trunc | sort | uniq)
if [[ -n "$dangling_images" ]]; then
  docker rmi $dangling_images
fi
