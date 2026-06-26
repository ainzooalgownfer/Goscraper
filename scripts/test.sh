#!/bin/bash

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
PURPLE='\033[0;35m'
RED='\033[0;31m'
NC='\033[0m'

API_URL="http://localhost:8080"
declare -a JOBS=()

# mix of easy, medium, and strict sites
TARGETS=(
    # IP verification — confirms which exit node was used
    "http://ip-api.com/json"
    "http://ip-api.com/json"
    "http://ip-api.com/json"

    # easy targets — no bot protection
    "https://books.toscrape.com"
    "https://quotes.toscrape.com"
    "https://wikipedia.org"
    "https://news.ycombinator.com"

    # medium — basic rate limiting
    "https://www.iana.org/domains/reserved"
    "https://webscraper.io/test-sites/e-commerce/allinone"

    # strict — will likely block or throttle, tests proxy rotation under pressure
    "https://www.reddit.com"
    "https://www.amazon.com"
    "https://www.cloudflare.com"

    # repeat hits on same domain — tests if pool rotates IPs between requests
    "https://books.toscrape.com/catalogue/page-2.html"
    "https://books.toscrape.com/catalogue/page-3.html"
    "https://quotes.toscrape.com/page/2/"
)

echo -e "${YELLOW}=================================================="
echo -e "         Proxy Pool Stress & Break Test"
echo -e "==================================================${NC}\n"
echo -e "  Total jobs : ${CYAN}${#TARGETS[@]}${NC}"
echo -e "  API        : ${CYAN}$API_URL${NC}\n"

# --- submit all at once ---
echo -e "${CYAN}[1/3] Flooding queue with ${#TARGETS[@]} jobs...${NC}\n"
for url in "${TARGETS[@]}"; do
    RESPONSE=$(curl -s -X POST "$API_URL/scrape" \
        -H "Content-Type: application/json" \
        -d "{\"url\": \"$url\"}")

    JOB_ID=$(echo "$RESPONSE" | grep -o '"job_id":"[^"]*' | grep -o '[^"]*$')

    if [ -n "$JOB_ID" ]; then
        echo -e "  ${GREEN}queued${NC} $JOB_ID → ${PURPLE}$url${NC}"
        JOBS+=("$JOB_ID:$url")
    else
        echo -e "  ${RED}failed to queue${NC} → $url"
        echo -e "  response: $RESPONSE"
    fi
done

# --- wait for workers ---
echo -e "\n${CYAN}[2/3] Waiting 40s for workers to process all jobs...${NC}"
echo -ne "  "
for i in $(seq 1 40); do
    echo -ne "${YELLOW}.${NC}"
    sleep 1
done
echo ""

# --- collect results ---
echo -e "\n${CYAN}[3/3] Collecting results...${NC}\n"

COMPLETED=0
FAILED=0
BLOCKED=0
declare -A IP_COUNT

printf "%-10s %-12s %-20s %s\n" "JOB ID" "STATUS" "EXIT IP" "URL"
echo "--------------------------------------------------------------------------------"

for entry in "${JOBS[@]}"; do
    JOB_ID="${entry%%:*}"
    URL="${entry#*:}"

    RESULT=$(curl -s "$API_URL/jobs/$JOB_ID")
    STATUS=$(echo "$RESULT" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
    TITLE=$(echo "$RESULT" | grep -o '"title":"[^"]*"' | cut -d'"' -f4)

    IP=$(echo "$TITLE" | grep -o '[0-9]\+\.[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1)
    SHORT_URL=$(echo "$URL" | sed 's|https\?://||' | cut -c1-40)
    SHORT_ID=$(echo "$JOB_ID" | cut -c1-8)

    if [ "$STATUS" = "completed" ]; then
        COMPLETED=$((COMPLETED+1))
        [ -n "$IP" ] && IP_COUNT[$IP]=$((${IP_COUNT[$IP]:-0}+1))
        DISPLAY_IP="${IP:-n/a}"
        printf "${GREEN}%-10s %-12s${NC} %-20s %s\n" "$SHORT_ID" "completed" "$DISPLAY_IP" "$SHORT_URL"
    elif [ "$STATUS" = "failed" ]; then
        # check if it was a block or a real failure
        if echo "$TITLE" | grep -qi "403\|forbidden\|blocked\|captcha"; then
            BLOCKED=$((BLOCKED+1))
            printf "${YELLOW}%-10s %-12s${NC} %-20s %s\n" "$SHORT_ID" "BLOCKED" "-" "$SHORT_URL"
        else
            FAILED=$((FAILED+1))
            printf "${RED}%-10s %-12s${NC} %-20s %s\n" "$SHORT_ID" "failed" "-" "$SHORT_URL"
        fi
    else
        printf "${YELLOW}%-10s %-12s${NC} %-20s %s\n" "$SHORT_ID" "$STATUS" "-" "$SHORT_URL"
    fi
done

# --- summary ---
echo ""
echo -e "${YELLOW}=================================================="
echo -e "                    Summary"
echo -e "==================================================${NC}"
echo -e "  Total jobs submitted : ${CYAN}${#JOBS[@]}${NC}"
echo -e "  Completed            : ${GREEN}$COMPLETED${NC}"
echo -e "  Blocked (403/captcha): ${YELLOW}$BLOCKED${NC}"
echo -e "  Failed               : ${RED}$FAILED${NC}"

echo -e "\n  Exit IP distribution:"
for ip in "${!IP_COUNT[@]}"; do
    echo -e "    ${CYAN}$ip${NC} → ${IP_COUNT[$ip]} job(s)"
done

UNIQUE=${#IP_COUNT[@]}
echo -e "\n  Unique exit IPs : ${CYAN}$UNIQUE${NC}"

echo ""
if [ "$FAILED" -eq 0 ] && [ "$COMPLETED" -gt 0 ]; then
    echo -e "  ${GREEN}PASS — pool held under load, no hard failures.${NC}"
elif [ "$FAILED" -gt 0 ] && [ "$COMPLETED" -gt "$FAILED" ]; then
    echo -e "  ${YELLOW}PARTIAL — pool mostly held, some failures (check logs).${NC}"
else
    echo -e "  ${RED}FAIL — too many failures, proxy pool likely broken.${NC}"
fi

echo -e "\n  Tip: blocked sites (403/captcha) are expected for Reddit/Amazon."
echo -e "  What matters is the pool didn't crash and IPs rotated.\n"
echo -e "${YELLOW}==================================================${NC}\n"