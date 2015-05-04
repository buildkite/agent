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

# Delete all exited containers
docker rm $(docker ps -aq --no-trunc --filter "status=exited") || echo "No finished containers to remove"

# Delete buildkite* images older than 1 day
docker rmi $(docker images -a | grep "buildkite.*\(days\|weeks\|months\)" | awk '{ print $3 }') || echo "No old buildkite* images to rmi"

# Delete dangling images
docker rmi $(docker images --filter 'dangling=true' -q --no-trunc) || echo "No dangling images to rmi"
