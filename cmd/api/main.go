// cmd/api/main.go
package main

import (
	"context"
	"log"
	"os"

	"scraper/internal/api"
	"scraper/internal/config"
	"scraper/internal/proxy"
	"scraper/internal/scraper"
	"scraper/internal/storage"

	_ "scraper/docs"
)

func main() {
	configPath := "configs/config.yaml"

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Critical error loading configuration: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := os.MkdirAll("data", 0755); err != nil {
		log.Fatalf("Failed to create disk partition space: %v", err)
	}

	sqliteRepo, err := storage.NewSQLiteRepository("data/goscrape.db")
	if err != nil {
		log.Fatalf("Failed to spin up database runtime context: %v", err)
	}

	sampleProxies := []string{
		"http://proxy-balancer:8888",
	}

	proxyPool := proxy.NewProxyPool(sampleProxies)
	scraperEngine := scraper.NewDefaultScraper()

	pool := scraper.NewWorkerPool(cfg.Scraper.Parallelism, 100, sqliteRepo, proxyPool, scraperEngine)
	pool.Start(ctx)

	log.Printf("Starting GoScrape API server on port %s...", cfg.Server.Port)
	server := api.NewServer(cfg, pool, sqliteRepo)

	if err := server.Start(); err != nil {
		log.Fatalf("Failed to run HTTP server: %v", err)
	}
}
