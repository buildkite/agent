#!/usr/bin/env bash

set -Eeufo pipefail

# When it comes time to run Bazel within a pipeline, this script will be ready to do so.

# Run all tests
bazelisk test //...
