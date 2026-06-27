package proxy

import (
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"
)

const (
	hardFailureThreshold = 5                // real connectivity failures before cooldown
	cooldownDuration     = 60 * time.Second // how long before a proxy is retried
)

type Proxy struct {
	URL           string
	Success       int
	Failures      int
	HardFailures  int // only connectivity failures — timeouts, refused, EOF
	Active        bool
	DeactivatedAt time.Time
}

type ProxyStatus struct {
	URL           string    `json:"url"`
	Active        bool      `json:"active"`
	Success       int       `json:"success"`
	Failures      int       `json:"failures"`
	HardFailures  int       `json:"hard_failures"`
	DeactivatedAt time.Time `json:"deactivated_at,omitempty"`
}

type ProxyPool struct {
	mu      sync.RWMutex
	proxies []*Proxy
	rng     *rand.Rand
}

func NewProxyPool(urls []string) *ProxyPool {
	var pool []*Proxy
	for _, u := range urls {
		pool = append(pool, &Proxy{
			URL:    u,
			Active: true,
		})
	}
	return &ProxyPool{
		proxies: pool,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *ProxyPool) GetNextProxy() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// auto-reactivate proxies that finished their cooldown
	for _, px := range p.proxies {
		if !px.Active && time.Since(px.DeactivatedAt) > cooldownDuration {
			px.Active = true
			px.HardFailures = 0
			px.Failures = 0
			log.Printf("[ProxyPool] Proxy %s reactivated after cooldown", px.URL)
		}
	}

	var active []*Proxy
	for _, px := range p.proxies {
		if px.Active {
			active = append(active, px)
		}
	}

	if len(active) == 0 {
		return "", errors.New("no active proxies — all nodes in cooldown, retry in 60s")
	}

	chosen := active[p.rng.Intn(len(active))]
	log.Printf("[ProxyPool] Selected: %s (active: %d/%d)", chosen.URL, len(active), len(p.proxies))
	return chosen.URL, nil
}

func (p *ProxyPool) GetAll() ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var active []string
	for _, px := range p.proxies {
		if px.Active {
			active = append(active, px.URL)
		}
	}
	if len(active) == 0 {
		return nil, errors.New("no active proxies")
	}
	return active, nil
}

// RecordSuccess resets failure counters on a successful scrape
func (p *ProxyPool) RecordSuccess(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pr := range p.proxies {
		if pr.URL == url {
			pr.Success++
			pr.Failures = 0 // reset on success
			pr.HardFailures = 0
			break
		}
	}
}

// RecordFailure records a soft failure — 403, 429, blocked by site
// does NOT deactivate the proxy, these are expected for strict sites
func (p *ProxyPool) RecordFailure(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pr := range p.proxies {
		if pr.URL == url {
			pr.Failures++
			log.Printf("[ProxyPool] Soft failure for %s (total: %d) — site block, proxy stays active", url, pr.Failures)
			break
		}
	}
}

// RecordHardFailure records a real connectivity failure — timeout, EOF, connection refused
// these indicate the proxy itself is broken, not just a site block
func (p *ProxyPool) RecordHardFailure(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pr := range p.proxies {
		if pr.URL == url {
			pr.HardFailures++
			log.Printf("[ProxyPool] Hard failure for %s (total: %d)", url, pr.HardFailures)

			if pr.HardFailures >= hardFailureThreshold {
				pr.Active = false
				pr.DeactivatedAt = time.Now()
				log.Printf("[ProxyPool] Proxy %s deactivated — too many connectivity failures, cooldown 60s", url)
			}
			break
		}
	}
}

// Reset reactivates all proxies and clears all counters — for the /pool/reset endpoint
func (p *ProxyPool) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pr := range p.proxies {
		pr.Active = true
		pr.Failures = 0
		pr.HardFailures = 0
		pr.DeactivatedAt = time.Time{}
	}
	log.Printf("[ProxyPool] Pool reset — all %d proxies reactivated", len(p.proxies))
}

// Status returns the current state of all proxies — for the /pool/status endpoint
func (p *ProxyPool) Status() []ProxyStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	statuses := make([]ProxyStatus, len(p.proxies))
	for i, px := range p.proxies {
		statuses[i] = ProxyStatus{
			URL:           px.URL,
			Active:        px.Active,
			Success:       px.Success,
			Failures:      px.Failures,
			HardFailures:  px.HardFailures,
			DeactivatedAt: px.DeactivatedAt,
		}
	}
	return statuses
}
