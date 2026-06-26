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
│   │   ├── worker.go                # Single worker — job processing, error handling
│   │   └── worker_pool.go           # Worker pool orchestration
│   └── storage/
│       └── repository.go            # SQLite persistence layer
├── scripts/
│   ├── test_tor.sh                  # Tor exit IP isolation test
│   └── test_scraper.sh              # Full proxy pool stress test
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

Tor containers take ~30 seconds to bootstrap circuits on first start. Wait for all nodes to reach `Bootstrapped 100%` before sending scrape requests.

Verify Tor is ready:

```bash
docker logs scraper-tor-node-1-1 2>&1 | grep -i "bootstrapped 100"
```

---

## Verifying Proxy Isolation

Before running scrape jobs, confirm each Tor container has a distinct exit IP:

```bash
./scripts/test_tor.sh
```

Expected output:

```
  tor-node-1: 194.15.36.117
  tor-node-2: 192.42.116.99
  tor-node-3: 185.129.61.5
  tor-node-4: 185.220.101.17
  tor-node-5: 109.71.252.182

  Unique exit IPs : 5
  PASS — all containers have distinct exit IPs.
```

If any nodes share an IP, restart the stack and wait longer for circuits to stabilize.

---

## Running Tests

Full proxy pool stress test across easy, medium, and strict targets:

```bash
./scripts/test_scraper.sh
```

Manual scrape request:

```bash
curl -X POST http://localhost:8080/scrape \
     -H "Content-Type: application/json" \
     -d '{"url":"https://news.ycombinator.com"}'
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

- Workers select proxies randomly on every job — no static worker-to-proxy binding
- Failed proxies accumulate a failure counter; after 3 failures the proxy is deactivated from rotation
- Successful jobs increment the proxy's success counter
- Pool recovers automatically if a previously failed node comes back (requires restart)

---

# Network Debugging

## Tor Containers Not Bootstrapping

Check full bootstrap log:

```bash
docker logs scraper-tor-node-1-1 2>&1 | grep -i "bootstrapped\|warn\|err"
```

If stuck below 100%, wait longer or restart:

```bash
docker-compose restart tor-node-1
```

## All Workers Showing the Same Exit IP

Possible causes:

- `rotate.conf` not being applied — verify with:

```bash
docker exec scraper-tor-node-1-1 cat /etc/torrc.d/rotate.conf
```

- `SocksPort` defined twice (in `rotate.conf` and default torrc) — remove it from `rotate.conf`
- Workers hitting HAProxy instead of nodes directly — verify `main.go` proxy list points to `tor-node-X:8118`

## 400 Invalid Header / HTTPS Failures Through Privoxy

BusyBox wget inside the containers does not support HTTPS CONNECT tunneling. Use plain HTTP endpoints for container-level testing:

```bash
docker exec -e http_proxy=http://127.0.0.1:8118 \
  scraper-tor-node-1-1 \
  wget -q -T 15 -O - http://ip-api.com/json
```

## HAProxy Exits with Code 1

Missing newline at end of `haproxy.cfg`. Fix:

```bash
echo "" >> haproxy.cfg
docker-compose restart proxy-balancer
```

---

# Technology Stack

## Backend

- Go
- Gin
- Colly
- SQLite

## Infrastructure

- Docker
- Docker Compose
- HAProxy
- Tor
- Privoxy

## Architecture Concepts

- Worker pools with randomized proxy selection
- Asynchronous job queue
- Per-node circuit isolation
- Proxy health tracking with automatic deactivation
- Repository pattern
- Distributed services

---

# Legal and Ethical Usage

This project is intended for public data collection, research, and educational purposes.

Users should:

- Respect website terms of service
- Follow `robots.txt` policies where applicable
- Avoid collecting personal data
- Implement rate limiting and responsible request patterns
- Respect privacy regulations such as GDPR and CCPA