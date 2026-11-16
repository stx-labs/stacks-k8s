#!/usr/bin/env bash
# Keeps an agent runner alive until the server job ends (its supervisor stops
# answering three times in a row) or the hold limit passes. Env: SERVER_IP.
set -euo pipefail
limit=${1:-2400}
started=$(date +%s) failures=0
while (( $(date +%s) - started < limit )); do
  if [ "$(curl -sk --max-time 5 "https://$SERVER_IP:6443/ping")" = pong ]; then
    failures=0
  else
    failures=$(( failures + 1 ))
    (( failures < 3 )) || { echo "server gone after $(( $(date +%s) - started ))s"; exit 0; }
  fi
  sleep 15
done
echo "hold limit ${limit}s reached"
