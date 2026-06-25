# Distributed High-Velocity Tor Scraping API Cluster

A high-performance, fault-tolerant asynchronous web scraping engine built in Go. This system leverages a multi-worker concurrency pool, a native SQLite job repository, and a distributed network proxy matrix using **HAProxy** and scaled **Tor/Privoxy** instances to rotate exit nodes dynamically on every request.

---

## System Architecture

The project separates scraping logic from raw network adapters to provide a scalable scraping infrastructure.

1. **Go API Service (Gin Engine)**  
   Accepts scraping requests asynchronously, generates tracking IDs, and submits jobs to the internal worker pool.

2. **Worker Pool Engine**  
   Runs multiple concurrent Go routines that process scraping jobs from an execution queue.

3. **HAProxy (Layer 4 TCP Load Balancer)**  
   Acts as a centralized proxy gateway:

   ```
   http://proxy-balancer:8888
   ```

   It distributes worker requests across multiple backend containers using round-robin balancing.

4. **Tor Replicas (Privoxy Gateways)**  
   Multiple containers running isolated Tor instances. Privoxy converts HTTP proxy requests into Tor network traffic.

---

## Project Directory Structure

```text
├── cmd/
│   └── api/
│       └── main.go              # Application Entry Point
├── internal/
│   ├── api/
│   │   └── server.go            # Gin Server & HTTP Handlers
│   ├── proxy/
│   │   └── pool.go              # Proxy Tracker & Health Matrix
│   ├── scraper/
│   │   ├── scraper.go           # Colly Engine & HTTP Transport
│   │   └── worker_pool.go       # Worker Pool Coordination
│   └── storage/
│       └── repository.go        # SQLite Persistence Layer
├── docker-compose.yml           # Container Infrastructure
├── haproxy.cfg                  # Load Balancer Configuration
└── README.md                    # Documentation
```

---

## Getting Started

### Prerequisites

Install:

- Docker
- Docker Compose
- Go

The project is compatible with Linux, macOS, and Windows WSL2 environments.

---

## Starting the Infrastructure

Launch the API and a cluster of Tor nodes:

```bash
docker-compose down && docker-compose up --build --scale tor-node=5
```

The first startup may take some time because Tor containers need to initialize circuits and download network information.

Wait before sending scraping requests.

---

## Running Tests

Using the helper script:

```bash
./test_scraper.sh
```

Or manually send a scraping request:

```bash
curl -X POST http://localhost:8080/scrape \
     -H "Content-Type: application/json" \
     -d '{"url":"https://news.ycombinator.com"}'
```

Retrieve a job result:

```bash
curl http://localhost:8080/jobs/<YOUR_JOB_ID>
```

---

# Configuration

## Go Scraper Transport Layer

The scraper uses a custom HTTP transport to control proxy routing and isolate network communication.

Example:

```go
t := &http.Transport{
    DisableKeepAlives: true,
}

if proxyURL != "" {
    parsedProxy, _ := url.Parse(proxyURL)
    t.Proxy = http.ProxyURL(parsedProxy)
}

c.WithTransport(t)
```

The custom transport ensures that scraper traffic is routed through the configured proxy layer.

---

## HAProxy Configuration

HAProxy runs as a TCP proxy to distribute connections between Tor/Privoxy containers.

Example:

```haproxy
frontend http_proxy_in
    bind *:8888
    mode tcp
    default_backend tor_backend


backend tor_backend
    mode tcp
    balance roundrobin
    default-server resolvers docker_dns init-addr none
    server tor-node-1 tor-node:8118
```

Using TCP mode allows HTTPS proxy tunnels to pass without HTTP-level processing.

---

# Network Debugging

## Unexpected EOF or 503 Errors

Possible causes:

- HAProxy running in HTTP mode instead of TCP mode
- Tor containers not fully initialized
- Proxy services unavailable during startup

Solutions:

Restart the stack:

```bash
docker-compose down
docker-compose up --build
```

Ensure Tor containers have enough time to initialize.

---

## Identical Exit IP Addresses

Possible cause:

Tor circuits may remain active for some time before changing exits.

Possible solution:

Configure isolated Tor SOCKS sessions:

```text
SocksPort "0.0.0.0:9050 IsolateSocksAuth"
```

This forces separate circuit isolation between sessions.

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

- Worker pools
- Asynchronous jobs
- Proxy abstraction
- Repository pattern
- Distributed services

---

# Legal and Ethical Usage

This project is intended for public data collection, research, and educational purposes.

Users should:

- Respect website terms of service
- Follow robots.txt policies where applicable
- Avoid collecting personal data
- Implement rate limiting and responsible request patterns
- Respect privacy regulations such as GDPR and CCPA
