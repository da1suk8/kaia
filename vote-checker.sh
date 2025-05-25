#!/bin/bash

# Constants
MAINNET_ENDPOINT="http://34.64.197.145:8551"
TESTNET_ENDPOINT="http://34.64.254.169:8551"
REQUEST_DELAY=0.5
MAX_RETRIES=3
RETRY_DELAY=5

# Extract block numbers from a log file
extract_block_numbers() {
  local file_path=$1
  local pattern=$2
  local numbers_str=$(grep -oP "$pattern" "$file_path" | grep -oP "\[\K[^\]]*")
  echo $numbers_str
}

# Extract blocks based on vote type
get_vote_blocks() {
  local file_path=$1
  local vote_type=$2
  local pattern=""

  # Set pattern based on vote type
  if [ "$vote_type" = "gov" ]; then
    pattern="headerGov: initSchema\s+voteBlockNums=\"(\[[\d\s]+\])\""
  else
    pattern="valset: initSchema\s+voteBlockNums=\"(\[[\d\s]+\])\""
  fi

  # Extract and return block numbers
  local block_numbers=$(extract_block_numbers "$file_path" "$pattern")
  if [ -z "$block_numbers" ]; then
    echo ""
    return 1
  fi

  echo "$block_numbers"
  return 0
}

# Make API calls with retry logic
make_api_call() {
  local endpoint=$1
  local data=$2
  local retry_count=0
  local response=""

  while [ $retry_count -lt $MAX_RETRIES ]; do
    response=$(curl -s -X POST -H "Content-Type: application/json" --data "$data" $endpoint)

    if [ -n "$response" ] && ! echo "$response" | grep -q "error"; then
      echo "$response"
      return 0
    fi

    retry_count=$((retry_count + 1))
    if [ $retry_count -lt $MAX_RETRIES ]; then
      sleep $RETRY_DELAY
    fi
  done

  echo "$response"
  return 1
}

# Get proposer for a block
get_proposer() {
  local endpoint=$1
  local block_num=$2
  local hex_block_num=$(printf "0x%x" $block_num)

  local request_data="{\"jsonrpc\":\"2.0\",\"method\":\"kaia_getBlockWithConsensusInfoByNumber\",\"params\":[\"$hex_block_num\"],\"id\":1}"
  local result=$(make_api_call "$endpoint" "$request_data")

  # Extract proposer
  local proposer=$(echo "$result" | grep -oP '"proposer":"\K[^"]*')
  echo "$proposer"
}

# Get vote data for a block
get_vote_data() {
  local endpoint=$1
  local block_num=$2
  local hex_block_num=$(printf "0x%x" $block_num)

  local request_data="{\"jsonrpc\":\"2.0\",\"method\":\"kaia_getBlockByNumber\",\"params\":[\"$hex_block_num\", true],\"id\":1}"
  local block_data=$(make_api_call "$endpoint" "$request_data")

  # Extract vote data
  local vote_hex=$(echo "$block_data" | grep -oP '"voteData":"\K[^"]*')
  echo "$vote_hex"
}

# Decode vote data with retry
decode_vote() {
  local vote_hex=$1
  local voter=""

  # Try direct extraction first
  if [[ "$vote_hex" =~ ^0xf84294([0-9a-f]{40}) ]]; then
    echo "0x${BASH_REMATCH[1]}"
    return 0
  fi

  # Use ken utility with retries
  for (( i=0; i<MAX_RETRIES; i++ )); do
    local vote_data=$(/var/kend/bin/ken util decode-vote "$vote_hex" 2>/dev/null)
    if [ $? -eq 0 ]; then
      voter=$(echo "$vote_data" | tr -d '\n\r' | sed 's/ \+/ /g' | sed -n 's/.*"validator"[ ]*:[ ]*"\([^"]*\)".*/\1/p')
      if [ -n "$voter" ]; then
        echo "$voter"
        return 0
      fi
    fi

    # Not last attempt, retry
    if [ $i -lt $((MAX_RETRIES-1)) ]; then
      sleep $RETRY_DELAY
    fi
  done

  return 1
}

# Check arguments
if [ $# -lt 2 ]; then
  echo "Usage: $0 [mainnet|testnet] [gov|valset]"
  echo "Example: $0 mainnet gov"
  exit 1
fi

# Set endpoint and result file
network_arg=$1
if [ "$network_arg" = "mainnet" ] || [ "$network_arg" = "main" ]; then
  endpoint=$MAINNET_ENDPOINT
  network_type="Mainnet"
  result_file="mainnet.result"
else
  endpoint=$TESTNET_ENDPOINT
  network_type="Testnet"
  result_file="kairos.result"
fi

vote_type=$2
if [ "$vote_type" != "gov" ] && [ "$vote_type" != "valset" ]; then
  echo "Vote type must be 'gov' or 'valset'"
  exit 1
fi

# Get vote blocks
block_numbers=$(get_vote_blocks "$result_file" "$vote_type")
if [ -z "$block_numbers" ]; then
  echo "No block numbers found in the result file"
  exit 1
fi

# Convert string to array and count blocks
block_numbers_array=($block_numbers)
total_blocks=$(echo "$block_numbers" | wc -w)

echo "Connected to $network_type ($endpoint)"
echo "Checking $total_blocks blocks for $vote_type votes"
echo ""

# Initialize counters
match_count=0
mismatch_count=0
error_count=0
skipped_count=0
current_count=0

start_time=$(date +%s)

# Verify each block
for (( i=0; i<${#block_numbers_array[@]}; i++ )); do
  block_num=${block_numbers_array[$i]}

  current_count=$((i + 1))
  echo -ne "Checking block $block_num ($current_count/$total_blocks)  \r"

  # Get proposer
  proposer=$(get_proposer "$endpoint" "$block_num")
  if [ -z "$proposer" ]; then
    error_count=$((error_count + 1))
    sleep $RETRY_DELAY 
    continue
  fi

  sleep $REQUEST_DELAY

  # Get vote data
  vote_hex=$(get_vote_data "$endpoint" "$block_num")

  # Skip if no vote data
  if [ -z "$vote_hex" ] || [ "$vote_hex" = "0x" ]; then
    skipped_count=$((skipped_count + 1))
    continue
  fi

  sleep $REQUEST_DELAY

  # Get voter address with retries
  voter=$(decode_vote "$vote_hex")

  # Skip if no voter found
  if [ -z "$voter" ]; then
    error_count=$((error_count + 1))
    sleep $RETRY_DELAY
    continue
  fi

  # Compare proposer and voter
  if [ "${proposer,,}" = "${voter,,}" ]; then
    match_count=$((match_count + 1))
  else
    mismatch_count=$((mismatch_count + 1))
    echo "MISMATCH at Block $block_num:                                 "
    echo "   Proposer: $proposer"
    echo "   Voter:    $voter"
    echo "------------------------------"
  fi
done

echo -ne "                                                               \r"

# Output summary
end_time=$(date +%s)
duration=$((end_time - start_time))
echo -e "\nSummary:"
echo "- Total blocks checked: $total_blocks"
echo "- Matches: $match_count"
echo "- Mismatches: $mismatch_count"
echo "- Errors: $error_count"
echo "- Skipped (no vote data): $skipped_count"
echo "- Processing time: $duration seconds"

if [ $mismatch_count -eq 0 ] && [ $error_count -eq 0 ]; then
  echo -e "\nAll blocks have consistent proposer and voter"
elif [ $mismatch_count -eq 0 ]; then
  echo -e "\nNo mismatches found, but some blocks had errors"
else
  echo -e "\nMismatches detected! See details above."
fi