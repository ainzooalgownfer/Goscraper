package scraper

import (
	"context"
	"log"
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
		w := &Worker{
			id:        i,
			store:     wp.store,
			proxyPool: wp.proxyPool,
			scraper:   wp.scraper.(*DefaultScraper),
		}
		go func() {
			defer wp.wg.Done()
			w.Run(ctx, wp.jobQueue)
		}()
	}
	log.Printf("Worker pool initialized with %d workers", wp.numWorkers)
}

func (wp *WorkerPool) Submit(job *storage.DBJob) {
	wp.jobQueue <- job
}

func (wp *WorkerPool) Wait() {
	wp.wg.Wait()
}