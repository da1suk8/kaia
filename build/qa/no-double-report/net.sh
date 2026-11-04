#!/usr/bin/env bash
# Local network for the RC4 "No double report after a re-proposal (#1104)" QA (kaiachain/kaia issue 990).
# Five CNs from the RC3 fixture of issue 990 (issuecomment-6333767584): cn1-cn4 validators,
# cn5 = C1 under testing (CandTesting) from genesis, policy 2 (WeightedRandom + Randao), every
# fork at 0, VRank epoch 120, PFS/CFS threshold 100. Static full mesh, no discovery.
# Ports: p2p P2P_BASE+1..5 (default 34001-34005), RPC RPC_BASE+1..5 (default 8701-8705).
#
#   BIN=<dir with kcn homi vrankdump> OUT=<run dir> [FIXTURE=<dir, default OUT/fixture>] ./net.sh <command>
#
#   fixture             homi (KAIA_QA_CANDIDATE_LAST=1) + jq adjustments into FIXTURE (wipes it)
#   start               init cn1-5 from FIXTURE into OUT and launch them with the QA env
#   stop                stop every node
#   status              head of every node
#   rounds A [B]        per height: committing round, round proposer, round-0 proposer, author (cn5's view)
#   vrank N [node]      decoded header(N).VRank (default: from cn1)
#   states N            kaia_getNodesByState at N (all states)
#   addr cnN            validator address of cnN
#
# QA env (same on every node; the roles follow from the chain):
#   QA_TARGET  N1 (cnN or an address, default cn4)        -> KAIA_QA_TARGET_PROPOSER
#   QA_START   first height that may be "7" (default 9)   -> KAIA_QA_START_HEIGHT
#              The Randao schedule is fixed by the genesis, so pick QA_START such that N1 also
#              proposes "9"+1: N1 is paused from the state of "9"+1 on (any PFS > 0 pauses), so
#              "9"+1 is its only possible next proposal in the epoch. With the genesis this script
#              generates, cn4 proposes 9, 15 and 16, so the defaults give "7" = 9, "9" = 15, N = 16.
#   KAIA_QA_TRACE=1 is always set.
set -euo pipefail
BIN="${BIN:?BIN=<dir with kcn homi vrankdump>}"
OUT="${OUT:?OUT=<run dir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FIXTURE="${FIXTURE:-${OUT}/fixture}"
CN_NUM=5; NETWORK_ID=9901031; VRANK_EPOCH=120
P2P_BASE="${P2P_BASE:-34000}"; RPC_BASE="${RPC_BASE:-8700}"
RPC_API="kaia,eth,net,governance,istanbul,debug,txpool,admin"
VERBOSITY="${VERBOSITY:-3}"
GCMODE="${GCMODE:-archive}"      # archive: kaia_getNodesByState works for every past height
QA_START="${QA_START:-9}"

need_bin() { for b in kcn homi; do [ -x "${BIN}/${b}" ] || { echo "missing ${BIN}/${b}" >&2; exit 1; }; done; }
rpc() { curl -s -m 5 -X POST -H 'Content-Type: application/json' --data "{\"jsonrpc\":\"2.0\",\"method\":\"$2\",\"params\":${3:-[]},\"id\":1}" "http://127.0.0.1:$1" 2>/dev/null || true; }
rpc_port() { echo $((RPC_BASE + ${1#cn})); }
p2p_port() { echo $((P2P_BASE + ${1#cn})); }
addr_of() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["Address"])' "${FIXTURE}/cn0${1#cn}/validator"; }
hex2dec() { python3 -c 'import sys; print(int(sys.argv[1], 16))' "$1"; }

fixture() {
  need_bin; rm -rf "${FIXTURE}"
  KAIA_QA_CANDIDATE_LAST=1 "${BIN}/homi" setup --gen-type deploy --cn-num "${CN_NUM}" --validators-num "${CN_NUM}" \
    --mnemonic "test junk" --chainID "${NETWORK_ID}" --mainnet-test --vrank-epoch "${VRANK_EPOCH}" \
    --randao-compatible-blocknumber 0 --permissionless-compatible-blocknumber 0 \
    --pfs-threshold 100 --cfs-threshold 100 --output "${FIXTURE}" >/dev/null
  patch_genesis
}

# Same adjustments as the RC3 comment: policy 2, subgroup 4, explicit KIP-71 settings.
patch_genesis() {
  jq '
    .config.istanbul |= (.policy = 2 | .sub = 4) |
    .config.governance.kip71 = {
      lowerboundbasefee:25000000000, upperboundbasefee:750000000000,
      gastarget:30000000, maxblockgasusedforbasefee:60000000,
      basefeedenominator:20
    }
  ' "${FIXTURE}/common/genesis.json" > "${FIXTURE}/genesis.json"
  jq -c '{policy:.config.istanbul.policy, sub:.config.istanbul.sub, vrankEpoch:.config.vrankEpoch, permissionless:.config.permissionlessCompatibleBlock, randao:.config.randaoCompatibleBlock, kip71:.config.governance.kip71}' "${FIXTURE}/genesis.json"
  for i in $(seq 1 "${CN_NUM}"); do echo "cn${i} $(addr_of "cn${i}")"; done
}

# static-nodes.json with the five CNs on their local ports
write_peers() {
  python3 - "${FIXTURE}/common/static-nodes.json" "$1" "${P2P_BASE}" <<'PYEOF'
import json, re, sys
src, dest, base = sys.argv[1], sys.argv[2], int(sys.argv[3])
peers = [re.sub(r'@[0-9.]+:\d+', f'@127.0.0.1:{base + i}', e) for i, e in enumerate(json.load(open(src)), 1)]
json.dump(peers, open(dest, 'w'), indent=2)
PYEOF
}

init_node() {
  local n="$1" dir="${OUT}/nodes/$1"; mkdir -p "${dir}"
  write_peers "${dir}/static-nodes.json"; cp "${dir}/static-nodes.json" "${dir}/trusted-nodes.json"
  "${BIN}/kcn" --datadir "${dir}" init "${FIXTURE}/genesis.json" >/dev/null 2>&1
}

launch() {
  local n="$1" i="${1#cn}" dir="${OUT}/nodes/$1"
  mkdir -p "${OUT}/logs"
  KAIA_QA_TARGET_PROPOSER="${QA_TARGET}" KAIA_QA_START_HEIGHT="${QA_START}" KAIA_QA_TRACE=1 \
  nohup "${BIN}/kcn" --datadir "${dir}" --nodekeyhex "$(cat "${FIXTURE}/cn0${i}/nodekey")" --rewardbase "0x$(cat "${FIXTURE}/keys/reward${i}")" \
    --networkid "${NETWORK_ID}" --port "$(p2p_port "${n}")" --nodiscover --rpc --rpcaddr 127.0.0.1 --rpcport "$(rpc_port "${n}")" \
    --rpcapi "${RPC_API}" --rpcvhosts '*' --ipcdisable --syncmode full --gcmode "${GCMODE}" --verbosity "${VERBOSITY}" --ntp.disable \
    >> "${OUT}/logs/${n}.log" 2>&1 &
  echo $! > "${OUT}/nodes/${n}.pid"
  echo "${n} $(addr_of "${n}") p2p=$(p2p_port "${n}") rpc=$(rpc_port "${n}") pid=$(cat "${OUT}/nodes/${n}.pid")"
}

start() {
  need_bin; [ -f "${FIXTURE}/genesis.json" ] || { echo "run fixture first (${FIXTURE}/genesis.json missing)" >&2; exit 1; }
  QA_TARGET="${QA_TARGET:-cn4}"; case "${QA_TARGET}" in cn[1-5]) QA_TARGET="$(addr_of "${QA_TARGET}")" ;; esac
  mkdir -p "${OUT}/nodes" "${OUT}/logs"
  { echo "binary: ${BIN}/kcn ($("${BIN}/kcn" version | head -1))"; echo "fixture: ${FIXTURE}"
    echo "env (every node): KAIA_QA_TARGET_PROPOSER=${QA_TARGET} KAIA_QA_START_HEIGHT=${QA_START} KAIA_QA_TRACE=1"
    echo "started: $(date '+%Y-%m-%d %H:%M:%S %Z')"; } | tee "${OUT}/run.txt"
  for i in $(seq 1 "${CN_NUM}"); do init_node "cn${i}"; done
  # C1 first, then the validators back to back; all peers are static, so block 1 should need no round change
  launch cn5 | tee -a "${OUT}/run.txt"
  for i in 1 2 3 4; do launch "cn${i}" | tee -a "${OUT}/run.txt"; done
}

stop() { for p in "${OUT}"/nodes/*.pid; do [ -f "$p" ] || continue; kill "$(cat "$p")" 2>/dev/null || true; rm -f "$p"; echo "stopped $(basename "$p" .pid)"; done; }

status() { for i in $(seq 1 "${CN_NUM}"); do r="$(rpc "$(rpc_port "cn${i}")" kaia_blockNumber | sed -nE 's/.*"result":"([^"]*)".*/\1/p')"; printf 'cn%s :%s %s\n' "$i" "$(rpc_port "cn${i}")" "${r:+$(hex2dec "$r")}"; done; }

# rounds A [B]: one line per height from cn5 (the non-validator view): round, round proposer, round-0 proposer, author
rounds() {
  local a="$1" b="${2:-$1}" port; port="$(rpc_port cn5)"
  python3 - "${port}" "$a" "$b" "$(for i in $(seq 1 "${CN_NUM}"); do printf '%s=cn%s ' "$(addr_of "cn${i}")" "$i"; done)" <<'PYEOF'
import json, sys, urllib.request
port, a, b, names = int(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3]), dict(kv.split('=') for kv in sys.argv[4].split())
def call(m, p):
    req = urllib.request.Request(f'http://127.0.0.1:{port}', json.dumps({'jsonrpc':'2.0','id':1,'method':m,'params':p}).encode(), {'Content-Type':'application/json'})
    return json.load(urllib.request.urlopen(req, timeout=5))['result']
nm = lambda a: names.get((a or '').lower(), a)
print('height round roundProposer round0Proposer author(header) vrank')
for n in range(a, b + 1):
    r = call('kaia_getBlockWithConsensusInfoByNumber', [hex(n)])
    if r is None: print(n, 'not yet'); break
    h = call('kaia_getHeaderByNumber', [hex(n)])
    print(n, int(r['round'], 16) if isinstance(r['round'], str) else r['round'], nm(r['proposer']), nm(r.get('originProposer')), nm(h.get('proposer') or h.get('miner')), h.get('vrank'))
PYEOF
}

vrank_of() { [ -x "${BIN}/vrankdump" ] || { echo "missing ${BIN}/vrankdump" >&2; exit 1; }; "${BIN}/vrankdump" "http://127.0.0.1:$(rpc_port "${2:-cn1}")" "$1"; }
states() { rpc "$(rpc_port cn5)" kaia_getNodesByState "[\"$(printf '0x%x' "$1")\", []]" | python3 -c 'import json,sys; r=json.load(sys.stdin); print(json.dumps(r.get("result", r), indent=1))'; }

case "${1:-}" in
  fixture) fixture ;; start) start ;; stop) stop ;; status) status ;;
  rounds) rounds "${2:?A}" "${3:-}" ;; vrank) vrank_of "${2:?N}" "${3:-cn1}" ;; states) states "${2:?N}" ;; addr) addr_of "${2:?cnN}" ;;
  *) sed -n '2,26p' "${BASH_SOURCE[0]}"; exit 1 ;;
esac
