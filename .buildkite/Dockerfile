FROM golang:1.11

WORKDIR /code

# cache go modules in docker cache
COPY go.mod go.sum /code/
RUN go mod download
