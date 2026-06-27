
# Distributed High-Velocity Tor Scraping API Cluster

A high-performance, fault-tolerant asynchronous web scraping engine built in Go. This system leverages a multi-worker concurrency pool, a native SQLite job repository, and a distributed network proxy matrix using **HAProxy** and scaled **Tor/Privoxy** instances to rotate exit nodes dynamically on every request.

---

## System Architecture

The project separates scraping logic from raw network adapters to provide a scalable scraping infrastructure.

1. **Go API Service (Gin Engine)**  
   Accepts scraping requests asynchronously, generates tracking IDs, and submits jobs to the internal worker pool.

2. **Worker Pool Engine**  
   Runs multiple concurrent Go routines that process scraping jobs from an execution queue. Each worker draws randomly from the proxy pool on every job, ensuring no two workers are pinned to the same exit node.

3. **HAProxy (Layer 4 TCP Load Balancer)**  
   Acts as a centralized proxy gateway on port `8888`. Available as a fallback entry point but workers bypass it by default, connecting directly to Tor nodes for better isolation:

   ```
   http://proxy-balancer:8888
   ```

4. **Tor Replicas (Privoxy Gateways)**  
   Five named containers each running an isolated Tor instance. Privoxy on port `8118` converts HTTP proxy requests into Tor SOCKS5 traffic. Each container builds its own circuit, producing a distinct exit IP. Workers connect directly to each node:

   ```
   http://tor-node-1:8118
   http://tor-node-2:8118
   ...
   http://tor-node-5:8118
   ```

---

## Project Directory Structure

```text
├── cmd/
│   └── api/
│       └── main.go                  # Application entry point, proxy pool init
├── internal/
│   ├── api/
│   │   └── server.go                # Gin server & HTTP handlers
│   ├── proxy/
│   │   └── pool.go                  # Proxy pool, random selection, health tracking
│   ├── scraper/
│   │   ├── scraper.go               # Colly engine & HTTP transport
│   │   ├── strategy.go              # Scraping strategies (title, news, ecommerce, custom)
│   │   ├── worker.go                # Single worker — job processing, error handling
│   │   └── worker_pool.go           # Worker pool orchestration
│   └── storage/
│       ├── repository.go            # Job model, interface, helpers
│       └── sqlite.go                # SQLite persistence layer
├── scripts/
│   ├── test_tor.sh                  # Tor exit IP isolation test
│   └── test_scraper.sh              # Full proxy pool stress & strategy test
├── docker-compose.yml               # Container infrastructure
├── haproxy.cfg                      # Load balancer configuration
├── rotate.conf                      # Tor circuit rotation settings (mounted into each node)
└── README.md
```

---

## Getting Started

### Prerequisites

- Docker
- Docker Compose
- Go

Compatible with Linux, macOS, and Windows WSL2.

---

## API Endpoints

| Method   | Endpoint           | Description                                      |
|----------|--------------------|--------------------------------------------------|
| `GET`    | `/health`          | Health check                                     |
| `POST`   | `/scrape`          | Submit a scrape job                              |
| `GET`    | `/jobs`            | List all jobs (paginated, filterable by status)  |
| `GET`    | `/jobs/export`     | Export all jobs as JSON                          |
| `DELETE` | `/jobs`            | Delete all job records                           |
| `GET`    | `/jobs/:id`        | Get a specific job                               |
| `DELETE` | `/jobs/:id`        | Delete a specific job                            |
| `POST`   | `/jobs/:id/retry`  | Requeue a failed job                             |
| `GET`    | `/pool/status`     | Proxy pool health and stats                      |
| `POST`   | `/pool/reset`      | Reactivate all proxies and clear failure counts  |
| `GET`    | `/metrics`         | Job counts and pool summary                      |
| `DELETE` | `/db/reset`        | Drop and recreate all tables                     |
| `GET`    | `/swagger/*`       | Swagger UI                                       |

Full interactive docs available at `http://localhost:8080/swagger/index.html`.

---

## Scraping Strategies

Jobs accept a `strategy` field that controls what gets extracted from the target page.

### `title` (default)
Extracts the page title and meta description.

```json
{
  "url": "https://news.ycombinator.com"
}
```

### `news`
Extracts the page title, first `h1`, and article paragraph text.

```json
{
  "url": "https://en.wikinews.org/wiki/Main_Page",
  "strategy": "news"
}
```

### `ecommerce`
Extracts product name, price, and availability using `itemprop` schema attributes. Use plain `http://` URLs for better Tor compatibility.

```json
{
  "url": "http://books.toscrape.com",
  "strategy": "ecommerce"
}
```

### `custom`
Caller defines CSS selectors at request time. No code changes needed.

```json
{
  "url": "http://quotes.toscrape.com",
  "strategy": "custom",
  "selectors": {
    "quote": ".text",
    "author": ".author",
    "tags": ".tag"
  }
}
```

---

## Configuration Files

### `rotate.conf`

Mounted into each Tor container at `/etc/torrc.d/rotate.conf`. Appended to the default torrc via `%include`. Controls circuit rotation behavior:

```text
NewCircuitPeriod 10
MaxCircuitDirtiness 10
EnforceDistinctSubnets 1
```

> Do not redefine `SocksPort` or `ControlPort` here — they are already set in the image's default torrc and will cause a bind conflict.

### `haproxy.cfg`

Distributes connections across the five named Tor backends using round-robin. Each backend is health-checked independently:

```haproxy
backend tor_backend
    mode tcp
    balance roundrobin
    default-server resolvers docker_dns init-addr none check inter 5s rise 2 fall 3
    server tor-node-1 tor-node-1:8118
    server tor-node-2 tor-node-2:8118
    server tor-node-3 tor-node-3:8118
    server tor-node-4 tor-node-4:8118
    server tor-node-5 tor-node-5:8118
```

---

## Starting the Infrastructure

```bash
docker-compose down && docker-compose up --build
```

Tor containers take ~30-60 seconds to bootstrap circuits on first start. The `test_tor.sh` script will wait automatically and abort if any node fails to reach `Bootstrapped 100%`.

---

## Verifying Proxy Isolation

Before running scrape jobs, confirm each Tor container has a distinct exit IP:

```bash
./scripts/test_tor.sh
```

Expected output:

```
  tor-node-1: 194.15.36.117
            Amsterdam, Netherlands
            Tor Exit Node

  tor-node-2: 185.220.101.17
            Frankfurt am Main, Germany
            Stiftung Erneuerbare Freiheit

  Unique exit IPs : 5
  PASS — all 5 nodes up with distinct exit IPs.
```

---

## Running the Stress Test

Runs jobs across all strategies against easy, medium, and strict targets. Waits intelligently for all jobs to finish before collecting results:

```bash
./scripts/test_scraper.sh
```

Manual scrape request:

```bash
curl -X POST http://localhost:8080/scrape \
     -H "Content-Type: application/json" \
     -d '{"url":"http://quotes.toscrape.com","strategy":"custom","selectors":{"quote":".text","author":".author"}}'
```

Retrieve job result:

```bash
curl http://localhost:8080/jobs/<YOUR_JOB_ID>
```

Check worker IP distribution in logs:

```bash
docker-compose logs api | grep "Target sees IP"
```

---

## Proxy Pool Behavior

- Workers select proxies **randomly** on every job — no static worker-to-proxy binding
- **Soft failures** (403, 429, site blocks) increment a failure counter but never deactivate the proxy — a site block is not a proxy failure
- **Hard failures** (EOF, connection refused, timeout) increment a separate hard failure counter; after 5 hard failures the proxy enters a 60 second cooldown
- Proxies **auto-recover** after the cooldown period without requiring a restart
- `POST /pool/reset` manually reactivates all proxies and clears all counters between test runs

---

# Network Debugging

## Tor Containers Not Bootstrapping

Check bootstrap log since container start:

```bash
docker logs <container-name> 2>&1 | grep -i "bootstrapped\|warn\|err"
```

If stuck below 100%, restart that node:

```bash
docker-compose restart tor-node-1
```

## All Workers Showing the Same Exit IP

Possible causes:

- `rotate.conf` not being applied — verify with:

```bash
docker exec <container-name> cat /etc/torrc.d/rotate.conf
```

- `SocksPort` defined in `rotate.conf` — remove it, it conflicts with the default torrc
- Workers hitting HAProxy instead of nodes directly — verify `main.go` proxy list points to `tor-node-X:8118`

## HTTPS Timeouts Through Tor

Some sites are slow or unreachable over HTTPS through Tor exit nodes. Use plain `http://` URLs where possible. HTTPS scraping works but may require retrying with a different exit node.

Retry a failed job via the API:

```bash
curl -X POST http://localhost:8080/jobs/<JOB_ID>/retry
```

## HAProxy Exits with Code 1

Missing newline at end of `haproxy.cfg`:

```bash
echo "" >> haproxy.cfg
docker-compose restart proxy-balancer
```

## Container Name Prefix

Container names vary by machine depending on the folder name. The test scripts auto-detect the prefix. If something breaks, check actual names with:

```bash
docker ps --format "{{.Names}}" | grep tor
```

---

# Technology Stack

## Backend

- Go
- Gin
- Colly
- SQLite (via GORM)

## Infrastructure

- Docker
- Docker Compose
- HAProxy
- Tor
- Privoxy

## Architecture Concepts

- Worker pools with randomized proxy selection
- Asynchronous job queue
- Pluggable scraping strategy pattern
- Per-node Tor circuit isolation
- Soft vs hard proxy failure classification
- Auto-recovering proxy cooldown
- Repository pattern
- Distributed services

---

# Known Issues

- **Aggressive anti-bot sites** (LinkedIn, Cloudflare-protected pages) will cause repeated timeouts that drain the hard failure counter and temporarily deactivate proxies. Use `POST /pool/reset` after targeting such sites.
- **Workers can share the same exit IP** within a short time window due to random selection landing on the same Tor container. Circuit rotation via `NewCircuitPeriod` mitigates this over time but does not eliminate it entirely.
- **HTTPS through Tor/Privoxy** is slower and less reliable than plain HTTP due to CONNECT tunnel overhead. Prefer `http://` targets where the site supports it.

---

# Planned

- Per-worker dedicated Tor node assignment to eliminate same-IP collisions
- NEWNYM signal support via Tor control port to force a fresh circuit per job
- JavaScript rendering support via headless browser integration
- Rate limiting per domain to avoid triggering anti-bot thresholds
- Webhook callbacks when jobs complete
- Dashboard UI for real-time job and pool monitoring

---

# Legal and Ethical Usage

This project is intended for public data collection, research, and educational purposes.

Users should:

- Respect website terms of service
- Follow `robots.txt` policies where applicable
- Avoid collecting personal data
- Implement rate limiting and responsible request patterns
- Respect privacy regulations such as GDPR and CCPA
