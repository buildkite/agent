#!/usr/bin/env bash

indir=$1
outdir=$2
binary_name=$3

for os in "darwin" "linux" "windows"; do
  for arch in "amd64" "arm64"; do
    echo "Building test binary for $os/$arch"
    GOOS=$os GOARCH=$arch go build -o "$outdir/$binary_name-$os-$arch" "$indir"
  done
done
