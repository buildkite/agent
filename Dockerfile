FROM golang:1.5

# see https://golang.org/doc/install/source#environment
# see also http://build.golang.org/
# and canonically, see defs_OS_ARCH.h files in src/runtime
#   https://go.googlesource.com/go/+/go1.4.1/src/runtime/
ENV GOLANG_CROSSPLATFORMS \
	darwin/386 darwin/amd64 \
	dragonfly/386 dragonfly/amd64 \
	freebsd/386 freebsd/amd64 freebsd/arm \
	linux/386 linux/amd64 linux/arm \
	nacl/386 nacl/amd64p32 nacl/arm \
	netbsd/386 netbsd/amd64 netbsd/arm \
	openbsd/386 openbsd/amd64 \
	plan9/386 plan9/amd64 \
	solaris/amd64 \
	windows/386 windows/amd64

# TODO gcc: error: unrecognized command line option '-marm'
#	android/arm \

# ls src/runtime/defs_*_*.h | sed -r 's!^.*/defs_([^_]+)_([^_]+)[.]h$!\1/\2!'

# (set an explicit GOARM of 5 for maximum ARM compatibility)
ENV GOARM 5

RUN cd /usr/local/go/src \
	&& set -ex \
	&& for platform in $GOLANG_CROSSPLATFORMS; do \
		GOOS=${platform%/*} \
		GOARCH=${platform##*/} \
		./make.bash --no-clean 2>&1; \
	done

# We need to Ruby to run FPM and the Homebrew update script
RUN echo "deb http://http.debian.net/debian jessie contrib" >> /etc/apt/sources.list
RUN apt-get update
RUN apt-get install -y ruby-full gcc-multilib
RUN gem install bundler

# When nokogiri installs, it calls out the `patch` command to fix some libxml
# stuffs
RUN apt-get install -y patch

# Install Golang dependencies
RUN go get github.com/golang/lint/golint
RUN go get github.com/buildkite/github-release

# Install zip which is required for releasing to GitHub
RUN apt-get install -y zip

# The golang Docker sets the $GOPATH to be /go
# https://github.com/docker-library/golang/blob/c1baf037d71331eb0b8d4c70cff4c29cf124c5e0/1.4/Dockerfile
RUN mkdir -p /go/src/github.com/buildkite/agent
WORKDIR /go/src/github.com/buildkite/agent
