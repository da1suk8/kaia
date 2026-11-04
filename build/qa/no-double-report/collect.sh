#!/usr/bin/env bash
# collect.sh: write OUT/evidence.txt for one run of net.sh (nodes still running).
#   BIN=<bin dir> OUT=<run dir> [N1=cn4] [C1=cn5] ./collect.sh
# Detects "7" (C1's "QA candidate silent"), "9" ("QA drop COMMIT") and N (N1's first round-0
# proposal after "9") from the logs and the chain, then records rounds, decoded VRank, node
# states, the re-proposer of "9", N1's report-target lines and the relay-drop count.
set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NET="${HERE}/net.sh"; N1="${N1:-cn4}"; C1="${C1:-cn5}"
EV="${OUT:?}/evidence.txt"
rpc() { curl -s -m 5 -X POST -H 'Content-Type: application/json' --data "{\"jsonrpc\":\"2.0\",\"method\":\"$2\",\"params\":${3:-[]},\"id\":1}" "http://127.0.0.1:$1" 2>/dev/null || true; }
port() { echo $((${RPC_BASE:-8700} + ${1#cn})); }
n1addr="$("${NET}" addr "${N1}")"; c1addr="$("${NET}" addr "${C1}")"
h7="$(grep -h 'QA candidate silent' "${OUT}/logs/${C1}.log" | sed -nE 's/.*height=([0-9]+).*/\1/p' | head -1)"
h9="$(grep -h 'QA drop COMMIT' "${OUT}/logs/${N1}.log" | sed -nE 's/.*height=([0-9]+).*/\1/p' | head -1)"
[ -n "${h7}" ] && [ -n "${h9}" ] || { echo "scenario not reached yet (h7=${h7:-?} h9=${h9:-?})" >&2; exit 1; }
hn=""
for h in $(seq $((h9 + 1)) $((h9 + 12))); do
  op="$(rpc "$(port "${C1}")" kaia_getBlockWithConsensusInfoByNumber "[\"$(printf '0x%x' "$h")\"]" | python3 -c 'import json,sys; r=json.load(sys.stdin).get("result"); print((r or {}).get("originProposer",""))')"
  [ "$(echo "${op}" | tr A-Z a-z)" = "$(echo "${n1addr}" | tr A-Z a-z)" ] && { hn="$h"; break; }
done
[ -n "${hn}" ] || { echo "N1 has no round-0 proposal in (${h9}, $((h9 + 12))] yet" >&2; exit 1; }
{
  echo "=== $(date '+%Y-%m-%d %H:%M:%S %Z')  N1=${N1} ${n1addr}  C1=${C1} ${c1addr}"
  cat "${OUT}/run.txt"
  echo; echo "--- heights: \"7\"=${h7} \"9\"=${h9} N=${hn} (VRank epoch 120: epoch starts are multiples of 120)"
  echo; echo "--- rounds 1..$((hn + 3)) (from ${C1})"; "${NET}" rounds 1 $((hn + 3)) | cut -d' ' -f1-5
  echo; echo "--- consensus info of ${h9} (round, author=proposer, originProposer, committers)"
  rpc "$(port "${C1}")" kaia_getBlockWithConsensusInfoByNumber "[\"$(printf '0x%x' "${h9}")\"]" | python3 -c 'import json,sys; r=json.load(sys.stdin)["result"]; print(json.dumps({k:r[k] for k in ("number","round","proposer","originProposer","committers","hash")}, indent=1))'
  echo; echo "--- re-proposer of ${h9} at round 1 (${C1}'s reply to its VRankPreprepare)"
  grep -h "QA candidate reply" "${OUT}/logs/${C1}.log" | grep "height=${h9} round=1" | sed -E 's/^.{0,30}//' | cut -c1-200
  for h in $(printf '%s\n' "${h7}" "${h9}" $((h9 + 1)) "${hn}" | awk '!seen[$0]++'); do echo; echo "--- header(${h}).VRank decoded (from ${N1})"; "${NET}" vrank "${h}" "${N1}"; done
  echo; echo "--- node states per height (kaia_getNodesByState on ${C1})"; python3 "${HERE}/states.py" "$(port "${C1}")" $(seq $((h7 - 1)) $((hn + 1)))
  echo; echo "--- ${C1} (C1): silent / replies to ${N1} between ${h7} and ${hn}"
  grep -h -E 'QA (candidate silent|candidate reply)' "${OUT}/logs/${C1}.log" | sed -E 's/^.{0,30}//' | awk -v a="${h7}" -v b="${hn}" '{ if (match($0, /height=[0-9]+/)) { h = substr($0, RSTART + 7, RLENGTH - 7) + 0; if (h >= a && h <= b) print } }' | cut -c1-200
  echo; echo "--- every validator: QA drop COMMIT"; grep -H 'QA drop COMMIT' "${OUT}"/logs/cn*.log | sed -E 's#.*/logs/(cn[0-9])\.log:.{0,30}#\1 #' | cut -c1-200
  echo; echo "--- ${N1} (N1): solicits and report targets at ${h7}..${hn}"
  grep -h -E 'QA (solicit|report target)' "${OUT}/logs/${N1}.log" | sed -E 's/^.{0,30}//' | awk -v a="${h7}" -v b="${hn}" '{ if (match($0, /height=[0-9]+/)) { h = substr($0, RSTART + 7, RLENGTH - 7) + 0; if (h >= a && h <= b) print } }' | cut -c1-200
  echo; echo "--- ${N1} (N1): round change at ${h9}"; grep -h -E '\[RC\] (timeoutEvent Sent|Catch up round|Received 2f\+1)' "${OUT}/logs/${N1}.log" | cut -c1-200
  echo; echo "--- ${N1} (N1): 'Dropping PrepreparedEvent due to full relay queue' lines: $(grep -c 'Dropping PrepreparedEvent' "${OUT}/logs/${N1}.log")"
  echo "--- ${N1} (N1): process starts in the log: $(grep -c -E 'Starting peer-to-peer node|Welcome to the Kaia' "${OUT}/logs/${N1}.log") (1 = no restart); pid $(cat "${OUT}/nodes/${N1}.pid") alive: $(kill -0 "$(cat "${OUT}/nodes/${N1}.pid")" 2>/dev/null && echo yes || echo no)"
  echo; echo "--- heads"; "${NET}" status
} | tee "${EV}"
