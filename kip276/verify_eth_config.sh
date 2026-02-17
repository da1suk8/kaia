#!/bin/bash

# Configuration
RPC_URL="${1:-http://localhost:8553}"
METHOD="eth_config"
PARAMS='[]'

echo "Verifying $METHOD on $RPC_URL..."

# Execute RPC call
RESPONSE=$(curl -s -X POST \
  -H "Content-Type: application/json" \
  --data "{\"jsonrpc\":\"2.0\",\"method\":\"$METHOD\",\"params\":$PARAMS,\"id\":1}" \
  "$RPC_URL")

# Check if curl failed or returned empty
if [[ $? -ne 0 || -z "$RESPONSE" ]]; then
  echo "❌ Failed to connect to $RPC_URL or received empty response."
  exit 1
fi

# Link to Verify Content using Python3
echo "$RESPONSE" | python3 -c '
import sys, json

try:
    data = json.load(sys.stdin)
    if not data or "result" not in data:
        raise ValueError("Invalid JSON-RPC response")
    
    current = data["result"].get("current")
    if not current:
        raise ValueError("Missing result.current field")

    # Required fields to verify
    required_fields = ["chainId", "forkId", "precompiles", "systemContracts", "blobSchedule"]
    
    missing = []
    for field in required_fields:
        if field not in current:
            missing.append(field)
    
    if missing:
        raise ValueError("Missing fields in current config: " + str(missing))

    # Additional basic checks
    if not current["precompiles"]:
        print("⚠️ Warning: precompiles is empty")
    
    print("✅ Verification PASSED")
    print("   ChainID: " + str(current.get("chainId")))
    print("   ForkID:  " + str(current.get("forkId")))
    print("   Precompiles count: " + str(len(current.get("precompiles", {}))))
    print("   SystemContracts count: " + str(len(current.get("systemContracts", {}))))

except Exception as e:
    print("❌ Verification FAILED: " + str(e))
    sys.exit(1)
'
