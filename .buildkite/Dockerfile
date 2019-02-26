FROM golang:1.12

WORKDIR /code

# cache go modules in docker cache
COPY go.mod go.sum /code/
RUN go mod download
