package scraper

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"time"

	"scraper/internal/proxy"
	"scraper/internal/storage"
)

type Worker struct {
	id        int
	store     storage.JobRepository
	proxyPool *proxy.ProxyPool
	scraper   *DefaultScraper
}

func (w *Worker) Run(ctx context.Context, jobs <-chan *storage.DBJob) {
	jitter := time.Duration(rand.Intn(500)) * time.Millisecond
	select {
	case <-time.After(jitter):
	case <-ctx.Done():
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			w.process(job)
		}
	}
}

func (w *Worker) process(job *storage.DBJob) {
	w.store.UpdateStatus(job.ID, "processing", "")

	px, err := w.proxyPool.GetNextProxy()
	if err != nil {
		log.Printf("[Worker %d] Proxy error: %v", w.id, err)
		w.store.UpdateStatus(job.ID, "failed", "")
		return
	}

	strategy := ResolveStrategy(job.Strategy, job.ParseSelectors())

	result, outboundIP, err := w.scraper.Scrape(context.Background(), job.URL, px, strategy)
	log.Printf("[Worker %d] Target sees IP: %-15s | Scraping: %s", w.id, outboundIP, job.URL)

	if err != nil {
		w.handleScrapeError(job.ID, px, err)
		return
	}

	w.store.UpdateStatus(job.ID, "completed", result.Title)
	w.proxyPool.RecordSuccess(px)
	log.Printf("[Worker %d] Job %s complete.", w.id, job.ID)
}

func (w *Worker) handleScrapeError(jobID string, px string, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "403") || strings.Contains(msg, "Forbidden"):
		log.Printf("[Worker %d] BLOCKED (403) on job %s — site block, proxy stays active", w.id, jobID)
		w.proxyPool.RecordFailure(px) // soft — site blocked us, not a proxy issue

	case strings.Contains(msg, "429") || strings.Contains(msg, "Too Many Requests"):
		log.Printf("[Worker %d] THROTTLED (429) on job %s — rate limited", w.id, jobID)
		w.proxyPool.RecordFailure(px) // soft — rate limit, not a proxy issue

	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "EOF") || strings.Contains(msg, "connection refused"):
		log.Printf("[Worker %d] CONNECTIVITY FAILURE on job %s: %v", w.id, jobID, err)
		w.proxyPool.RecordHardFailure(px)

	default:
		log.Printf("[Worker %d] Scrape failed for job %s: %v", w.id, jobID, err)
		w.proxyPool.RecordFailure(px) // unknown — treat as soft
	}
	w.store.UpdateStatus(jobID, "failed", "")
}
