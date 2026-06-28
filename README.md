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
│   ├── gen_torrc.sh                 # Generates torrc from template using .env
│   ├── test_tor.sh                  # Tor exit IP isolation test
│   └── test_scraper.sh              # Full proxy pool stress & strategy test
├── docker-compose.yml               # Container infrastructure
├── haproxy.cfg                      # Load balancer configuration
├── torrc.template                   # Tor config template — rendered into torrc via gen_torrc.sh
├── torrc                            # Generated Tor config — gitignored, do not commit
├── .env                             # Secrets — gitignored, do not commit
├── .env.example                     # Env template — safe to commit
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

## First-Time Setup

**1 — Clone and copy env template:**

```bash
git clone https://github.com/ainzooalgownfer/Goscraper.git
cd Goscraper
cp .env.example .env
```

**2 — Generate a Tor control password hash:**

```bash
docker run --rm dockage/tor-privoxy:latest tor --hash-password yourpassword
```

**3 — Update `.env` with your password and hash:**

```bash
TOR_CONTROL_PASSWORD=yourpassword
TOR_CONTROL_PASSWORD_HASH=16:YOURHASHHERE
```

**4 — Generate `torrc` from template:**

```bash
chmod +x scripts/gen_torrc.sh
./scripts/gen_torrc.sh
```

**5 — Boot the stack:**

```bash
docker-compose down && docker-compose up --build
```

---

## API Endpoints

| Method   | Endpoint              | Description                                           |
|----------|-----------------------|-------------------------------------------------------|
| `GET`    | `/health`             | Basic health check                                    |
| `GET`    | `/health/deep`        | Deep check — DB, proxy pool, worker queue             |
| `GET`    | `/version`            | Go version and build info                             |
| `GET`    | `/metrics`            | Job counts and pool summary                           |
| `DELETE` | `/db/reset`           | Drop and recreate all tables                          |
| `POST`   | `/scrape`             | Submit an async scrape job                            |
| `POST`   | `/scrape/test`        | Synchronous scrape — returns result immediately       |
| `POST`   | `/scrape/bulk`        | Submit up to 50 jobs in one request                   |
| `GET`    | `/strategies`         | List available strategies and descriptions            |
| `GET`    | `/jobs`               | List all jobs (paginated, filterable by status)       |
| `GET`    | `/jobs/export`        | Export all jobs as JSON                               |
| `GET`    | `/jobs/stats`         | Breakdown by strategy and success rates               |
| `DELETE` | `/jobs`               | Delete all job records                                |
| `GET`    | `/jobs/:id`           | Get a specific job                                    |
| `DELETE` | `/jobs/:id`           | Delete a specific job                                 |
| `POST`   | `/jobs/:id/retry`     | Requeue a failed job                                  |
| `GET`    | `/pool/status`        | Proxy pool health and per-node stats                  |
| `POST`   | `/pool/reset`         | Reactivate all proxies and clear failure counts       |
| `POST`   | `/pool/rotate`        | Force NEWNYM on all Tor nodes — fresh exit IPs        |
| `GET`    | `/pool/node`          | Check current exit IP of a specific node              |
| `GET`    | `/swagger/*`          | Swagger UI                                            |

Full interactive docs: `http://localhost:8080/swagger/index.html`

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

### Bulk submission

```json
POST /scrape/bulk
{
  "jobs": [
    { "url": "http://books.toscrape.com", "strategy": "ecommerce" },
    { "url": "http://quotes.toscrape.com", "strategy": "custom", "selectors": { "quote": ".text" } },
    { "url": "https://news.ycombinator.com", "strategy": "title" }
  ]
}
```

### Synchronous test scrape

Returns the result immediately without saving to DB. Useful for testing selectors in Swagger before committing to a full job run.

```json
POST /scrape/test
{
  "url": "http://quotes.toscrape.com",
  "strategy": "custom",
  "selectors": { "quote": ".text", "author": ".author" }
}
```

---

## Configuration

### `.env`

Never committed. Generated from `.env.example`.

```bash
TOR_CONTROL_PASSWORD=yourpassword
TOR_CONTROL_PASSWORD_HASH=16:YOURHASHHERE
```

### `torrc.template`

Template for the Tor config file. Rendered into `torrc` by `gen_torrc.sh`. Contains:

- `ControlPort 0.0.0.0:9052` — exposes control port to the Docker network for NEWNYM rotation
- `HashedControlPassword` — injected from `.env` at render time
- `NewCircuitPeriod 10` — builds a new circuit every 10 seconds
- `MaxCircuitDirtiness 10` — retires circuits after 10 seconds
- `EnforceDistinctSubnets 1` — prevents Tor from routing two hops through the same `/16` subnet

> `ControlPort 0.0.0.0:9052` is only exposed within the Docker network — port 9052 is never published externally in `docker-compose.yml`. Do not add it to the `ports` block.

### `haproxy.cfg`

Distributes connections across the five named Tor backends using round-robin:

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

Tor containers take ~30-60 seconds to bootstrap. The `test_tor.sh` script waits automatically and aborts if any node fails to reach `Bootstrapped 100%`.

---

## Verifying Proxy Isolation

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

Pass a custom node count or bootstrap timeout:

```bash
./scripts/test_tor.sh 5      # 5 nodes, default 90s timeout
./scripts/test_tor.sh 5 120  # 5 nodes, 120s timeout
```

---

## Running the Stress Test

```bash
./scripts/test_scraper.sh
```

The script runs in 5 steps — health check, pool rotation, strategy listing, job submission, and result collection. It waits intelligently for all jobs to finish using `/metrics` polling instead of a fixed sleep.

Manual scrape:

```bash
curl -X POST http://localhost:8080/scrape \
     -H "Content-Type: application/json" \
     -d '{"url":"http://quotes.toscrape.com","strategy":"custom","selectors":{"quote":".text","author":".author"}}'
```

Retrieve result:

```bash
curl http://localhost:8080/jobs/<YOUR_JOB_ID>
```

Check worker IP distribution:

```bash
docker-compose logs api | grep "Target sees IP"
```

---

## Proxy Pool Behavior

- Workers select proxies **randomly** on every job — no static worker-to-proxy binding
- **Soft failures** (403, 429, site blocks) increment a failure counter but never deactivate the proxy
- **Hard failures** (EOF, connection refused) increment a hard failure counter; after 5 hard failures the proxy enters a 60 second cooldown
- Proxies **auto-recover** after the cooldown period without requiring a restart
- `POST /pool/rotate` sends NEWNYM to all Tor nodes via the control port, forcing fresh exit IPs immediately
- `POST /pool/reset` reactivates all proxies and clears all counters

---

# Network Debugging

## Tor Containers Not Bootstrapping

```bash
docker logs <container-name> 2>&1 | grep -i "bootstrapped\|warn\|err"
```

If stuck below 100%:

```bash
docker-compose restart tor-node-1
```

## Circuit Rotation Failing

If `/pool/rotate` returns `control port unreachable`:

```bash
# verify control port is listening on 0.0.0.0
docker exec <container-name> ss -tlnp | grep 9052

# verify torrc has correct ControlPort
docker exec <container-name> grep ControlPort /etc/tor/torrc

# regenerate torrc and rebuild
./scripts/gen_torrc.sh
docker-compose down && docker-compose up --build
```

## All Workers Showing the Same Exit IP

- Workers hitting HAProxy instead of nodes directly — verify `main.go` proxy list points to `tor-node-X:8118`
- Circuits not rotating — run `POST /pool/rotate` before the scrape run
- Random selection collision — expected occasionally, mitigated by `NewCircuitPeriod 10`

## HTTPS Timeouts Through Tor

Some sites are slow over HTTPS through Tor. Use plain `http://` where possible. Retry failed jobs:

```bash
curl -X POST http://localhost:8080/jobs/<JOB_ID>/retry
```

## HAProxy Exits with Code 1

```bash
echo "" >> haproxy.cfg
docker-compose restart proxy-balancer
```

## Container Name Prefix

Container names depend on the folder name. Scripts auto-detect the prefix. Check manually:

```bash
docker ps --format "{{.Names}}" | grep tor
```

---

# Security Notes

- `ControlPort 0.0.0.0:9052` is intentionally bound to all interfaces **within the Docker network only** — it is never published to the host or internet
- The control password is stored in `.env` which is gitignored — never commit `.env`
- The hashed password in `torrc` is generated per deployment and also gitignored
- For cloud deployment, generate a new password and hash before deploying — treat any password that has been in a public repo as compromised

---

# Technology Stack

## Backend
- Go, Gin, Colly, SQLite (via GORM)

## Infrastructure
- Docker, Docker Compose, HAProxy, Tor, Privoxy

## Architecture Concepts
- Worker pools with randomized proxy selection
- Asynchronous job queue
- Pluggable scraping strategy pattern
- Per-node Tor circuit isolation with on-demand NEWNYM rotation
- Soft vs hard proxy failure classification
- Auto-recovering proxy cooldown
- Repository pattern
- Distributed services

---

# Known Issues

- **Aggressive anti-bot sites** (LinkedIn, Cloudflare-protected pages) cause repeated timeouts that drain the hard failure counter and temporarily deactivate proxies. Run `POST /pool/reset` after targeting such sites.
- **Workers can share the same exit IP** within a short time window due to random selection landing on the same Tor container. Run `POST /pool/rotate` before large scrape runs to ensure all nodes have fresh circuits.
- **HTTPS through Tor/Privoxy** is slower and less reliable than plain HTTP. Prefer `http://` targets where the site supports it.

---

# Planned

- Per-worker dedicated Tor node assignment to eliminate same-IP collisions
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
