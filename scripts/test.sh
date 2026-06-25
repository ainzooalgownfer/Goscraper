#!/bin/bash

# Format colors for terminal output
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
PURPLE='\033[0;35m'
NC='\033[0m' 

API_URL="http://localhost:8080"
declare -a JOBS=()

echo -e "${YELLOW}=================================================="
echo -e " Launching Load-Balanced Sequential Worker Test"
echo -e "==================================================${NC}\n"

TARGETS=(
    "https://api.ipify.org"
    "https://news.ycombinator.com"
    "https://www.reddit.com/robots.txt"
    "https://www.python.org"
    "https://www.apache.org"
)

echo -e "${CYAN}[1/2] Submitting target endpoints to worker pool...${NC}"
for url in "${TARGETS[@]}"; do
    echo -e " Dispatching payload for: ${PURPLE}$url${NC}"
    
    RESPONSE=$(curl -s -X POST "$API_URL/scrape" \
        -H "Content-Type: application/json" \
        -d "{\"url\": \"$url\"}")
    
    JOB_ID=$(echo "$RESPONSE" | grep -o '"job_id":"[^"]*' | grep -o '[^"]*$')
    
    if [ -n "$JOB_ID" ]; then
        echo -e "   Tracking Token Assigned: ${GREEN}$JOB_ID${NC}"
        JOBS+=("$JOB_ID")
    else
        echo -e "   Submission Failed: $RESPONSE"
    fi

   
    sleep 2
done

echo -e "\n${YELLOW}--------------------------------------------------"
echo -e " Waiting 5 more seconds for the final worker to finish..."
echo -e "--------------------------------------------------${NC}\n"
sleep 5


echo -e "${CYAN}[2/2] Retrieving state vectors from SQLite persistent layer...${NC}"
for id in "${JOBS[@]}"; do
    echo -e "\nQuerying Job Record: ${GREEN}$id${NC}"
    curl -s "$API_URL/jobs/$id" | sed 's/,/,\n  /g' | sed 's/{/{\n  /g' | sed 's/}/\n}/g'
    echo ""
done

echo -e "\n${YELLOW}=================================================="
echo -e " Diagnostics Run Complete."
echo -e "==================================================${NC}"