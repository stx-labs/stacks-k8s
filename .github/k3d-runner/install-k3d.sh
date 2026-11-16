#!/usr/bin/env bash
# Installs the pinned k3d release after verifying its SHA-256.
set -euo pipefail
version=v5.9.0
case "$(uname -m)" in
  x86_64) arch=amd64 digest=06d8f25bc3a971c4eb29e0ff08429b180402db0f4dec838c9eac427e296800a0 ;;
  aarch64) arch=arm64 digest=03cde5cf23e6e8e67de5a039ecf26e5b85aca82fba3e5d13dadf904cd218a250 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
"$(dirname "$0")/../runner-lib/install-pinned.sh" k3d \
  "https://github.com/k3d-io/k3d/releases/download/$version/k3d-linux-$arch" "$digest"
k3d version
