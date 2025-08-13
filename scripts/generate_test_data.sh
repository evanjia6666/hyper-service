#!/bin/bash

# Test script that generates sample data files
# Usage: ./scripts/generate_test_data.sh [data_dir]

DATA_DIR="${1:-/tmp/test-data}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
WHITELIST_FILE="$PROJECT_DIR/configs/whitelist.txt"

# Check if whitelist file exists
if [ ! -f "$WHITELIST_FILE" ]; then
    echo "Whitelist file not found: $WHITELIST_FILE"
    exit 1
fi

# Function to generate filename based on current time
generate_filename() {
    local date=$(date -u +%Y%m%d)
    local hour=$(date -u +%H | sed 's/^0*//')  # Remove leading zeros
    echo "${DATA_DIR}/node_fills_by_block/hourly/${date}/${hour}"
}

# Create the initial directory structure
mkdir -p "${DATA_DIR}/node_fills_by_block/hourly"

echo "Starting test data generation. Press Ctrl+C to stop."

# Generate sample data lines continuously
counter=0
while true; do
    # Generate filename based on current time
    FILENAME=$(generate_filename)
    
    # Create directory if it doesn't exist
    mkdir -p "$(dirname "$FILENAME")"
    
    # Generate random events
    EVENTS="["
    for j in {1..3}; do
        # Randomly select a user from our whitelist
        USER=$(shuf -n 1 "$WHITELIST_FILE")
        
        # Generate random data
        COIN=$(shuf -n 1 -e "SUI" "XRP" "BTC" "ETH")
        PX=$(echo "scale=4; $(shuf -n 1 -i 100-10000)/1000" | bc)
        SZ=$(shuf -n 1 -i 1-1000)
        SIDE=$(shuf -n 1 -e "B" "A")
        TIME=$(date +%s)$(shuf -n 1 -i 0-999)
        START_POS=$((RANDOM % 20000 - 10000))  # -10000 to 10000
        DIR=$(shuf -n 1 -e "Open Long" "Close Long" "Open Short" "Close Short")
        CLOSED_PNL=$(echo "scale=4; $(shuf -n 1 -i 0-1000)/1000" | bc)
        HASH="0x$(openssl rand -hex 32)"
        OID=$((RANDOM * 1000 + RANDOM))
        CROSSED=$(shuf -n 1 -e "true" "false")
        FEE=$(echo "scale=6; $(shuf -n 1 -i 0-1000)/1000000" | bc)
        TID=$((RANDOM * 10000000000000 + RANDOM))
        CLOID="0x$(openssl rand -hex 16)"
        FEE_TOKEN="USDC"
        
        EVENTS+="[\"${USER}\",{\"coin\":\"${COIN}\",\"px\":\"${PX}\",\"sz\":\"${SZ}\",\"side\":\"${SIDE}\",\"time\":${TIME},\"startPosition\":\"${START_POS}\",\"dir\":\"${DIR}\",\"closedPnl\":\"${CLOSED_PNL}\",\"hash\":\"${HASH}\",\"oid\":${OID},\"crossed\":${CROSSED},\"fee\":\"${FEE}\",\"tid\":${TID},\"cloid\":\"${CLOID}\",\"feeToken\":\"${FEE_TOKEN}\",\"twapId\":null}]"
        
        if [ $j -lt 3 ]; then
            EVENTS+="," 
        fi
    done
    EVENTS+="]"
    
    # Write a line to the file
    echo "{\"local_time\":\"$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)\",\"block_time\":\"$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)\",\"block_number\":$((RANDOM * 1000 + RANDOM)),\"events\":${EVENTS}}" >> "${FILENAME}"
    
    echo "Added line to ${FILENAME}"
    
    # Increment counter and exit after 50 lines for testing purposes
    counter=$((counter + 1))
    if [ $counter -ge 50 ]; then
        echo "Generated 50 lines of test data. Exiting."
        exit 0
    fi
    
    # Wait for a second before generating the next line
    sleep 1
done