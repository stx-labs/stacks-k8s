#!/usr/bin/env bash
# Usage: install-pinned.sh NAME URL SHA256
# Downloads a release binary, verifies its SHA-256 and installs it as /usr/local/bin/NAME.
set -euo pipefail
name=$1 url=$2 digest=$3
dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT
curl -fsSLo "$dir/$name" "$url"
printf '%s  %s\n' "$digest" "$dir/$name" | sha256sum -c -
sudo install -m 0755 "$dir/$name" "/usr/local/bin/$name"
