// internal/api/server.go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/swaggo/files"
	"github.com/swaggo/gin-swagger"
	"scraper/internal/config"
	"scraper/internal/scraper"
	"scraper/internal/storage"
)

type Server struct {
	cfg    *config.Config
	router *gin.Engine
	store  storage.JobRepository
	pool   *scraper.WorkerPool
}

type ScrapeRequest struct {
	URL string `json:"url" binding:"required,url"`
}

func NewServer(cfg *config.Config, pool *scraper.WorkerPool, repo storage.JobRepository) *Server {
	gin.SetMode(gin.DebugMode)

	r := gin.Default()
	s := &Server{
		cfg:    cfg,
		router: r,
		store:  repo,
		pool:   pool,
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	s.router.GET("/health", s.handleHealth)
	s.router.POST("/scrape", s.handleScrape)
	s.router.GET("/jobs/:id", s.handleGetJob)
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

func (s *Server) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func (s *Server) handleScrape(c *gin.Context) {
	var req ScrapeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valid URL parameter is required"})
		return
	}

	jobID := generateID()
	newJob := &storage.DBJob{
		ID:        jobID,
		URL:       req.URL,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	if err := s.store.Create(newJob); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write database record"})
		return
	}
	
	s.pool.Submit(newJob)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id": jobID,
		"status": newJob.Status,
	})
}

func (s *Server) handleGetJob(c *gin.Context) {
	id := c.Param("id")
	job, err := s.store.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job database record not found"})
		return
	}

	c.JSON(http.StatusOK, job)
}