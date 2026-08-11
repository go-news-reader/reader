#!/bin/zsh
measure() {
  local bin=$1 label=$2
  "$bin" >/dev/null 2>&1 &
  local pid=$!
  sleep 12
  local c1=$(ps -o %cpu= -p $pid 2>/dev/null | tr -d ' ')
  sleep 2
  local c2=$(ps -o %cpu= -p $pid 2>/dev/null | tr -d ' ')
  local st=$(ps -o state= -p $pid 2>/dev/null | tr -d ' ')
  echo "$label: cpu=$c1% then $c2%  state=$st  (pid $pid)"
  kill $pid 2>/dev/null; wait $pid 2>/dev/null
}
measure /private/tmp/rd-build/reader-fixed OLD-broken 2>/dev/null || echo "OLD: no binary at rd-build"
measure /private/tmp/rd-a11y/reader-fixed NEW-cached
