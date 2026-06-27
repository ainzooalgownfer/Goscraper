#!/bin/bash

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
PURPLE='\033[0;35m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

API_URL="http://localhost:8080"
MAX_WAIT=180
declare -a JOBS=()

declare -A TARGET_STRATEGIES
declare -A TARGET_SELECTORS

# IP verification
TARGET_STRATEGIES["http://ip-api.com/json"]="title"
TARGET_STRATEGIES["http://ip-api.com/json:2"]="title"
TARGET_STRATEGIES["http://ip-api.com/json:3"]="title"

# easy — title
TARGET_STRATEGIES["https://wikipedia.org"]="title"
TARGET_STRATEGIES["https://news.ycombinator.com"]="title"
TARGET_STRATEGIES["https://www.iana.org/domains/reserved"]="title"

# news strategy
TARGET_STRATEGIES["https://en.wikinews.org/wiki/Main_Page"]="news"

# ecommerce — plain HTTP
TARGET_STRATEGIES["http://books.toscrape.com"]="ecommerce"
TARGET_STRATEGIES["http://books.toscrape.com/catalogue/page-2.html"]="ecommerce"
TARGET_STRATEGIES["http://webscraper.io/test-sites/e-commerce/allinone"]="ecommerce"

# custom
TARGET_STRATEGIES["http://quotes.toscrape.com"]="custom"
TARGET_SELECTORS["http://quotes.toscrape.com"]='{"quote":".text","author":".author","tags":".tag"}'

TARGET_STRATEGIES["http://quotes.toscrape.com/page/2/"]="custom"
TARGET_SELECTORS["http://quotes.toscrape.com/page/2/"]='{"quote":".text","author":".author"}'

# strict — expected blocks
TARGET_STRATEGIES["https://www.reddit.com"]="title"
TARGET_STRATEGIES["https://www.amazon.com"]="ecommerce"
TARGET_STRATEGIES["https://www.cloudflare.com"]="title"

TARGETS=(
    "http://ip-api.com/json"
    "http://ip-api.com/json:2"
    "http://ip-api.com/json:3"
    "https://wikipedia.org"
    "https://news.ycombinator.com"
    "https://www.iana.org/domains/reserved"
    "https://en.wikinews.org/wiki/Main_Page"
    "http://books.toscrape.com"
    "http://books.toscrape.com/catalogue/page-2.html"
    "http://webscraper.io/test-sites/e-commerce/allinone"
    "http://quotes.toscrape.com"
    "http://quotes.toscrape.com/page/2/"
    "https://www.reddit.com"
    "https://www.amazon.com"
    "https://www.cloudflare.com"
)

echo -e "${YELLOW}=================================================="
echo -e "      Proxy Pool Stress & Break Test v2"
echo -e "==================================================${NC}\n"
echo -e "  Total jobs : ${CYAN}${#TARGETS[@]}${NC}"
echo -e "  API        : ${CYAN}$API_URL${NC}\n"

# --- pool check ---
echo -e "${CYAN}[0/4] Checking proxy pool status...${NC}"
POOL=$(curl -s "$API_URL/pool/status")
ACTIVE=$(echo "$POOL" | grep -o '"active":[0-9]*' | head -1 | cut -d: -f2)
TOTAL=$(echo "$POOL"  | grep -o '"total":[0-9]*'  | head -1 | cut -d: -f2)

if [ "${ACTIVE:-0}" = "0" ]; then
    echo -e "  ${RED}Pool has 0 active proxies — resetting...${NC}"
    curl -s -X POST "$API_URL/pool/reset" > /dev/null
    sleep 2
    echo -e "  ${GREEN}Pool reset done.${NC}\n"
else
    echo -e "  Pool: ${GREEN}$ACTIVE${NC}/${TOTAL} proxies active\n"
fi

# --- submit ---
echo -e "${CYAN}[1/4] Flooding queue with ${#TARGETS[@]} jobs...${NC}\n"
for target in "${TARGETS[@]}"; do
    url=$(echo "$target" | sed 's/:[0-9]*$//')
    strategy="${TARGET_STRATEGIES[$target]:-title}"
    selectors="${TARGET_SELECTORS[$target]:-}"

    if [ -n "$selectors" ]; then
        PAYLOAD="{\"url\":\"$url\",\"strategy\":\"$strategy\",\"selectors\":$selectors}"
    else
        PAYLOAD="{\"url\":\"$url\",\"strategy\":\"$strategy\"}"
    fi

    RESPONSE=$(curl -s -X POST "$API_URL/scrape" \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD")

    JOB_ID=$(echo "$RESPONSE" | grep -o '"job_id":"[^"]*' | grep -o '[^"]*$')
    STRAT=$(echo "$RESPONSE"  | grep -o '"strategy":"[^"]*"' | cut -d'"' -f4)

    if [ -n "$JOB_ID" ]; then
        echo -e "  ${GREEN}queued${NC} $JOB_ID [${BLUE}$STRAT${NC}] → ${PURPLE}$url${NC}"
        JOBS+=("$JOB_ID|$url|$strategy")
    else
        echo -e "  ${RED}failed to queue${NC} → $url — $RESPONSE"
    fi
done

# --- smart wait ---
echo -e "\n${CYAN}[2/4] Waiting for all jobs to complete (max ${MAX_WAIT}s)...${NC}"
ELAPSED=0
while [ $ELAPSED -lt $MAX_WAIT ]; do
    METRICS=$(curl -s "$API_URL/metrics")
    PENDING=$(echo "$METRICS"    | grep -o '"pending":[0-9]*'    | cut -d: -f2)
    PROCESSING=$(echo "$METRICS" | grep -o '"processing":[0-9]*' | cut -d: -f2)
    REMAINING=$((${PENDING:-0} + ${PROCESSING:-0}))

    if [ "$REMAINING" -eq 0 ]; then
        echo -e "\n  ${GREEN}All jobs finished after ${ELAPSED}s.${NC}"
        break
    fi

    echo -ne "\r  ${YELLOW}${ELAPSED}s elapsed — ${REMAINING} job(s) still running (pending:${PENDING:-0} processing:${PROCESSING:-0})${NC}    "
    sleep 3
    ELAPSED=$((ELAPSED+3))
done

if [ $ELAPSED -ge $MAX_WAIT ]; then
    echo -e "\n  ${YELLOW}Max wait reached — collecting partial results.${NC}"
fi

# --- results ---
echo -e "\n${CYAN}[3/4] Results...${NC}\n"

COMPLETED=0
FAILED=0
BLOCKED=0

printf "%-10s %-12s %-10s %-45s %s\n" "JOB ID" "STATUS" "STRATEGY" "URL" "RESULT"
echo "--------------------------------------------------------------------------------------------------------"

for entry in "${JOBS[@]}"; do
    JOB_ID=$(echo "$entry"  | cut -d'|' -f1)
    URL=$(echo "$entry"     | cut -d'|' -f2)
    STRAT=$(echo "$entry"   | cut -d'|' -f3)

    RESULT=$(curl -s "$API_URL/jobs/$JOB_ID")
    STATUS=$(echo "$RESULT" | grep -o '"status":"[^"]*"'       | cut -d'"' -f4)
    TITLE=$(echo "$RESULT"  | grep -o '"result_title":"[^"]*"' | cut -d'"' -f4)

    SHORT_URL=$(echo "$URL"   | sed 's|https\?://||' | cut -c1-45)
    SHORT_ID=$(echo "$JOB_ID" | cut -c1-8)
    SHORT_TITLE=$(echo "$TITLE" | cut -c1-40)

    if [ "$STATUS" = "completed" ]; then
        COMPLETED=$((COMPLETED+1))
        printf "${GREEN}%-10s %-12s${NC} ${BLUE}%-10s${NC} %-45s %s\n" \
            "$SHORT_ID" "completed" "$STRAT" "$SHORT_URL" "$SHORT_TITLE"

    elif [ "$STATUS" = "failed" ]; then
        if echo "$TITLE" | grep -qi "403\|forbidden\|blocked\|captcha"; then
            BLOCKED=$((BLOCKED+1))
            printf "${YELLOW}%-10s %-12s${NC} ${BLUE}%-10s${NC} %-45s %s\n" \
                "$SHORT_ID" "BLOCKED" "$STRAT" "$SHORT_URL" "-"
        else
            FAILED=$((FAILED+1))
            printf "${RED}%-10s %-12s${NC} ${BLUE}%-10s${NC} %-45s %s\n" \
                "$SHORT_ID" "failed" "$STRAT" "$SHORT_URL" "-"
        fi
    else
        printf "${YELLOW}%-10s %-12s${NC} ${BLUE}%-10s${NC} %-45s %s\n" \
            "$SHORT_ID" "$STATUS" "$STRAT" "$SHORT_URL" "-"
    fi
done

# --- metrics ---
echo -e "\n${CYAN}  API Metrics:${NC}"
METRICS=$(curl -s "$API_URL/metrics")
TOTAL_JOBS=$(echo "$METRICS" | grep -o '"total":[0-9]*'      | head -1 | cut -d: -f2)
COMP_JOBS=$(echo "$METRICS"  | grep -o '"completed":[0-9]*'  | cut -d: -f2)
FAIL_JOBS=$(echo "$METRICS"  | grep -o '"failed":[0-9]*'     | cut -d: -f2)
PEND_JOBS=$(echo "$METRICS"  | grep -o '"pending":[0-9]*'    | cut -d: -f2)
echo -e "  Total: ${CYAN}$TOTAL_JOBS${NC} | Completed: ${GREEN}$COMP_JOBS${NC} | Failed: ${RED}$FAIL_JOBS${NC} | Pending: ${YELLOW}$PEND_JOBS${NC}"

# --- summary ---
echo ""
echo -e "${YELLOW}=================================================="
echo -e "                    Summary"
echo -e "==================================================${NC}"
echo -e "  Completed : ${GREEN}$COMPLETED${NC}"
echo -e "  Blocked   : ${YELLOW}$BLOCKED${NC}"
echo -e "  Failed    : ${RED}$FAILED${NC}"
echo ""

if [ "$FAILED" -eq 0 ] && [ "$COMPLETED" -gt 0 ]; then
    echo -e "  ${GREEN}PASS — pool held under load.${NC}"
elif [ "$COMPLETED" -gt "$FAILED" ]; then
    echo -e "  ${YELLOW}PARTIAL — pool mostly held, some failures.${NC}"
else
    echo -e "  ${RED}FAIL — too many failures.${NC}"
fi

echo -e "\n  Tip: Reddit/Amazon/Cloudflare blocks are expected through Tor."
echo -e "  Use ${CYAN}curl -X POST $API_URL/pool/reset${NC} between runs.\n"
echo -e "${YELLOW}==================================================${NC}\n"