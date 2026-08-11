#!/bin/zsh
/private/tmp/rd-a11y/reader-fixed >/private/tmp/rd-a11y/run.log 2>&1 &
APP=$!
echo "launched pid=$APP"
sleep 10
echo "=== idle CPU sample (2 reads @1s, second is instantaneous) ==="
top -l 2 -s 1 -pid $APP -stats pid,cpu,state 2>/dev/null | grep -A2 "PID" | tail -3
sleep 4
echo "=== second idle sample ==="
top -l 2 -s 1 -pid $APP -stats pid,cpu,state 2>/dev/null | grep -A2 "PID" | tail -3
kill $APP 2>/dev/null
echo "=== done ==="
