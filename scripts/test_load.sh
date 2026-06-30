#!/bin/bash

API_URL="http://localhost:8080/scrape"

# The target list to blast
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

echo " Blasting API with instant requests..."

# Loop 100 times total (repeating the targets to create a heavy load)
for i in {1..150}; do
    # Pick a target from our list using modulo arithmetic
    target=${TARGETS[$((i % ${#TARGETS[@]}))]}
    
    # Construct payload
    PAYLOAD="{\"url\":\"$target\",\"strategy\":\"title\"}"
    
    # The '&' at the end forces bash to fire the request and 
    # immediately move to the next loop iteration without waiting!
    curl -s -X POST "$API_URL" \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD" \
        -w "Req $i fired -> Status: %{http_code}\n" &
done

# Wait for all background requests to clear out before closing the script
wait
echo "🎯 All requests successfully deployed."