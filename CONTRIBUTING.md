# Contributing to the Agent

1. Fork this repo
1. Create a feature branch with a nice name (`git checkout -b my-new-feature`)
1. Write your code!
    - Make sure your code is correctly formatted by running `go tool gofumpt -extra -w .`, and that the tests are passing by running `go test ./...`
1. Commit your changes (`git commit -am 'Add some feature'`)
    - In an ideal world we have [atomic commits](https://www.pauline-vos.nl/atomic-commits/) and use [Tim Pope-style commit messages](https://tbaggery.com/2008/04/19/a-note-about-git-commit-messages.html), but so long as it's clear what's happening in your PR, that's fine. We try to not be super persnickety about these things.
1. Push to your branch (`git push origin my-new-feature`)
1. Create a pull request for your branch. Make sure that your PR has a nice description (fill in the template), and that it's linked to any relevant issues.

The agent wranglers at Buildkite will review your PR, and start a CI build for it. For security reasons, we don't automatically run CI against forked repos, and a human will review your PR prior to its CI running.

Our objective is to have no PR wait more than a week for some sort of interaction from us -- this might be a review, or it might be a "I'm going to come back to this and review it a bit later". This isn't a guarantee though, and sometimes other work might get in the way of reviewing opensource contributions. If we're really dragging our feet on reviewing a PR, please feel free to ping us through GitHub or Slack, or get in touch with support@buildkite.com, and they can bug us to get things done :)
