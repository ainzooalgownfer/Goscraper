package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"scraper/internal/config"
	"scraper/internal/proxy"
	"scraper/internal/scraper"
	"scraper/internal/storage"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

var buildTime = time.Now().Format(time.RFC3339)

type Server struct {
	cfg       *config.Config
	router    *gin.Engine
	store     storage.JobRepository
	pool      *scraper.WorkerPool
	proxyPool *proxy.ProxyPool
	scraper   *scraper.DefaultScraper
}

type ScrapeRequest struct {
	URL       string            `json:"url"       binding:"required,url" example:"https://books.toscrape.com"`
	Strategy  string            `json:"strategy"  example:"custom"`
	Selectors map[string]string `json:"selectors,omitempty"`
}

type BulkScrapeRequest struct {
	Jobs []ScrapeRequest `json:"jobs" binding:"required"`
}

func NewServer(cfg *config.Config, pool *scraper.WorkerPool, repo storage.JobRepository, proxyPool *proxy.ProxyPool, scraperEngine *scraper.DefaultScraper) *Server {
	gin.SetMode(gin.DebugMode)
	r := gin.Default()
	s := &Server{
		cfg:       cfg,
		router:    r,
		store:     repo,
		pool:      pool,
		proxyPool: proxyPool,
		scraper:   scraperEngine,
	}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// system
	s.router.GET("/health", s.handleHealth)
	s.router.GET("/health/deep", s.handleDeepHealth)
	s.router.GET("/metrics", s.handleMetrics)
	s.router.GET("/version", s.handleVersion)
	s.router.DELETE("/db/reset", s.handleDBReset)

	// jobs
	s.router.POST("/scrape", s.handleScrape)
	s.router.POST("/scrape/test", s.handleScrapeTest)
	s.router.POST("/scrape/bulk", s.handleBulkScrape)
	s.router.GET("/strategies", s.handleStrategies)
	s.router.GET("/jobs", s.handleListJobs)
	s.router.GET("/jobs/export", s.handleExportJobs)
	s.router.GET("/jobs/stats", s.handleJobStats)
	s.router.DELETE("/jobs", s.handleDeleteAllJobs)
	s.router.GET("/jobs/:id", s.handleGetJob)
	s.router.DELETE("/jobs/:id", s.handleDeleteJob)
	s.router.POST("/jobs/:id/retry", s.handleRetryJob)

	// pool
	s.router.GET("/pool/status", s.handlePoolStatus)
	s.router.POST("/pool/reset", s.handlePoolReset)
	s.router.POST("/pool/rotate", s.handlePoolRotate)
	s.router.GET("/pool/node", s.handlePoolNodeIP)

	s.router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
}

func (s *Server) Start() error {
	return s.router.Run(":" + s.cfg.Server.Port)
}

func generateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── System ────────────────────────────────────────────────────────────────────

// handleHealth godoc
// @Summary      Health check
// @Description  Returns API health status
// @Tags         system
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /health [get]
func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

// handleDeepHealth godoc
// @Summary      Deep health check
// @Description  Checks DB connection, proxy pool, and worker queue depth
// @Tags         system
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]interface{}
// @Router       /health/deep [get]
func (s *Server) handleDeepHealth(c *gin.Context) {
	issues := []string{}

	// check DB
	if _, err := s.store.Metrics(); err != nil {
		issues = append(issues, "database unreachable")
	}

	// check pool
	statuses := s.proxyPool.Status()
	active := 0
	for _, ps := range statuses {
		if ps.Active {
			active++
		}
	}
	if active == 0 {
		issues = append(issues, "no active proxies")
	}

	status := "healthy"
	code := http.StatusOK
	if len(issues) > 0 {
		status = "degraded"
		code = http.StatusInternalServerError
	}

	c.JSON(code, gin.H{
		"status":         status,
		"issues":         issues,
		"active_proxies": active,
		"total_proxies":  len(statuses),
	})
}

// handleVersion godoc
// @Summary      Version info
// @Description  Returns Go version and build time
// @Tags         system
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /version [get]
func (s *Server) handleVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"go_version": runtime.Version(),
		"build_time": buildTime,
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
	})
}

// handleMetrics godoc
// @Summary      API metrics
// @Description  Returns job counts and proxy pool summary
// @Tags         system
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]string
// @Router       /metrics [get]
func (s *Server) handleMetrics(c *gin.Context) {
	metrics, err := s.store.Metrics()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch metrics"})
		return
	}
	statuses := s.proxyPool.Status()
	active := 0
	for _, ps := range statuses {
		if ps.Active {
			active++
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"jobs": metrics,
		"pool": gin.H{"active": active, "total": len(statuses)},
	})
}

// handleDBReset godoc
// @Summary      Reset database
// @Description  Drops and recreates all tables
// @Tags         system
// @Produce      json
// @Success      200  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /db/reset [delete]
func (s *Server) handleDBReset(c *gin.Context) {
	if err := s.store.ResetDB(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset database"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Database reset — all tables dropped and recreated"})
}

// ── Scrape ────────────────────────────────────────────────────────────────────

// handleScrape godoc
// @Summary      Submit a scrape job
// @Description  Submits a URL to the worker pool asynchronously
// @Tags         jobs
// @Accept       json
// @Produce      json
// @Param        request body ScrapeRequest true "Scrape request"
// @Success      202  {object}  map[string]string
// @Failure      400  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /scrape [post]
func (s *Server) handleScrape(c *gin.Context) {
	var req ScrapeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valid URL parameter is required"})
		return
	}
	if req.Strategy == "" {
		req.Strategy = "title"
	}
	if req.Strategy == "custom" && len(req.Selectors) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "custom strategy requires at least one selector"})
		return
	}

	jobID := generateID()
	newJob := &storage.DBJob{
		ID:        jobID,
		URL:       req.URL,
		Status:    "pending",
		Strategy:  req.Strategy,
		Selectors: storage.EncodeSelectors(req.Selectors),
		CreatedAt: time.Now(),
	}
	if err := s.store.Create(newJob); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write database record"})
		return
	}
	s.pool.Submit(newJob)
	c.JSON(http.StatusAccepted, gin.H{
		"job_id":   jobID,
		"status":   newJob.Status,
		"strategy": newJob.Strategy,
	})
}

// handleScrapeTest godoc
// @Summary      Synchronous test scrape
// @Description  Scrapes a URL immediately and returns the result without saving to DB. Useful for testing selectors.
// @Tags         jobs
// @Accept       json
// @Produce      json
// @Param        request body ScrapeRequest true "Scrape request"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /scrape/test [post]
func (s *Server) handleScrapeTest(c *gin.Context) {
	var req ScrapeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valid URL parameter is required"})
		return
	}
	if req.Strategy == "" {
		req.Strategy = "title"
	}
	if req.Strategy == "custom" && len(req.Selectors) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "custom strategy requires at least one selector"})
		return
	}

	px, err := s.proxyPool.GetNextProxy()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "No active proxies available"})
		return
	}

	strategy := scraper.ResolveStrategy(req.Strategy, req.Selectors)
	result, outboundIP, err := s.scraper.Scrape(c.Request.Context(), req.URL, px, strategy)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":      err.Error(),
			"proxy_used": px,
			"exit_ip":    outboundIP,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"url":        result.URL,
		"title":      result.Title,
		"data":       result.Data,
		"exit_ip":    outboundIP,
		"proxy_used": px,
		"scraped_at": result.ScrapedAt,
	})
}

// handleBulkScrape godoc
// @Summary      Submit multiple scrape jobs
// @Description  Submits multiple URLs in one request, each with its own strategy
// @Tags         jobs
// @Accept       json
// @Produce      json
// @Param        request body BulkScrapeRequest true "Bulk scrape request"
// @Success      202  {object}  map[string]interface{}
// @Failure      400  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /scrape/bulk [post]
func (s *Server) handleBulkScrape(c *gin.Context) {
	var req BulkScrapeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	if len(req.Jobs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "At least one job is required"})
		return
	}
	if len(req.Jobs) > 50 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Maximum 50 jobs per bulk request"})
		return
	}

	var queued []gin.H
	var failed []gin.H

	for _, j := range req.Jobs {
		if j.Strategy == "" {
			j.Strategy = "title"
		}
		if j.Strategy == "custom" && len(j.Selectors) == 0 {
			failed = append(failed, gin.H{"url": j.URL, "error": "custom strategy requires selectors"})
			continue
		}

		jobID := generateID()
		newJob := &storage.DBJob{
			ID:        jobID,
			URL:       j.URL,
			Status:    "pending",
			Strategy:  j.Strategy,
			Selectors: storage.EncodeSelectors(j.Selectors),
			CreatedAt: time.Now(),
		}
		if err := s.store.Create(newJob); err != nil {
			failed = append(failed, gin.H{"url": j.URL, "error": "failed to create job"})
			continue
		}
		s.pool.Submit(newJob)
		queued = append(queued, gin.H{"job_id": jobID, "url": j.URL, "strategy": j.Strategy})
	}

	c.JSON(http.StatusAccepted, gin.H{
		"queued": queued,
		"failed": failed,
		"total":  len(req.Jobs),
	})
}

// handleStrategies godoc
// @Summary      List available strategies
// @Description  Returns all available scraping strategies and their descriptions
// @Tags         jobs
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /strategies [get]
func (s *Server) handleStrategies(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"strategies": []gin.H{
			{
				"name":               "title",
				"description":        "Extracts page title and meta description. Default strategy.",
				"selectors_required": false,
			},
			{
				"name":               "news",
				"description":        "Extracts page title, first h1 heading, and article paragraph text.",
				"selectors_required": false,
			},
			{
				"name":               "ecommerce",
				"description":        "Extracts product name, price, and availability using itemprop schema attributes.",
				"selectors_required": false,
			},
			{
				"name":               "custom",
				"description":        "Caller defines CSS selectors at request time. Key is field name, value is CSS selector.",
				"selectors_required": true,
				"example": map[string]string{
					"quote":  ".text",
					"author": ".author",
					"tags":   ".tag",
				},
			},
		},
	})
}

// ── Jobs ──────────────────────────────────────────────────────────────────────

// handleListJobs godoc
// @Summary      List all jobs
// @Description  Returns paginated list of scrape jobs with optional status filter
// @Tags         jobs
// @Produce      json
// @Param        page    query  int     false  "Page number (default 1)"
// @Param        limit   query  int     false  "Page size (default 20, max 100)"
// @Param        status  query  string  false  "Filter: pending, processing, completed, failed"
// @Success      200  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]string
// @Router       /jobs [get]
func (s *Server) handleListJobs(c *gin.Context) {
	page, limit := 1, 20
	if p := c.Query("page"); p != "" {
		fmt.Sscanf(p, "%d", &page)
	}
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	jobs, total, err := s.store.List(page, limit, c.Query("status"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch jobs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs, "total": total, "page": page, "limit": limit})
}

// handleJobStats godoc
// @Summary      Job statistics
// @Description  Returns breakdown by strategy and success rates
// @Tags         jobs
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]string
// @Router       /jobs/stats [get]
func (s *Server) handleJobStats(c *gin.Context) {
	stats, err := s.store.Stats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch stats"})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// handleExportJobs godoc
// @Summary      Export all jobs
// @Description  Returns all job records as a JSON array for download
// @Tags         jobs
// @Produce      json
// @Success      200  {array}   storage.DBJob
// @Failure      500  {object}  map[string]string
// @Router       /jobs/export [get]
func (s *Server) handleExportJobs(c *gin.Context) {
	jobs, err := s.store.ExportAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to export jobs"})
		return
	}
	c.Header("Content-Disposition", "attachment; filename=jobs_export.json")
	c.JSON(http.StatusOK, gin.H{"exported": len(jobs), "jobs": jobs})
}

// handleDeleteAllJobs godoc
// @Summary      Delete all jobs
// @Description  Wipes all job records without dropping the table
// @Tags         jobs
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Failure      500  {object}  map[string]string
// @Router       /jobs [delete]
func (s *Server) handleDeleteAllJobs(c *gin.Context) {
	count, err := s.store.DeleteAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete jobs"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "All jobs deleted", "deleted": count})
}

// handleGetJob godoc
// @Summary      Get a scrape job
// @Description  Retrieves the details of a specific scrape job
// @Tags         jobs
// @Produce      json
// @Param        id path string true "Job ID"
// @Success      200  {object}  storage.DBJob
// @Failure      404  {object}  map[string]string
// @Router       /jobs/{id} [get]
func (s *Server) handleGetJob(c *gin.Context) {
	job, err := s.store.Get(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

// handleDeleteJob godoc
// @Summary      Delete a job
// @Description  Deletes a single job record by ID
// @Tags         jobs
// @Produce      json
// @Param        id path string true "Job ID"
// @Success      200  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /jobs/{id} [delete]
func (s *Server) handleDeleteJob(c *gin.Context) {
	id := c.Param("id")
	if _, err := s.store.Get(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}
	if err := s.store.Delete(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete job"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Job deleted", "job_id": id})
}

// handleRetryJob godoc
// @Summary      Retry a failed job
// @Description  Requeues a failed job back into the worker pool
// @Tags         jobs
// @Produce      json
// @Param        id path string true "Job ID"
// @Success      200  {object}  map[string]string
// @Failure      400  {object}  map[string]string
// @Failure      404  {object}  map[string]string
// @Router       /jobs/{id}/retry [post]
func (s *Server) handleRetryJob(c *gin.Context) {
	id := c.Param("id")
	job, err := s.store.Retry(id)
	if err != nil {
		if err.Error() == "job not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.pool.Submit(job)
	c.JSON(http.StatusOK, gin.H{
		"message":  "Job requeued",
		"job_id":   job.ID,
		"strategy": job.Strategy,
	})
}

// ── Pool ──────────────────────────────────────────────────────────────────────

// handlePoolStatus godoc
// @Summary      Proxy pool status
// @Description  Returns health and stats for each proxy in the pool
// @Tags         pool
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /pool/status [get]
func (s *Server) handlePoolStatus(c *gin.Context) {
	statuses := s.proxyPool.Status()
	active := 0
	for _, ps := range statuses {
		if ps.Active {
			active++
		}
	}
	c.JSON(http.StatusOK, gin.H{"proxies": statuses, "active": active, "total": len(statuses)})
}

// handlePoolReset godoc
// @Summary      Reset proxy pool
// @Description  Reactivates all proxies and clears failure counters
// @Tags         pool
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /pool/reset [post]
func (s *Server) handlePoolReset(c *gin.Context) {
	s.proxyPool.Reset()
	c.JSON(http.StatusOK, gin.H{"message": "Proxy pool reset — all nodes reactivated"})
}

// handlePoolRotate godoc
// @Summary      Rotate all Tor circuits
// @Description  Sends NEWNYM signal to all active Tor nodes forcing fresh exit IPs
// @Tags         pool
// @Produce      json
// @Success      200  {object}  map[string]interface{}
// @Router       /pool/rotate [post]
func (s *Server) handlePoolRotate(c *gin.Context) {
	results := s.proxyPool.Rotate()
	c.JSON(http.StatusOK, gin.H{
		"message": "Circuit rotation complete",
		"results": results,
	})
}

// handlePoolNodeIP godoc
// @Summary      Get current exit IP of a node
// @Description  Checks what exit IP a specific Tor node is currently using
// @Tags         pool
// @Produce      json
// @Param        url  query  string  true  "Proxy URL e.g. http://tor-node-1:8118"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  map[string]string
// @Failure      500  {object}  map[string]string
// @Router       /pool/node [get]
func (s *Server) handlePoolNodeIP(c *gin.Context) {
	nodeURL := c.Query("url")
	if nodeURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url query parameter is required"})
		return
	}

	result, err := s.proxyPool.GetNodeIP(nodeURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"node": nodeURL,
		"info": result,
	})
}
