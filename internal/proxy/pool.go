package proxy

import (
	"errors"
	"log"
	"math/rand"
	"sync"
	"time"
)

// targeting Linkdlen broke the proxy pool....

type Proxy struct {
	URL      string
	Success  int
	Failures int
	Active   bool
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

	var active []*Proxy
	for _, px := range p.proxies {
		if px.Active {
			active = append(active, px)
		}
	}

	if len(active) == 0 {
		return "", errors.New("no active proxies available")
	}

	chosen := active[p.rng.Intn(len(active))]
    log.Printf("[ProxyPool] Selected proxy: %s (pool size: %d)", chosen.URL, len(active))
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
			log.Printf("Proxy failure recorded for %s (Total Failures: %d)", url, pr.Failures)

			if pr.Failures >= 3 {
				pr.Active = false
				log.Printf("Proxy %s deactivated from pool rotation", url)
			}
			break
		}
	}
}