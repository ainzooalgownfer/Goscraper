package scraper

import (
	"context"
	"log"
	"strings"
	"sync"

	"scraper/internal/proxy"
	"scraper/internal/storage"
)

type WorkerPool struct {
	numWorkers int
	jobQueue   chan *storage.DBJob
	store      storage.JobRepository
	proxyPool  *proxy.ProxyPool
	scraper    Scraper
	wg         sync.WaitGroup
}

func NewWorkerPool(numWorkers int, queueSize int, store storage.JobRepository, proxyPool *proxy.ProxyPool, scraperEngine Scraper) *WorkerPool {
	return &WorkerPool{
		numWorkers: numWorkers,
		jobQueue:   make(chan *storage.DBJob, queueSize),
		store:      store,
		proxyPool:  proxyPool,
		scraper:    scraperEngine,
	}
}

func (wp *WorkerPool) Start(ctx context.Context) {
	for i := 1; i <= wp.numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker(ctx, i)
	}
	log.Printf(" Worker pool initialized with %d workers", wp.numWorkers)
}

func (wp *WorkerPool) Submit(job *storage.DBJob) {
	wp.jobQueue <- job
}

func (wp *WorkerPool) worker(ctx context.Context, id int) {
	defer wp.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-wp.jobQueue:
			if !ok {
				return
			}
			wp.processJob(id, job)
		}
	}
}

func (wp *WorkerPool) processJob(workerID int, job *storage.DBJob) {
	wp.store.UpdateStatus(job.ID, "processing", "")

	px, err := wp.proxyPool.GetNextProxy()
	if err != nil {
		log.Printf("[Worker %d]  Proxy error: %v", workerID, err)
		wp.store.UpdateStatus(job.ID, "failed", "")
		return
	}

	s := wp.scraper.(*DefaultScraper)
	ctx := context.Background()

	result, outboundIP, err := s.Scrape(ctx, job.URL, px)

	
	log.Printf("[Worker %d]  Target Server Sees IP: %-15s |  Scraping: %s", workerID, outboundIP, job.URL)

	if err != nil {
		errMsg := err.Error()

		if strings.Contains(errMsg, "Forbidden") || strings.Contains(errMsg, "403") {
			log.Printf("[Worker %d]  CRITICAL: Job %s halted. Target server fired an explicit anti-bot Firewall block.", workerID, job.ID)
		} else if strings.Contains(errMsg, "Too Many Requests") || strings.Contains(errMsg, "429") {
			log.Printf("[Worker %d]  WARNING: Job %s throttled. Server issued a 429 Rate Limit block.", workerID, job.ID)
		} else {
			log.Printf("[Worker %d]  Scraping failed for job %s: %v", workerID, job.ID, err)
		}

		wp.store.UpdateStatus(job.ID, "failed", "")
		wp.proxyPool.RecordFailure(px)
		return
	}

	wp.store.UpdateStatus(job.ID, "completed", result.Title)
	wp.proxyPool.RecordSuccess(px)

	log.Printf("[Worker %d] Job %s complete! Title saved.", workerID, job.ID)
}