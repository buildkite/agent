FROM golang:1.9.2
WORKDIR /go/src/github.com/buildkite/agent
COPY . .
RUN go build -i -o /go/bin/buildkite-agent github.com/buildkite/agent
CMD ["buildkite-agent", "start"]
