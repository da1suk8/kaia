#!/usr/bin/env bash
# Local network for the RC3 "round split at height H" QA (kaiachain/kaia issue 990).
# 4 genesis CNs in a static full mesh (no discovery), 2 ENs that follow the chain, and
# a third EN added later that full-syncs from genesis. Proposer policy 2 (WeightedRandom
# + Randao, as on Mainnet) so that the proposer of H+1 is independent of the round-1
# proposer of H; the Randao fork is at 0 in the homi genesis.
#
#   BIN=<dir with kcn ken homi> OUT=<datadir> ./round-split-net.sh <command>
#
#   generate                 homi genesis + keys (wipes OUT)
#   start                    init and start cn1-4 and en1-2
#   stop                     stop everything
#   status                   block number of every node
#   addr cnN                 validator address of cnN
#   env cnN KEY=VAL ...      write OUT/env/cnN.env (read by start / restart-cns)
#   env-clear                remove every env file
#   restart-cns              stop cn1-4, start them again with the current env files
#   add-en enN               init + start a fresh EN from genesis (static peers = cn1-4)
#   stop-node name           stop one node (cnN / enN)
#   start-node name          start one node again (no init)
set -euo pipefail
BIN="${BIN:?BIN=<dir with kcn ken homi>}"
OUT="${OUT:?OUT=<datadir>}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CN_NUM=4; EN_NUM=2; NETWORK_ID=9999
RPC_API="kaia,eth,net,governance,istanbul,debug,txpool,admin"
PORT_OFFSET="${PORT_OFFSET:-0}"           # run a second network beside the first (status / logs only; compare-nodes.py assumes offset 0)
CN_P2P_BASE=$((32322 + PORT_OFFSET)); CN_RPC_BASE=$((8560 + PORT_OFFSET)); EN_P2P_BASE=$((32350 + PORT_OFFSET)); EN_RPC_BASE=$((8550 + PORT_OFFSET))
VRANK_EPOCH="${VRANK_EPOCH:-64}"   # 64 % floor(64/8) == 0: safe from the vrank checkpoint-seed bug
POLICY="${POLICY:-2}"              # istanbul proposer policy: 2 = WeightedRandom (+ Randao at 0, as on Mainnet); 0 = RoundRobin for the variant check
PL_FORK="${PL_FORK:-64}"           # permissionless fork = VRANK_EPOCH: a fork that is not a multiple of the epoch stalls at the first epoch boundary (GetCFS reads pre-fork blocks); must also be > 1 so the block-1 start-up round change does not pause that proposer
EN_SLOTS="${EN_SLOTS:-8}"           # EN side: CN slots (default 2 would reject a 3rd static CN... see soak finding 2)
CN_EN_SLOTS="${CN_EN_SLOTS:-8}"     # CN side: EN slots; the default 3 accepts only 2 inbound ENs (exceedsPeerTarget keeps one for a dialed peer)
VERBOSITY="${VERBOSITY:-3}"

need_bin() { for b in kcn ken homi; do [ -x "${BIN}/${b}" ] || { echo "missing ${BIN}/${b}" >&2; exit 1; }; done; }
rpc() { curl -s -m 5 -X POST -H 'Content-Type: application/json' --data "{\"jsonrpc\":\"2.0\",\"method\":\"$2\",\"params\":${3:-[]},\"id\":1}" "http://127.0.0.1:$1" 2>/dev/null || true; }
addr_of() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["Address"])' "${OUT}/keys/validator${1#cn}"; }

generate() {
  need_bin; rm -rf "${OUT}"
  "${BIN}/homi" setup --cn-num "${CN_NUM}" --en-num "${EN_NUM}" --test-num 4 --chainID "${NETWORK_ID}" \
    --governance --gov-mode single --reward-minimum-stake 5000000 --permissionless-compatible-blocknumber "${PL_FORK}" \
    --vrank-epoch "${VRANK_EPOCH}" --pfs-threshold 2 --cfs-threshold 5 --ist-proposer-policy "${POLICY}" \
    --alloc-permissionless-prerequisites -o "${OUT}" local   # ABv2 + Registry records in genesis for a delayed fork
  python3 "${HERE}/patch-genesis.py" "${OUT}/scripts/genesis.json"
  python3 - "${OUT}/scripts/genesis.json" "${POLICY}" <<'PYEOF'
import json, sys
c = json.load(open(sys.argv[1]))["config"]
print("policy", c["istanbul"]["policy"], "randao", c.get("randaoCompatibleBlock"), "permissionless", c.get("permissionlessCompatibleBlock"), "vrankEpoch", c.get("vrankEpoch"))
assert c["istanbul"]["policy"] == int(sys.argv[2]) and c.get("randaoCompatibleBlock") == 0, "policy must match POLICY, Randao at 0"
assert c.get("permissionlessCompatibleBlock", 0) > 1, "the fork must come after block 1 (start-up round change)"
assert c.get("permissionlessCompatibleBlock", 0) % c.get("vrankEpoch", 1) == 0, "the fork must be a multiple of vrankEpoch: GetCFS/GetPFS read pre-fork blocks otherwise and the chain stalls at the first epoch boundary"
PYEOF
  mkdir -p "${OUT}/env"
  for i in $(seq 1 "${CN_NUM}"); do echo "cn${i} $(addr_of "cn${i}")"; done
}

# static-nodes.json with the genesis CNs on their local ports (+ EXTRA_KNIS lines)
write_peers() {
  python3 - "${OUT}/scripts/static-nodes.json" "$1" "${CN_NUM}" "${CN_P2P_BASE}" <<'PYEOF'
import json, os, re, sys
src, dest, cn_num, base = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
peers = [re.sub(r'@0\.0\.0\.0:\d+', f'@127.0.0.1:{base + i}', e) for i, e in enumerate(json.load(open(src))[:cn_num], 1)]
peers += [l.strip() for l in os.environ.get('EXTRA_KNIS', '').splitlines() if l.strip()]
json.dump(peers, open(dest, 'w'), indent=2)
PYEOF
}

launch_cn() { # launch_cn <i>; env from OUT/env/cn<i>.env
  local i="$1" dir="${OUT}/nodes/cn$1" envfile="${OUT}/env/cn$1.env"
  mkdir -p "${OUT}/logs"
  (
    if [ -f "${envfile}" ]; then set -a; . "${envfile}"; set +a; fi
    nohup "${BIN}/kcn" --datadir "${dir}" --nodekeyhex "$(cat "${OUT}/keys/nodekey${i}")" --rewardbase "0x$(cat "${OUT}/keys/reward${i}")" \
      --networkid "${NETWORK_ID}" --port $((CN_P2P_BASE + i)) --nodiscover --rpc --rpcaddr 127.0.0.1 --rpcport $((CN_RPC_BASE + i)) \
      --rpcapi "${RPC_API}" --rpcvhosts '*' --rpccorsdomain '*' --ipcdisable --syncmode full --verbosity "${VERBOSITY}" --ntp.disable \
      --reserved-cross-type-slots "${CN_EN_SLOTS}" >> "${OUT}/logs/cn${i}.log" 2>&1 &
    echo $! > "${OUT}/nodes/cn${i}.pid"
  )
  echo "cn${i} rpc=$((CN_RPC_BASE + i)) p2p=$((CN_P2P_BASE + i)) pid=$(cat "${OUT}/nodes/cn${i}.pid") env=$([ -f "${envfile}" ] && tr '\n' ' ' < "${envfile}" || echo none)"
}

launch_en() { # launch_en <i>
  local i="$1" dir="${OUT}/nodes/en$1"
  mkdir -p "${OUT}/logs"
  nohup "${BIN}/ken" --datadir "${dir}" --networkid "${NETWORK_ID}" --port $((EN_P2P_BASE + i)) --nodiscover --rpc --rpcaddr 127.0.0.1 --rpcport $((EN_RPC_BASE + i)) \
    --rpcapi "${RPC_API}" --rpcvhosts '*' --rpccorsdomain '*' --ipcdisable --syncmode full --verbosity "${VERBOSITY}" --ntp.disable \
    --gcmode archive --reserved-cross-type-slots "${EN_SLOTS}" >> "${OUT}/logs/en${i}.log" 2>&1 &
  echo $! > "${OUT}/nodes/en${i}.pid"; echo "en${i} rpc=$((EN_RPC_BASE + i)) pid=$(cat "${OUT}/nodes/en${i}.pid")"
}

init_node() { # init_node cnN|enN
  local name="$1" dir="${OUT}/nodes/$1"; mkdir -p "${dir}"
  write_peers "${dir}/static-nodes.json"
  case "${name}" in
    cn*) cp "${dir}/static-nodes.json" "${dir}/trusted-nodes.json"; "${BIN}/kcn" --datadir "${dir}" init "${OUT}/scripts/genesis.json" >/dev/null 2>&1 ;;
    en*) "${BIN}/ken" --datadir "${dir}" init "${OUT}/scripts/genesis.json" >/dev/null 2>&1 ;;
  esac
}

start() {
  need_bin; [ -d "${OUT}/keys" ] || { echo "run generate first" >&2; exit 1; }
  mkdir -p "${OUT}/nodes" "${OUT}/logs" "${OUT}/env"
  # init everything first, then launch the CNs back to back: a CN that proposes block 1 before
  # its peers are up times out, and after the fork that failure would pause it (#20)
  for i in $(seq 1 "${CN_NUM}"); do init_node "cn${i}"; done
  for i in $(seq 1 "${EN_NUM}"); do init_node "en${i}"; done
  for i in $(seq 1 "${CN_NUM}"); do launch_cn "${i}"; done
  for i in $(seq 1 "${EN_NUM}"); do launch_en "${i}"; done
}

stop_node() { local p="${OUT}/nodes/$1.pid"; [ -f "$p" ] && { kill "$(cat "$p")" 2>/dev/null || true; rm -f "$p"; echo "stopped $1"; } || echo "$1 not running"; }
wait_down() { for _ in $(seq 1 30); do rpc "$1" kaia_blockNumber | grep -q result || return 0; sleep 1; done; return 1; }
stop() { for f in "${OUT}"/nodes/*.pid; do [ -f "$f" ] || continue; stop_node "$(basename "$f" .pid)"; done; }

restart_cns() {
  for i in $(seq 1 "${CN_NUM}"); do stop_node "cn${i}"; done
  for i in $(seq 1 "${CN_NUM}"); do wait_down $((CN_RPC_BASE + i)) || echo "cn${i} still answering" >&2; done
  sleep 2
  for i in $(seq 1 "${CN_NUM}"); do launch_cn "${i}"; done
}

# A third EN is refused by every CN with "too many peers" unless the CNs run with a larger
# --reserved-cross-type-slots (set above); if an EN still has 0 peers, add it from a CN with admin_addPeer.
add_en() { local i="${1#en}"; [ "${i}" -gt "${EN_NUM}" ] || { echo "en${i} is a genesis EN; use start-node" >&2; exit 1; }; init_node "en${i}"; launch_en "${i}"; }

start_node() { case "$1" in cn*) launch_cn "${1#cn}" ;; en*) launch_en "${1#en}" ;; esac; }

write_env() { local name="$1"; shift; mkdir -p "${OUT}/env"; printf '%s\n' "$@" > "${OUT}/env/${name}.env"; echo "${OUT}/env/${name}.env:"; cat "${OUT}/env/${name}.env"; }

status() {
  for i in $(seq 1 "${CN_NUM}"); do printf "cn%s :%s %s\n" "$i" $((CN_RPC_BASE + i)) "$(rpc $((CN_RPC_BASE + i)) kaia_blockNumber | sed -E 's/.*"result":"([^"]*)".*/\1/' | xargs -I{} printf '%d' {} 2>/dev/null)"; done
  for i in 1 2 3; do printf "en%s :%s %s\n" "$i" $((EN_RPC_BASE + i)) "$(rpc $((EN_RPC_BASE + i)) kaia_blockNumber | sed -E 's/.*"result":"([^"]*)".*/\1/' | xargs -I{} printf '%d' {} 2>/dev/null)"; done
}

case "${1:-}" in
  generate) generate ;; start) start ;; stop) stop ;; status) status ;;
  addr) addr_of "${2:?cnN}" ;; env) n="${2:?cnN}"; shift 2; write_env "$n" "$@" ;; env-clear) rm -f "${OUT}"/env/*.env; echo cleared ;;
  restart-cns) restart_cns ;; add-en) add_en "${2:?enN}" ;;
  stop-node) stop_node "${2:?name}" ;; start-node) start_node "${2:?name}" ;;
  *) sed -n '2,20p' "${BASH_SOURCE[0]}"; exit 1 ;;
esac
