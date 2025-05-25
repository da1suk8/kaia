#!/bin/bash

# Constants
MAINNET_ENDPOINT="http://34.64.197.145:8551"
TESTNET_ENDPOINT="http://34.64.254.169:8551"
REQUEST_DELAY=0.5  # リクエスト間の遅延（秒）

# Function to display usage
display_usage() {
  echo "Usage: $0 [mainnet|testnet] [block_number]"
  echo "Example: $0 mainnet 90860440"
  exit 1
}

# Check arguments
if [ $# -lt 2 ]; then
  display_usage
fi

# Set endpoint
network_arg=$1
if [ "$network_arg" = "mainnet" ] || [ "$network_arg" = "main" ]; then
  endpoint=$MAINNET_ENDPOINT
  network_type="Mainnet"
else
  endpoint=$TESTNET_ENDPOINT
  network_type="Testnet"
fi

# Get block number
block_num=$2
if ! [[ "$block_num" =~ ^[0-9]+$ ]]; then
  echo "Error: Block number must be a number"
  display_usage
fi

echo "Connected to $network_type ($endpoint)"
echo "Checking block $block_num for proposer and voter consistency"
echo "------------------------------"

# Convert to hex for RPC
hex_block_num=$(printf "0x%x" $block_num)

# 1. Get block with consensus info (for proposer)
echo "1. Getting block proposer information..."
result=$(curl -s -X POST -H "Content-Type: application/json" \
  --data "{\"jsonrpc\":\"2.0\",\"method\":\"kaia_getBlockWithConsensusInfoByNumber\",\"params\":[\"$hex_block_num\"],\"id\":1}" \
  $endpoint)

# Extract proposer
proposer=$(echo "$result" | grep -oP '"proposer":"\K[^"]*')
if [ -z "$proposer" ]; then
  echo "ERROR: Failed to get proposer for block $block_num"
  exit 1
fi
echo "   Proposer: $proposer"

sleep $REQUEST_DELAY

# 2. Get block data (for vote data)
echo "2. Getting block vote data..."
block_data=$(curl -s -X POST -H "Content-Type: application/json" \
  --data "{\"jsonrpc\":\"2.0\",\"method\":\"kaia_getBlockByNumber\",\"params\":[\"$hex_block_num\", true],\"id\":1}" \
  $endpoint)

# Extract vote data
vote_hex=$(echo "$block_data" | grep -oP '"voteData":"\K[^"]*')

# Check if vote data exists
if [ -z "$vote_hex" ] || [ "$vote_hex" = "0x" ]; then
  echo "   No vote data found in this block"
  exit 0
fi
echo "   Vote data: ${vote_hex:0:30}... (truncated)"

# 3. Extract voter from vote data
echo "3. Extracting voter from vote data..."
voter=""
if [[ "$vote_hex" =~ ^0xf84294([0-9a-f]{40}) ]]; then
  voter="0x${BASH_REMATCH[1]}"
  echo "   Voter address extracted directly from hex"
else
  # Use ken utility to decode vote data
  echo "   Decoding vote data using ken utility..."
  sleep $REQUEST_DELAY
  
  vote_data=$(/var/kend/bin/ken util decode-vote "$vote_hex" 2>/dev/null)
  
  if [ $? -eq 0 ]; then
    # Extract validator from decoded data
    voter=$(echo "$vote_data" | tr -d '\n\r' | sed 's/ \+/ /g' | sed -n 's/.*"validator"[ ]*:[ ]*"\([^"]*\)".*/\1/p')
    echo "   Voter address extracted from decoded data"
  else
    echo "   Failed to decode vote data"
  fi
fi

if [ -z "$voter" ]; then
  echo "ERROR: Failed to get voter for block $block_num"
  exit 1
fi
echo "   Voter: $voter"

# 4. Compare proposer and voter
echo "4. Comparing proposer and voter..."
if [ "${proposer,,}" = "${voter,,}" ]; then
  echo "✅ MATCH: Proposer and voter are the same"
else
  echo "❌ MISMATCH: Proposer and voter are different"
fi

echo "------------------------------"
echo "Summary:"
echo "- Block: $block_num"
echo "- Proposer: $proposer"
echo "- Voter: $voter"
echo "- Consistency: $([ "${proposer,,}" = "${voter,,}" ] && echo "Consistent ✅" || echo "Inconsistent ❌")" 