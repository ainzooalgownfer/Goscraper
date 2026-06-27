package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"scraper/internal/config"
	"scraper/internal/proxy"
	"scraper/internal/scraper"
	"scraper/internal/storage"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Server struct {
	cfg       *config.Config
	router    *gin.Engine
	store     storage.JobRepository
	pool      *scraper.WorkerPool
	proxyPool *proxy.ProxyPool
}

// ScrapeRequest represents the scrape job submission payload
// @Description Scrape request body
type ScrapeRequest struct {
	URL       string            `json:"url"       binding:"required,url" example:"https://books.toscrape.com"`
	Strategy  string            `json:"strategy"  example:"custom"`
	Selectors map[string]string `json:"selectors,omitempty"`
}

func NewServer(cfg *config.Config, pool *scraper.WorkerPool, repo storage.JobRepository, proxyPool *proxy.ProxyPool) *Server {
	gin.SetMode(gin.DebugMode)
	r := gin.Default()
	s := &Server{cfg: cfg, router: r, store: repo, pool: pool, proxyPool: proxyPool}
	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/health", s.handleHealth)
	s.router.POST("/scrape", s.handleScrape)
	s.router.GET("/jobs", s.handleListJobs)
	s.router.GET("/jobs/export", s.handleExportJobs)
	s.router.DELETE("/jobs", s.handleDeleteAllJobs)
	s.router.GET("/jobs/:id", s.handleGetJob)
	s.router.DELETE("/jobs/:id", s.handleDeleteJob)
	s.router.POST("/jobs/:id/retry", s.handleRetryJob)
	s.router.GET("/pool/status", s.handlePoolStatus)
	s.router.POST("/pool/reset", s.handlePoolReset)
	s.router.GET("/metrics", s.handleMetrics)
	s.router.DELETE("/db/reset", s.handleDBReset)
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

// handleScrape godoc
// @Summary      Submit a scrape job
// @Description  Submits a URL to the worker pool. Supports title, news, ecommerce, custom strategies.
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
// @Description  Wipes all job records from the database without dropping the table
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
// @Failure      500  {object}  map[string]string
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
// @Description  Reactivates all deactivated proxies and clears failure counters
// @Tags         pool
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /pool/reset [post]
func (s *Server) handlePoolReset(c *gin.Context) {
	s.proxyPool.Reset()
	c.JSON(http.StatusOK, gin.H{"message": "Proxy pool reset — all nodes reactivated"})
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
// @Description  Drops and recreates all tables — full wipe including schema
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
