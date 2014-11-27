#!/bin/bash
set -e

echo '--- downloading debian packages'
buildbox-artifact download "pkg/*.deb" .
