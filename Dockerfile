FROM golang:cross

# We need to Ruby to run FPM
RUN echo "deb http://http.debian.net/debian jessie contrib" >> /etc/apt/sources.list
RUN apt-get update
RUN apt-get install -y ruby
RUN gem install bundler

# Install Golang dependencies
RUN go get github.com/tools/godep
RUN go get github.com/golang/lint/golint

# The golang Docker sets the $GOPATH to be /go
# https://github.com/docker-library/golang/blob/c1baf037d71331eb0b8d4c70cff4c29cf124c5e0/1.4/Dockerfile
RUN mkdir -p /go/src/github.com/buildkite/agent
WORKDIR /go/src/github.com/buildkite/agent
