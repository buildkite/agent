#!/bin/bash
set -e

echo '--- Install dependencies'
godep restore

echo '--- Running golint'
make lint

echo '--- Running tests'
make test
