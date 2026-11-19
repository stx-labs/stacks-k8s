#!/usr/bin/env bash
# Starts or waits for a named background command. Env: PHASE (start|wait), NAME, RUN,
# TIMEOUT (seconds), STATE (directory for logs and exit statuses).
set -euo pipefail
[[ "$NAME" =~ ^[A-Za-z0-9._-]+$ ]] || { echo "invalid name: $NAME" >&2; exit 1; }
mkdir -p "$STATE"
log="$STATE/$NAME.log" status="$STATE/$NAME.status"
case "$PHASE" in
  start)
    [ -n "$RUN" ] || { echo "start needs run" >&2; exit 1; }
    rm -f "$log" "$status"
    # The job keeps processes started by earlier steps running until it ends.
    # shellcheck disable=SC2016 # Expanded by the inner shell.
    nohup bash -c 'bash -c "$0" > "$1" 2>&1; echo $? > "$2"' "$RUN" "$log" "$status" > /dev/null 2>&1 &
    echo "started $NAME"
    ;;
  wait)
    started=$(date +%s)
    until [ -s "$status" ]; do
      if (( $(date +%s) - started > TIMEOUT )); then
        cat "$log" 2>/dev/null || true
        echo "$NAME still running after ${TIMEOUT}s" >&2
        exit 1
      fi
      sleep 2
    done
    cat "$log"
    code="$(cat "$status")"
    echo "$NAME exited $code after waiting $(( $(date +%s) - started ))s"
    exit "$code"
    ;;
  *) echo "phase must be start or wait" >&2; exit 1 ;;
esac
