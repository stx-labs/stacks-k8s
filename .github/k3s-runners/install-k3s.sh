#!/usr/bin/env bash
# Installs the pinned k3s release after verifying its SHA-256.
set -euo pipefail
version=v1.37.0%2Bk3s1
case "$(uname -m)" in
  x86_64) asset=k3s digest=39eed8f53f277497dfc2542f66eab0ed68a94dfc598946dbebfb50366916c7a2 ;;
  aarch64) asset=k3s-arm64 digest=9bc2c128a597bf7c10ee45df844f2838251ba53806f7673d7fea24a6cb1b6a99 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
"$(dirname "$0")/../runner-lib/install-pinned.sh" k3s \
  "https://github.com/k3s-io/k3s/releases/download/$version/$asset" "$digest"
k3s --version
