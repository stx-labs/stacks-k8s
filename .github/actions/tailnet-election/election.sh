#!/usr/bin/env bash
# Usage: election.sh start|result (see action.yml), or run|row|elect|summary internally.
# Env: INDEX, SIZE, PREFIX, WINDOW, TIMEOUT, PORT, COORDINATOR, STATE.
set -euo pipefail
www="$STATE/www"
stamp() { date -u +%H:%M:%S; }
fail() { echo "$(stamp) $*" | tee "$STATE/failed" >&2; exit 1; }

# row SAMPLES: per-peer median and sample counts from "peer ms path" lines.
row() {
  jq -Rs --argjson index "$INDEX" '
    [split("\n")[] | select(. != "") | split(" ") | {peer: .[0], ms: (.[1] | tonumber), path: .[2]}]
    | group_by(.peer)
    | map((map(.ms) | sort) as $ms | {key: .[0].peer, value: {
        median: $ms[($ms | length) / 2 | floor], samples: length, relayed: map(select(.path == "relayed")) | length}})
    | {index: $index, peers: from_entries}' "$1"
}

# elect ROWS_DIR: the result from every node's row (ROWS_DIR/<index>.json) and IPS (JSON).
elect() {
  jq -s --argjson size "$SIZE" --argjson ips "$1" '
    (map({key: (.index | tostring), value: .peers}) | from_entries) as $rows
    | def rtt($a; $b): [$rows[$a][$b].median, $rows[$b][$a].median] | map(select(. != null))
        | if length == 0 then error("no round trips between \($a) and \($b)") else add / length end;
    [range(1; $size + 1) | tostring] as $nodes
    | [$nodes[] as $n | [$nodes[] | select(. != $n) | rtt($n; .)]
        | {index: ($n | tonumber), mean: (add / length), max: max}] as $scores
    | ($scores | sort_by(.mean, .max, .index) | first.index) as $leader
    | {leader: $leader, leaderIp: $ips[$leader | tostring], scores: $scores,
       ranks: ([$nodes[] | tonumber | select(. != $leader)] | to_entries | map({key: (.value | tostring), value: (.key + 1)}) | from_entries),
       rows: $rows}' "${@:2}"
}

# summary ELECTION: Markdown round-trip matrix (median ms, * when any sample was relayed).
summary() {
  jq -r --argjson size "$SIZE" '
    [range(1; $size + 1) | tostring] as $nodes
    | "### Tailnet latency election", "",
      "Median round-trip ms from row to column over the window; * marks relayed (DERP) samples.", "",
      "| From \\ To | " + ($nodes | join(" | ")) + " | Mean | Max |",
      "| --- | " + ($nodes | map("---") | join(" | ")) + " | --- | --- |",
      ($nodes[] as $n | (.scores[] | select(.index == ($n | tonumber))) as $s
        | "| " + $n + (if ($n | tonumber) == .leader then " (elected)" else "" end) + " | "
          + ([$nodes[] as $m | if $m == $n then "-" else (.rows[$n][$m]
              | if . == null then "?" else "\(.median)" + (if .relayed > 0 then "*" else "" end) end) end] | join(" | "))
          + " | \($s.mean * 10 | round / 10) | \($s.max) |"),
      ""' "$1"
}

# sample PEER IP: one disco ping as "peer ms direct|relayed", nothing on loss.
sample() {
  tailscale ping -c 1 --until-direct=false --timeout 2s "$2" 2>/dev/null |
    sed -nE "s/.* via (DERP\\([^)]*\\)|[^ ]+) in ([0-9.]+)ms.*/$1 \\2 \\1/p" |
    sed -E 's/ DERP\([^)]*\)$/ relayed/; s/ [^ ]+:[0-9]+$/ direct/' || true
}

run() {
  local deadline self i peer
  deadline=$(( $(date +%s) + TIMEOUT ))
  self="$(tailscale ip -4)"
  declare -A ips=([$INDEX]="$self")
  # A subshell detaches the server, so wait below only covers pings.
  (python3 -m http.server "$PORT" --bind "$self" --directory "$www" > /dev/null 2>&1 &)
  echo "$(stamp) node $INDEX of $SIZE at $self, serving on :$PORT"
  for (( i = 1; i <= SIZE; i++ )); do
    until [ -n "${ips[$i]:-}" ]; do
      ips[$i]="$(tailscale ip -4 "$PREFIX$i" 2>/dev/null || true)"
      [ -n "${ips[$i]}" ] || { (( $(date +%s) < deadline )) || fail "$PREFIX$i never joined"; sleep 2; }
    done
  done
  for i in "${!ips[@]}"; do printf '%s %s\n' "$i" "${ips[$i]}"; done | sort -n > "$STATE/ips.txt"
  echo "$(stamp) all $SIZE nodes present; letting direct paths form"
  for i in "${!ips[@]}"; do
    [ "$i" = "$INDEX" ] || tailscale ping -c 10 --timeout 2s "${ips[$i]}" > /dev/null 2>&1 &
  done
  wait
  echo "$(stamp) measuring for ${WINDOW}s"
  local end=$(( $(date +%s) + WINDOW ))
  : > "$STATE/samples.txt"
  while (( $(date +%s) < end )); do
    for i in "${!ips[@]}"; do
      [ "$i" = "$INDEX" ] || sample "$i" "${ips[$i]}" >> "$STATE/samples.txt" &
    done
    sleep 1
  done
  wait
  row "$STATE/samples.txt" > "$www/row.json.tmp" && mv "$www/row.json.tmp" "$www/row.json"
  echo "$(stamp) published row: $(jq -c .peers "$www/row.json")"
  local ipmap
  ipmap="$(jq -Rn '[inputs | split(" ") | {key: .[0], value: .[1]}] | from_entries' < "$STATE/ips.txt")"
  if [ "$INDEX" = "$COORDINATOR" ]; then
    mkdir -p "$STATE/rows"
    for i in "${!ips[@]}"; do
      until curl -sf --max-time 5 "http://${ips[$i]}:$PORT/row.json" -o "$STATE/rows/$i.json"; do
        (( $(date +%s) < deadline )) || fail "no row from node $i"
        sleep 2
      done
    done
    elect "$ipmap" "$STATE"/rows/*.json > "$www/election.json.tmp" && mv "$www/election.json.tmp" "$www/election.json"
  fi
  peer="${ips[$COORDINATOR]}"
  until curl -sf --max-time 5 "http://$peer:$PORT/election.json" -o "$STATE/election.json.tmp"; do
    (( $(date +%s) < deadline )) || fail "no result from coordinator $COORDINATOR"
    sleep 2
  done
  mv "$STATE/election.json.tmp" "$STATE/election.json"
  echo "$(stamp) elected node $(jq .leader "$STATE/election.json")"
}

case "${1:-}" in
  start)
    rm -rf "$STATE"
    mkdir -p "$www"
    nohup "$0" run > "$STATE/run.log" 2>&1 &
    echo "started election as node $INDEX of $SIZE"
    ;;
  run) run ;;
  result)
    started=$(date +%s)
    until [ -s "$STATE/election.json" ] || [ -e "$STATE/failed" ]; do
      (( $(date +%s) - started < TIMEOUT )) || { cat "$STATE/run.log"; echo "no election result" >&2; exit 1; }
      sleep 2
    done
    cat "$STATE/run.log"
    [ ! -e "$STATE/failed" ] || exit 1
    summary "$STATE/election.json" > "$STATE/summary.md"
    cat "$STATE/summary.md"
    jq -r --arg index "$INDEX" --arg summary "$STATE/summary.md" '
      "leader-index=\(.leader)", "leader-ip=\(.leaderIp)",
      "elected=\(.leader | tostring == $index)", "rank=\(.ranks[$index] // 0)", "summary=\($summary)"' \
      "$STATE/election.json" >> "${GITHUB_OUTPUT:-/dev/stdout}"
    ;;
  row | elect | summary) "$@" ;;
  *) echo "usage: $0 start|result" >&2; exit 1 ;;
esac
