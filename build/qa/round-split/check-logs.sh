#!/usr/bin/env bash
# check-logs.sh <OUT>: the lines that decide the round-split QA, per node log.
OUT="${1:?OUT}"
for f in "${OUT}"/logs/*.log; do
  n="$(basename "$f" .log)"
  echo "=== ${n} ==="
  echo "  qualified-set mismatch : $(grep -c 'header extra validators do not match qualified validators' "$f")  (must be 0)"
  echo "  invalid vrank          : $(grep -c 'invalid vrank' "$f")  (must be 0)"
  echo "  bad block records      : $(grep -ci 'bad block' "$f")"
  grep -h 'QA split: armed\|QA split: round-0 COMMIT\|QA split: dropping\|QA split: ignoring' "$f" | sed 's/^/  /' | head -12
  grep -h '\[RC\] timeoutEvent' "$f" | sed 's/^/  /' | tail -4
done
