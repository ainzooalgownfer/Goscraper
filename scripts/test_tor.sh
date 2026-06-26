#!/bin/bash

CYAN='\033[0;36m'
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

NUM_NODES=${1:-5}

echo -e "${CYAN}================================${NC}"
echo -e "${CYAN}   Tor Exit IP Isolation Test   ${NC}"
echo -e "${CYAN}================================${NC}\n"

SEEN_IPS=()
UNIQUE_IPS=()

for i in $(seq 1 $NUM_NODES); do
  CONTAINER="scraper-tor-node-$i-1"

  IP=$(docker exec \
    -e http_proxy=http://127.0.0.1:8118 \
    "$CONTAINER" \
    wget -q -T 15 -O - http://ip-api.com/json 2>/dev/null \
    | grep -o '"query":"[^"]*"' | cut -d'"' -f4)

  if [ -z "$IP" ]; then
    echo -e "  tor-node-$i ($CONTAINER): ${RED}unreachable${NC}"
  else
    SEEN_IPS+=("$IP")
    if ! printf '%s\n' "${UNIQUE_IPS[@]}" | grep -q "^$IP$"; then
      UNIQUE_IPS+=("$IP")
      echo -e "  tor-node-$i: ${GREEN}$IP${NC}"
    else
      echo -e "  tor-node-$i: ${YELLOW}$IP (duplicate)${NC}"
    fi
  fi
done

echo ""
echo -e "  Containers checked : ${#SEEN_IPS[@]}"
echo -e "  Unique exit IPs    : ${CYAN}${#UNIQUE_IPS[@]}${NC}"
echo ""

if [ ${#SEEN_IPS[@]} -eq 0 ]; then
  echo -e "${RED}  FAIL — no containers reachable.${NC}"
elif [ ${#UNIQUE_IPS[@]} -eq ${#SEEN_IPS[@]} ]; then
  echo -e "${GREEN}  PASS — all containers have distinct exit IPs.${NC}"
elif [ ${#UNIQUE_IPS[@]} -gt 1 ]; then
  echo -e "${YELLOW}  PARTIAL — some containers share an exit IP.${NC}"
else
  echo -e "${RED}  FAIL — all containers share the same IP.${NC}"
fi