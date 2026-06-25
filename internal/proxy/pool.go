// internal/proxy/pool.go
package proxy

import (
	"errors"
	"log"
	"sync"
)

type Proxy struct {
	URL      string
	Success  int
	Failures int
	Active   bool
}

type ProxyPool struct {
	mu       sync.RWMutex
	proxies  []*Proxy
	currentIndex int
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
	}
}

func (p *ProxyPool) GetNextProxy() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.proxies) == 0 {
		return "", errors.New("no proxies available")
	}

	for i := 0 ; i < len(p.proxies); i++ {
		idx := (p.currentIndex + i) % len(p.proxies)
		target := p.proxies[idx]

		if target.Active{
			p.currentIndex = (idx + 1) % len(p.proxies)
			return target.URL, nil
		}
	}
	return "", errors.New("no active proxies available")
}


func (p *ProxyPool) RecordSuccess(url string) { 
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, pr := range p.proxies{
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
				log.Printf("Proxy %s has been deactivated from active pool rotation", url)
			}
			break
		}
	}
}
