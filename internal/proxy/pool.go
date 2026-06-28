package proxy

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	hardFailureThreshold = 5
	cooldownDuration     = 60 * time.Second
)

type Proxy struct {
	URL           string
	Success       int
	Failures      int
	HardFailures  int
	Active        bool
	DeactivatedAt time.Time
	LastUsedAt    time.Time
}

type ProxyStatus struct {
	URL           string    `json:"url"`
	Active        bool      `json:"active"`
	Success       int       `json:"success"`
	Failures      int       `json:"failures"`
	HardFailures  int       `json:"hard_failures"`
	DeactivatedAt time.Time `json:"deactivated_at,omitempty"`
	LastUsedAt    time.Time `json:"last_used_at,omitempty"`
}

type ProxyPool struct {
	mu      sync.RWMutex
	proxies []*Proxy
	rng     *rand.Rand
}

func NewProxyPool(urls []string) *ProxyPool {
	var pool []*Proxy
	for _, u := range urls {
		pool = append(pool, &Proxy{URL: u, Active: true})
	}
	return &ProxyPool{
		proxies: pool,
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *ProxyPool) GetNextProxy() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

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
	chosen.LastUsedAt = time.Now()
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

func (p *ProxyPool) RecordSuccess(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pr := range p.proxies {
		if pr.URL == url {
			pr.Success++
			pr.Failures = 0
			pr.HardFailures = 0
			break
		}
	}
}

func (p *ProxyPool) RecordFailure(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pr := range p.proxies {
		if pr.URL == url {
			pr.Failures++
			log.Printf("[ProxyPool] Soft failure for %s (total: %d)", url, pr.Failures)
			break
		}
	}
}

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
				log.Printf("[ProxyPool] Proxy %s deactivated — cooldown 60s", url)
			}
			break
		}
	}
}

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
			LastUsedAt:    px.LastUsedAt,
		}
	}
	return statuses
}

func (p *ProxyPool) Rotate() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	password := os.Getenv("TOR_CONTROL_PASSWORD")
	if password == "" {
		password = "goscraper" // fallback for local dev
	}

	var results []string
	for _, px := range p.proxies {
		if !px.Active {
			results = append(results, fmt.Sprintf("%s: skipped (inactive)", px.URL))
			continue
		}

		host := strings.TrimPrefix(px.URL, "http://")
		host = strings.Split(host, ":")[0]
		controlAddr := fmt.Sprintf("%s:9052", host)

		conn, err := net.DialTimeout("tcp", controlAddr, 5*time.Second)
		if err != nil {
			results = append(results, fmt.Sprintf("%s: control port unreachable (%v)", px.URL, err))
			continue
		}

		fmt.Fprintf(conn, "AUTHENTICATE \"%s\"\r\n", password)
		time.Sleep(100 * time.Millisecond)
		fmt.Fprintf(conn, "SIGNAL NEWNYM\r\n")
		conn.Close()
		time.Sleep(1 * time.Second)

		results = append(results, fmt.Sprintf("%s: circuit rotated", px.URL))
		log.Printf("[ProxyPool] NEWNYM sent to %s", px.URL)
	}
	return results
}

// GetNodeIP checks the current exit IP of a specific node by index (1-based)
func (p *ProxyPool) GetNodeIP(nodeURL string) (string, error) {
	p.mu.RLock()
	found := false
	for _, px := range p.proxies {
		if px.URL == nodeURL {
			found = true
			break
		}
	}
	p.mu.RUnlock()

	if !found {
		return "", fmt.Errorf("node %s not found in pool", nodeURL)
	}

	// connect through the node's privoxy to ip-api.com
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(nodeURL)),
		},
	}

	resp, err := client.Get("http://ip-api.com/json")
	if err != nil {
		return "", fmt.Errorf("failed to reach ip-api through %s: %w", nodeURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}

func mustParseURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}
