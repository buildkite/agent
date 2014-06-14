#!/bin/bash
set -e

echo '--- go version'
go version

echo '--- building packages'
./scripts/build.sh
