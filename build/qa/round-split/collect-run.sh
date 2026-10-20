#!/usr/bin/env bash
# collect-run.sh <label>: wait for the split to arm, wait for H+2 on every CN, then run the checks.
# Writes OUT/../runs/<label>/ (compare, vrankdump on every CN for H+1, log check, raw logs).
set -u
OUT="${OUT:?}"; HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"; LABEL="${1:?label}"; MIN="${2:-0}"; DEST="${HERE}/runs/${LABEL}"; mkdir -p "${DEST}"
rpcnum() { curl -s -m 3 -X POST -H 'Content-Type: application/json' --data '{"jsonrpc":"2.0","method":"kaia_blockNumber","params":[],"id":1}' "http://127.0.0.1:$1" | sed -E 's/.*"result":"([^"]*)".*/\1/' | xargs -I{} printf '%d' {} 2>/dev/null; }
# the logs keep earlier runs: take the latest armed height at or above MIN
armed_h() { grep -h "QA split: armed" "${OUT}"/logs/cn*.log 2>/dev/null | sed -E 's/.*H=([0-9]+).*/\1/' | sort -n | tail -1; }
H=""
for _ in $(seq 1 120); do h=$(armed_h); if [ -n "$h" ] && [ "$h" -ge "${MIN}" ]; then H="$h"; break; fi; sleep 5; done
[ -n "${H}" ] || { echo "not armed at or above ${MIN} within 10 minutes"; exit 1; }
ARMED=$(grep -h "QA split: armed" "${OUT}"/logs/cn*.log | grep -c "H=${H} ")
echo "armed H=${H}; armed lines for it: ${ARMED}"
# every CN must arm at the same H; a node that comes back from a restart after the round-0 PRE-PREPARE never arms,
# accepts the holder's block, and the network splits 2 + 2 (seen 2026-10-20 with MIN_HEIGHT = restart height + 1)
[ "${ARMED}" -eq 4 ] || echo "!!! PARTIAL ARMING: ${ARMED}/4 CNs armed at H=${H}; the fault is not applied on every node, expect a stall. Recover (env-clear, restart-cns) and rerun with MIN_HEIGHT >= head + 25."
for _ in $(seq 1 60); do ok=1; for p in 8561 8562 8563 8564; do n=$(rpcnum $p); [ -n "$n" ] && [ "$n" -ge $((H + 2)) ] || ok=0; done; [ $ok = 1 ] && break; sleep 3; done
echo "heights: cn1=$(rpcnum 8561) cn2=$(rpcnum 8562) cn3=$(rpcnum 8563) cn4=$(rpcnum 8564) en1=$(rpcnum 8551) en2=$(rpcnum 8552)"
{
  echo "H=${H}"; grep -h "QA split: armed" "${OUT}"/logs/cn*.log | grep "H=${H} " | cut -c1-420
  echo; echo "##### compare-nodes.py ${H}"; python3 "${HERE}/compare-nodes.py" "${H}"
  for p in 8561 8562 8563 8564; do echo; echo "##### vrankdump :$p $((H + 1))"; "${VRANKDUMP:-${HERE}/bin/vrankdump}" "http://127.0.0.1:$p" $((H + 1)); done
  echo; echo "##### check-logs"; "${HERE}/check-logs.sh" "${OUT}"
} > "${DEST}/result.txt" 2>&1
cp "${OUT}"/logs/*.log "${DEST}/" 2>/dev/null; cp -r "${OUT}/env" "${DEST}/" 2>/dev/null
echo "written ${DEST}/result.txt"; cat "${DEST}/result.txt"
