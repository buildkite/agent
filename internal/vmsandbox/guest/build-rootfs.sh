#!/bin/bash
# Build the guest root filesystem image for the VM sandbox.
#
# Run this on the Linux host that will run the agent (it needs Docker, KVM is
# not required). It builds the agent for linux/arm64, builds the Dockerfile
# in this directory, and turns the resulting image into a sparse ext4 image
# at $SANDBOX_DIR/images/rootfs.ext4. It also fetches the pinned Firecracker
# release and guest kernel if they aren't already present.
#
# Usage: build-rootfs.sh [sandbox-dir]      (default /opt/bko-sandbox)
#
# Requires: go, docker, mkfs.ext4 (e2fsprogs), sudo (to extract the image
# with correct ownership; the result is chowned back to the caller).
set -euo pipefail

SANDBOX_DIR=${1:-/opt/bko-sandbox}
HERE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$HERE/../../.." && pwd)
ROOTFS_SIZE=${ROOTFS_SIZE:-8G}

# Pinned artefacts. Firecracker release binaries are published with sha256
# sums; the CI kernel builds aren't, so the kernel's sum was recorded from
# the first download and is checked on subsequent ones.
FC_VERSION=v1.17.0
KERNEL_VERSION=6.1.186
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/20260909-a8e1c3830545-0/aarch64/vmlinux-${KERNEL_VERSION}"
KERNEL_SHA256=85b23e1fc17848d90a7028c557b3d1ba71d676ff48ad1c357bc18f207302141a

case "$(uname -m)" in
    aarch64|arm64) ;;
    *) echo "this prototype only supports aarch64 hosts (got $(uname -m))" >&2; exit 1 ;;
esac

mkdir -p "$SANDBOX_DIR"/{bin,images,state}

if [ ! -x "$SANDBOX_DIR/bin/firecracker" ]; then
    echo "--- Fetching Firecracker $FC_VERSION"
    tmp=$(mktemp -d)
    curl -fsSL -o "$tmp/fc.tgz" \
        "https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-aarch64.tgz"
    curl -fsSL -o "$tmp/fc.tgz.sha256" \
        "https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-aarch64.tgz.sha256.txt"
    (cd "$tmp" && sed "s| .*| fc.tgz|" fc.tgz.sha256 | sha256sum -c -)
    tar -xzf "$tmp/fc.tgz" -C "$tmp"
    install -m 0755 "$tmp/release-${FC_VERSION}-aarch64/firecracker-${FC_VERSION}-aarch64" "$SANDBOX_DIR/bin/firecracker"
    rm -rf "$tmp"
fi

if [ ! -f "$SANDBOX_DIR/images/vmlinux" ]; then
    echo "--- Fetching guest kernel $KERNEL_VERSION"
    curl -fsSL -o "$SANDBOX_DIR/images/vmlinux-${KERNEL_VERSION}" "$KERNEL_URL"
    curl -fsSL -o "$SANDBOX_DIR/images/vmlinux-${KERNEL_VERSION}.config" "$KERNEL_URL.config"
    echo "$KERNEL_SHA256  $SANDBOX_DIR/images/vmlinux-${KERNEL_VERSION}" | sha256sum -c -
    ln -sf "vmlinux-${KERNEL_VERSION}" "$SANDBOX_DIR/images/vmlinux"
fi

echo "--- Building agent for linux/arm64"
mkdir -p "$HERE/build"
(cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o "$HERE/build/buildkite-agent" .)

echo "--- Building guest image"
docker build -t bko-sandbox-guest "$HERE"

echo "--- Exporting to ext4"
work=$(mktemp -d)
trap 'sudo rm -rf "$work"' EXIT
cid=$(docker create bko-sandbox-guest /sbin/init)
mkdir "$work/root"
# Ownership and setuid bits in the export matter (e.g. for sudo-less
# ping/su inside the guest), so extract as root.
docker export "$cid" | sudo tar -x -C "$work/root"
docker rm "$cid" > /dev/null
# The container runtime puts placeholder files here; the guest writes its
# own at boot.
sudo rm -f "$work/root/etc/resolv.conf" "$work/root/etc/hostname" "$work/root/etc/hosts"
sudo touch "$work/root/etc/resolv.conf" "$work/root/etc/hostname" "$work/root/etc/hosts"

img="$work/rootfs.ext4"
truncate -s "$ROOTFS_SIZE" "$img"
sudo mkfs.ext4 -q -F -d "$work/root" "$img"
sudo chown "$(id -u):$(id -g)" "$img"
mv "$img" "$SANDBOX_DIR/images/rootfs.ext4"

echo "--- Done"
ls -lsh "$SANDBOX_DIR/images/rootfs.ext4"
rm -rf "$HERE/build"
