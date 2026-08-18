#!/usr/bin/env bash
# Compatibility smoke test: KEN connects to existing Mainnet ENs.
#
# Uses KEN's built-in Mainnet bootstrap nodes. It deliberately does not pass
# --bootnodes and does not configure static or trusted peers, so any EN shown
# by `status` exercised normal peer admission.

set -euo pipefail

readonly ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly KEN_BIN="$ROOT_DIR/build/bin/ken"
readonly TEST_DIR="/tmp/kaia-ken-mainnet-en-smoke"
readonly P2P_PORT=34323
readonly SUBPORT=34324
readonly RPC_PORT=8651
readonly LOG_DIR="$TEST_DIR/logs"
readonly LOG_FILE="$LOG_DIR/ken.log"
readonly PID_FILE="$TEST_DIR/ken.pid"
readonly RPC_URL="http://127.0.0.1:$RPC_PORT"

usage() {
	cat <<USAGE
Usage:
  $0 clean   Stop this test's KEN and delete only $TEST_DIR.
  $0 start   Start a fresh KEN using the built-in Mainnet bootstrap nodes.
  $0 wait    Wait up to 10 minutes for a dynamic Mainnet EN connection.
  $0 status  Show dynamic, non-static, non-trusted Mainnet EN peers.
  $0 assert  Succeed only when at least one such EN peer is connected.
  $0 stop    Stop only the KEN process started by this script.

Fixed test settings:
  datadir:  $TEST_DIR/data
  P2P:      $P2P_PORT / $SUBPORT (multichannel)
  RPC:      $RPC_URL

Run 'clean' before every independent test. 'start' waits until local RPC is ready.
USAGE
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "Required command not found: $1" >&2
		exit 1
	}
}

rpc() {
	curl --fail --silent --show-error --connect-timeout 1 --max-time 5 \
		-X POST -H 'Content-Type: application/json' \
		--data "{\"jsonrpc\":\"2.0\",\"method\":\"$1\",\"params\":$2,\"id\":1}" \
		"$RPC_URL"
}

dynamic_en_filter='any(.result[];
  any(.networks[];
    .nodeType == "en" and .static == false and .trusted == false
  )
)'

stop_if_running() {
	[ -f "$PID_FILE" ] || return 0

	local pid command_line
	pid="$(cat "$PID_FILE")"
	command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
	case "$command_line" in
		*"--datadir $TEST_DIR/data"*)
			kill -TERM "$pid"
			for _ in {1..10}; do
				kill -0 "$pid" 2>/dev/null || return 0
				sleep 1
			done
			echo "KEN pid=$pid did not stop within 10 seconds." >&2
			return 1
			;;
		"") return 0 ;;
		*)
			echo "Refusing to stop pid=$pid: it is not this smoke test's KEN process." >&2
			return 1
			;;
	esac
}

clean() {
	stop_if_running
	rm -rf "$TEST_DIR"
	echo "Removed test data: $TEST_DIR"
}

wait_for_rpc() {
	for _ in {1..30}; do
		if rpc net_version '[]' >/dev/null 2>&1; then
			echo "KEN RPC is ready: $RPC_URL"
			return 0
		fi
		sleep 1
	done

	echo "KEN did not open RPC within 30 seconds. See: $LOG_FILE" >&2
	tail -50 "$LOG_FILE" >&2 || true
	return 1
}

start() {
	require_command curl
	require_command git
	require_command lsof
	[ -x "$KEN_BIN" ] || {
		echo "KEN binary not found: $KEN_BIN (run: make ken)" >&2
		exit 1
	}
	[ ! -e "$TEST_DIR" ] || {
		echo "Test data already exists. Run: $0 clean" >&2
		exit 1
	}

	if lsof -nP -iUDP:"$P2P_PORT" -iTCP:"$P2P_PORT" -iTCP:"$SUBPORT" -iTCP:"$RPC_PORT" >/dev/null; then
		echo "A smoke-test port is already in use; do not stop unrelated nodes." >&2
		exit 1
	fi

	mkdir -p "$LOG_DIR"
	"$KEN_BIN" version >"$LOG_DIR/version.txt"
	git -C "$ROOT_DIR" rev-parse HEAD >"$LOG_DIR/commit.txt"

	nohup "$KEN_BIN" \
		--mainnet --syncmode full --multichannel --maxconnections 10 --nat any \
		--datadir "$TEST_DIR/data" --port "$P2P_PORT" --subport "$SUBPORT" \
		--rpc --rpcaddr 127.0.0.1 --rpcport "$RPC_PORT" --rpcapi admin,net,kaia \
		--verbosity 4 \
		</dev/null >"$LOG_FILE" 2>&1 &
	local pid=$!
	printf '%s\n' "$pid" >"$PID_FILE"
	disown "$pid" 2>/dev/null || true

	echo "Started KEN pid=$pid"
	echo "Bootstrap: built-in Mainnet BN list (no --bootnodes)"
	wait_for_rpc
}

status() {
	require_command curl
	require_command jq

	local peers
	peers="$(rpc admin_peers '[]')" || {
		echo "KEN RPC is unavailable at $RPC_URL. Start the test with: $0 start" >&2
		return 1
	}

	echo "=== Mainnet EN compatibility smoke test: $(date -u +%FT%TZ) ==="
	echo "commit    : $(cat "$LOG_DIR/commit.txt" 2>/dev/null || echo unknown)"
	echo "bootstrap : built-in Mainnet BN list (no --bootnodes)"
	echo "block     : $(rpc kaia_blockNumber '[]' | jq -r '.result')"
	echo "peerCount : $(rpc net_peerCount '[]' | jq -r '.result')"

	printf '%s\n' "$peers" | jq -r '
	  [.result[]
	   | select(any(.networks[]; .nodeType == "en" and .static == false and .trusted == false))]
	  | "dynamic-mainnet-en=\(length)"
	'
	printf '%s\n' "$peers" | jq -r '
	  .result[] as $peer
	  | $peer.networks[]
	  | select(.nodeType == "en" and .static == false and .trusted == false)
	  | "  \(if .inbound then "inbound " else "outbound" end)  \($peer.name)  \(.remoteAddress)"
	'
}

assert_connection() {
	require_command jq
	local peers
	peers="$(rpc admin_peers '[]')" || {
		echo "FAIL: KEN RPC is unavailable at $RPC_URL." >&2
		return 1
	}

	if printf '%s\n' "$peers" | jq -e "$dynamic_en_filter" >/dev/null; then
		echo "PASS: dynamic Mainnet EN connection present (not static, not trusted)."
	else
		echo "FAIL: no dynamic Mainnet EN connection is present." >&2
		return 1
	fi
}

wait_for_en() {
	require_command jq
	for _ in {1..60}; do
		if rpc admin_peers '[]' 2>/dev/null | jq -e "$dynamic_en_filter" >/dev/null 2>&1; then
			status
			assert_connection
			return 0
		fi
		sleep 10
	done

	echo "FAIL: no dynamic Mainnet EN connection appeared within 10 minutes." >&2
	return 1
}

case "${1:-}" in
	clean) clean ;;
	start) start ;;
	wait) wait_for_en ;;
	status) status ;;
	assert) assert_connection ;;
	stop) stop_if_running ;;
	*) usage; exit 1 ;;
esac
