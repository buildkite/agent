#!/bin/bash
# One-time host network setup for the VM sandbox: a tap device owned by the
# agent user, addressed as the host end of a /30, with NAT out to the
# host's default route.
#
# Needs CAP_NET_ADMIN, so run with sudo. The agent itself never needs it.
# This is not persistent across host reboots; run it again (it's idempotent).
#
# Usage: sudo setup-network.sh [agent-user] [tap-dev] [host-cidr] [guest-ip]
#   defaults:                  $SUDO_USER   bko-tap0  172.16.0.1/30 172.16.0.2
# which matches the agent's default --vm-sandbox-tap.
set -euo pipefail

USER_NAME=${1:-${SUDO_USER:?run with sudo, or pass the agent user}}
TAP=${2:-bko-tap0}
HOST_CIDR=${3:-172.16.0.1/30}
GUEST_IP=${4:-172.16.0.2}
SUBNET=$(python3 -c 'import ipaddress,sys; print(ipaddress.ip_interface(sys.argv[1]).network)' "$HOST_CIDR")

if [ ! -e "/sys/class/net/$TAP" ]; then
    ip tuntap add dev "$TAP" mode tap user "$USER_NAME"
fi
ip addr replace "$HOST_CIDR" dev "$TAP"
ip link set "$TAP" up
sysctl -qw net.ipv4.ip_forward=1

# iptables rules, added only if missing so re-running doesn't stack them.
ensure() { iptables -C "$@" 2>/dev/null || iptables -A "$@"; }
ensure_nat() { iptables -t nat -C "$@" 2>/dev/null || iptables -t nat -A "$@"; }
ensure_nat POSTROUTING -s "$SUBNET" ! -o "$TAP" -j MASQUERADE
# Docker on the host sets FORWARD's policy to DROP; allow the guest through.
ensure FORWARD -i "$TAP" -j ACCEPT
ensure FORWARD -o "$TAP" -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT

echo "tap $TAP up: host $HOST_CIDR, guest $GUEST_IP, owner $USER_NAME"
