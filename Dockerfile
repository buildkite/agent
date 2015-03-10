FROM golang:cross

# Install buildkite-agent
RUN C=1 BETA=true bash -c "`curl -sL https://raw.githubusercontent.com/buildkite/agent/master/install.sh`"
RUN ln -s /root/.buildkite/bin/buildkite-agent /usr/local/bin/buildkite-agent

# We need to Ruby to run FPM and the Homebrew update script
RUN echo "deb http://http.debian.net/debian jessie contrib" >> /etc/apt/sources.list
RUN apt-get update
RUN apt-get install -y ruby-full
RUN gem install bundler

# When nokogiri installs, it calls out the `patch` command to fix some libxml
# stuffs
RUN apt-get install -y patch

# Install Golang dependencies
RUN go get github.com/tools/godep
RUN go get github.com/golang/lint/golint
RUN go get github.com/buildkite/github-release

# Install zip which is required for releasing to GitHub
RUN apt-get install -y zip

# The golang Docker sets the $GOPATH to be /go
# https://github.com/docker-library/golang/blob/c1baf037d71331eb0b8d4c70cff4c29cf124c5e0/1.4/Dockerfile
RUN mkdir -p /go/src/github.com/buildkite/agent
WORKDIR /go/src/github.com/buildkite/agent
