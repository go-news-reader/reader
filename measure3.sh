#!/bin/zsh
measure() {
  local bin=$1 label=$2
  "$bin" >/dev/null 2>&1 &
  local pid=$!
  sleep 12
  local c1=$(ps -o %cpu= -p $pid 2>/dev/null | tr -d ' ')
  sleep 2
  local c2=$(ps -o %cpu= -p $pid 2>/dev/null | tr -d ' ')
  echo "$label: cpu=${c1:-DEAD}% then ${c2:-DEAD}%  (pid $pid)"
  kill $pid 2>/dev/null; wait $pid 2>/dev/null
}
measure /private/tmp/rd-build/newsreader     OLD-broken-149
sleep 2
measure /private/tmp/rd-a11y/reader-fixed     NEW-a11y-cached
