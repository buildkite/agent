#!/bin/bash
set -e
set -x

echo "sleepy time $BUILDBOX_COMIMT"
sleep 1
echo 'hi'
sleep 1
echo 'FINISHED'

exit 123

if [ ! -d ".git" ]; then
  git clone "$BUILDBOX_REPO" . -q
fi

echo '--- preparing git'

git clean -fdq
git fetch -q
git reset --hard origin/master # always reset back to master
git checkout -qf "$BUILDBOX_COMMIT"

echo '--- bundling'

bundle install
cp .env-sample .env

echo '--- preparing database'

./bin/rake db:schema:load

echo '--- running specs'

./bin/rspec

# We're all done if we're not on master
if [ "$BUILDBOX_BRANCH" != "master" ]
then
  echo "Skipping deploy for the $BUILDBOX_BRANCH branch."
  exit 0
fi

COMMIT=$BUILDBOX_COMMIT ./bin/deploy
