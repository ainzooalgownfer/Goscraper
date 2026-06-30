#!/bin/bash

GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
PURPLE='\033[0;35m'
RED='\033[0;31m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

API_URL="http://localhost:8080"
MAX_WAIT=250
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

echo -e "${YELLOW}${BOLD}=================================================="
echo -e "      Proxy Pool Stress & Break Test v3"
echo -e "==================================================${NC}\n"
echo -e "  Total jobs : ${CYAN}${#TARGETS[@]}${NC}"
echo -e "  API        : ${CYAN}$API_URL${NC}\n"

# --- [0] health check ---
echo -e "${CYAN}${BOLD}[0/5] System health...${NC}"
HEALTH=$(curl -s "$API_URL/health/deep")
H_STATUS=$(echo "$HEALTH" | grep -o '"status":"[^"]*"' | cut -d'"' -f4)
H_ACTIVE=$(echo "$HEALTH" | grep -o '"active_proxies":[0-9]*' | cut -d: -f2)
H_TOTAL=$(echo "$HEALTH"  | grep -o '"total_proxies":[0-9]*'  | cut -d: -f2)

if [ "$H_STATUS" = "healthy" ]; then
    echo -e "  ${GREEN}● healthy${NC} — proxies: ${GREEN}$H_ACTIVE${NC}/${H_TOTAL}"
else
    echo -e "  ${YELLOW}● degraded${NC} — proxies: ${YELLOW}$H_ACTIVE${NC}/${H_TOTAL}"
fi

VERSION=$(curl -s "$API_URL/version")
GO_VER=$(echo "$VERSION" | grep -o '"go_version":"[^"]*"' | cut -d'"' -f4)
BUILD=$(echo "$VERSION"  | grep -o '"build_time":"[^"]*"' | cut -d'"' -f4)
echo -e "  Go: ${CYAN}$GO_VER${NC} | Built: ${CYAN}$BUILD${NC}\n"

# --- [1] pool check + rotate ---
echo -e "${CYAN}${BOLD}[1/5] Proxy pool...${NC}"
POOL=$(curl -s "$API_URL/pool/status")
ACTIVE=$(echo "$POOL" | grep -o '"active":[0-9]*' | head -1 | cut -d: -f2)
TOTAL=$(echo "$POOL"  | grep -o '"total":[0-9]*'  | head -1 | cut -d: -f2)

if [ "${ACTIVE:-0}" = "0" ]; then
    echo -e "  ${RED}Pool has 0 active proxies — resetting...${NC}"
    curl -s -X POST "$API_URL/pool/reset" > /dev/null
    sleep 2
    echo -e "  ${GREEN}Pool reset done.${NC}"
else
    echo -e "  Pool: ${GREEN}$ACTIVE${NC}/${TOTAL} proxies active"
fi

echo -e "  Rotating Tor circuits for fresh exit IPs..."
ROTATE=$(curl -s -X POST "$API_URL/pool/rotate")
echo "$ROTATE" | grep -o '"http[^"]*"' | while read r; do
    echo -e "    ${CYAN}$r${NC}"
done
echo ""

# --- [2] check strategies ---
echo -e "${CYAN}${BOLD}[2/5] Available strategies...${NC}"
STRATS=$(curl -s "$API_URL/strategies")
echo "$STRATS" | grep -o '"name":"[^"]*"' | cut -d'"' -f4 | while read s; do
    echo -e "  ${BLUE}▸${NC} $s"
done
echo ""

# --- [3] submit ---
echo -e "${CYAN}${BOLD}[3/5] Submitting ${#TARGETS[@]} jobs...${NC}\n"
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
echo -e "\n${CYAN}${BOLD}[4/5] Waiting for jobs to complete (max ${MAX_WAIT}s)...${NC}"
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

    echo -ne "\r  ${YELLOW}${ELAPSED}s — ${REMAINING} running (pending:${PENDING:-0} processing:${PROCESSING:-0})${NC}    "
    sleep 3
    ELAPSED=$((ELAPSED+3))
done

if [ $ELAPSED -ge $MAX_WAIT ]; then
    echo -e "\n  ${YELLOW}Max wait reached — collecting partial results.${NC}"
fi

# --- results ---
echo -e "\n${CYAN}${BOLD}[5/5] Results...${NC}\n"

COMPLETED=0
FAILED=0
BLOCKED=0

printf "${BOLD}%-10s %-12s %-10s %-40s %s${NC}\n" "JOB ID" "STATUS" "STRATEGY" "URL" "RESULT"
echo "-----------------------------------------------------------------------------------------------------------"

for entry in "${JOBS[@]}"; do
    JOB_ID=$(echo "$entry" | cut -d'|' -f1)
    URL=$(echo "$entry"    | cut -d'|' -f2)
    STRAT=$(echo "$entry"  | cut -d'|' -f3)

    RESULT=$(curl -s "$API_URL/jobs/$JOB_ID")
    STATUS=$(echo "$RESULT" | grep -o '"status":"[^"]*"'       | cut -d'"' -f4)
    TITLE=$(echo "$RESULT"  | grep -o '"result_title":"[^"]*"' | cut -d'"' -f4)

    SHORT_URL=$(echo "$URL"   | sed 's|https\?://||' | cut -c1-40)
    SHORT_ID=$(echo "$JOB_ID" | cut -c1-8)
    SHORT_TITLE=$(echo "$TITLE" | cut -c1-45)

    if [ "$STATUS" = "completed" ]; then
        COMPLETED=$((COMPLETED+1))
        printf "${GREEN}%-10s %-12s${NC} ${BLUE}%-10s${NC} %-40s %s\n" \
            "$SHORT_ID" "completed" "$STRAT" "$SHORT_URL" "$SHORT_TITLE"
    elif [ "$STATUS" = "failed" ]; then
        if echo "$TITLE" | grep -qi "403\|forbidden\|blocked\|captcha"; then
            BLOCKED=$((BLOCKED+1))
            printf "${YELLOW}%-10s %-12s${NC} ${BLUE}%-10s${NC} %-40s %s\n" \
                "$SHORT_ID" "BLOCKED" "$STRAT" "$SHORT_URL" "-"
        else
            FAILED=$((FAILED+1))
            printf "${RED}%-10s %-12s${NC} ${BLUE}%-10s${NC} %-40s %s\n" \
                "$SHORT_ID" "failed" "$STRAT" "$SHORT_URL" "-"
        fi
    else
        printf "${YELLOW}%-10s %-12s${NC} ${BLUE}%-10s${NC} %-40s %s\n" \
            "$SHORT_ID" "$STATUS" "$STRAT" "$SHORT_URL" "-"
    fi
done

# --- job stats ---
echo -e "\n${CYAN}${BOLD}  Strategy breakdown:${NC}"
STATS=$(curl -s "$API_URL/jobs/stats")
echo "$STATS" | grep -o '"strategy":"[^"]*"\|"total":[0-9]*\|"completed":[0-9]*\|"success_rate":"[^"]*"' \
    | paste - - - - \
    | while IFS=$'\t' read strat total comp rate; do
        S=$(echo "$strat" | cut -d'"' -f4)
        T=$(echo "$total" | cut -d: -f2)
        C=$(echo "$comp"  | cut -d: -f2)
        R=$(echo "$rate"  | cut -d'"' -f4)
        echo -e "    ${BLUE}$S${NC} — total:${CYAN}$T${NC} completed:${GREEN}$C${NC} rate:${YELLOW}$R${NC}"
    done

# --- pool status after ---
echo -e "\n${CYAN}${BOLD}  Pool after test:${NC}"
POOL=$(curl -s "$API_URL/pool/status")
echo "$POOL" | grep -o '"url":"[^"]*"\|"active":[^,}]*\|"success":[^,}]*\|"failures":[^,}]*\|"hard_failures":[^,}]*' \
    | paste - - - - - \
    | while IFS=$'\t' read url active success failures hard; do
        URL_VAL=$(echo "$url"      | cut -d'"' -f4)
        ACT_VAL=$(echo "$active"   | cut -d: -f2)
        SUC_VAL=$(echo "$success"  | cut -d: -f2)
        FAI_VAL=$(echo "$failures" | cut -d: -f2)
        HRD_VAL=$(echo "$hard"     | cut -d: -f2)
        if [ "$ACT_VAL" = "true" ]; then
            echo -e "  ${GREEN}●${NC} $URL_VAL — success:${GREEN}$SUC_VAL${NC} soft:${YELLOW}$FAI_VAL${NC} hard:${RED}$HRD_VAL${NC}"
        else
            echo -e "  ${RED}●${NC} $URL_VAL — ${RED}DEACTIVATED${NC} hard:${RED}$HRD_VAL${NC}"
        fi
    done

# --- metrics ---
echo -e "\n${CYAN}${BOLD}  Cumulative metrics:${NC}"
METRICS=$(curl -s "$API_URL/metrics")
TOTAL_JOBS=$(echo "$METRICS" | grep -o '"total":[0-9]*'      | head -1 | cut -d: -f2)
COMP_JOBS=$(echo "$METRICS"  | grep -o '"completed":[0-9]*'  | cut -d: -f2)
FAIL_JOBS=$(echo "$METRICS"  | grep -o '"failed":[0-9]*'     | cut -d: -f2)
PEND_JOBS=$(echo "$METRICS"  | grep -o '"pending":[0-9]*'    | cut -d: -f2)
echo -e "  Total: ${CYAN}$TOTAL_JOBS${NC} | Completed: ${GREEN}$COMP_JOBS${NC} | Failed: ${RED}$FAIL_JOBS${NC} | Pending: ${YELLOW}$PEND_JOBS${NC}"

# --- summary ---
echo ""
echo -e "${YELLOW}${BOLD}=================================================="
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

echo -e "\n  Tips:"
echo -e "  — Reddit/Amazon/Cloudflare blocks are expected through Tor"
echo -e "  — Run ${CYAN}curl -X POST $API_URL/pool/rotate${NC} before large scrape runs"
echo -e "  — Run ${CYAN}curl -X POST $API_URL/pool/reset${NC} if pool degrades"
echo -e "  — Use ${CYAN}POST $API_URL/scrape/test${NC} to test selectors synchronously"
echo -e "  — Use ${CYAN}POST $API_URL/scrape/bulk${NC} to submit many URLs at once\n"
echo -e "${YELLOW}${BOLD}==================================================${NC}\n"