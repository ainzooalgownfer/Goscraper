package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"scraper/internal/scraper"
	"scraper/internal/storage"

	"github.com/gin-gonic/gin"
)

var pageTemplates map[string]*template.Template

func (s *Server) loadTemplates() {
	base := filepath.Join("web", "templates", "base.html")
	pages := []string{"dashboard", "submit", "jobs", "pool"}

	pageTemplates = make(map[string]*template.Template)
	for _, page := range pages {
		pagePath := filepath.Join("web", "templates", page+".html")
		pageTemplates[page] = template.Must(template.ParseFiles(base, pagePath))
	}
}

func (s *Server) renderPage(c *gin.Context, page string, data gin.H) {
	t, ok := pageTemplates[page]
	if !ok {
		c.String(http.StatusNotFound, "template not found: "+page)
		return
	}
	c.Writer.Header().Set("Content-Type", "text/html")
	if err := t.ExecuteTemplate(c.Writer, "base.html", data); err != nil {
		c.String(http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) setupUIRoutes() {
	s.router.Static("/static", "web/static")

	// pages
	s.router.GET("/ui", s.uiDashboard)
	s.router.GET("/ui/submit", s.uiSubmit)
	s.router.GET("/ui/jobs", s.uiJobs)
	s.router.GET("/ui/pool", s.uiPool)

	// partials — HTMX fragments
	s.router.GET("/ui/partials/metrics", s.partialMetrics)
	s.router.GET("/ui/partials/pool-mini", s.partialPoolMini)
	s.router.GET("/ui/partials/pool-nodes", s.partialPoolNodes)
	s.router.GET("/ui/partials/pool-summary", s.partialPoolSummary)
	s.router.GET("/ui/partials/recent-jobs", s.partialRecentJobs)
	s.router.GET("/ui/partials/jobs-table", s.partialJobsTable)

	// actions — form submissions
	s.router.POST("/ui/actions/scrape", s.actionScrape)
	s.router.POST("/ui/actions/scrape-test", s.actionScrapeTest)
	s.router.POST("/ui/actions/pool-rotate", s.actionPoolRotate)
	s.router.POST("/ui/actions/pool-reset", s.actionPoolReset)
	s.router.DELETE("/ui/actions/jobs-all", s.actionDeleteAllJobs)
	s.router.POST("/ui/actions/retry/:id", s.actionRetryJob)
	s.router.DELETE("/ui/actions/job/:id", s.actionDeleteJob)
}

// ── Pages ─────────────────────────────────────────────────────────────────────

func (s *Server) uiDashboard(c *gin.Context) {
	s.renderPage(c, "dashboard", gin.H{"Page": "dashboard"})
}
func (s *Server) uiSubmit(c *gin.Context) {
	s.renderPage(c, "submit", gin.H{"Page": "submit"})
}
func (s *Server) uiJobs(c *gin.Context) {
	s.renderPage(c, "jobs", gin.H{"Page": "jobs"})
}
func (s *Server) uiPool(c *gin.Context) {
	s.renderPage(c, "pool", gin.H{"Page": "pool"})
}

// ── Partials ──────────────────────────────────────────────────────────────────

func (s *Server) partialMetrics(c *gin.Context) {
	metrics, _ := s.store.Metrics()
	statuses := s.proxyPool.Status()
	active := 0
	for _, ps := range statuses {
		if ps.Active {
			active++
		}
	}

	total := int64(0)
	completed := int64(0)
	failed := int64(0)
	pending := int64(0)
	processing := int64(0)

	if v, ok := metrics["total"].(int64); ok {
		total = v
	}
	if v, ok := metrics["completed"].(int64); ok {
		completed = v
	}
	if v, ok := metrics["failed"].(int64); ok {
		failed = v
	}
	if v, ok := metrics["pending"].(int64); ok {
		pending = v
	}
	if v, ok := metrics["processing"].(int64); ok {
		processing = v
	}

	successRate := 0.0
	if total > 0 {
		successRate = float64(completed) / float64(total) * 100
	}

	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `
	<div class="card">
		<div class="metric-value">%d</div>
		<div class="metric-label">Total Jobs</div>
	</div>
	<div class="card">
		<div class="metric-value" style="color:var(--green)">%d</div>
		<div class="metric-label">Completed</div>
	</div>
	<div class="card">
		<div class="metric-value" style="color:var(--red)">%d</div>
		<div class="metric-label">Failed</div>
	</div>
	<div class="card">
		<div class="metric-value" style="color:var(--accent)">%d/%d</div>
		<div class="metric-label">Active Proxies</div>
	</div>
	<div class="card">
		<div class="metric-value" style="color:var(--yellow)">%d</div>
		<div class="metric-label">Pending</div>
	</div>
	<div class="card">
		<div class="metric-value" style="color:var(--blue)">%d</div>
		<div class="metric-label">Processing</div>
	</div>
	<div class="card">
		<div class="metric-value" style="color:var(--green)">%.1f%%</div>
		<div class="metric-label">Success Rate</div>
	</div>
	`, total, completed, failed, active, len(statuses), pending, processing, successRate)
}

func (s *Server) partialPoolMini(c *gin.Context) {
	statuses := s.proxyPool.Status()
	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `<div class="card-title">Proxy Pool</div>`)
	for _, ps := range statuses {
		dot := "dot-green"
		badge := `<span class="badge badge-green">active</span>`
		if !ps.Active {
			dot = "dot-red"
			badge = `<span class="badge badge-red">cooldown</span>`
		}
		name := ps.URL
		name = strings.TrimPrefix(name, "http://")
		name = strings.Split(name, ":")[0]
		fmt.Fprintf(c.Writer, `
		<div class="node-row">
			<div class="flex items-center gap-8">
				<span class="node-dot %s"></span>
				<span style="font-size:13px;font-weight:500">%s</span>
			</div>
			<div class="flex items-center gap-8">
				%s
				<span class="text-muted">✓%d ✗%d</span>
			</div>
		</div>`, dot, name, badge, ps.Success, ps.Failures)
	}
}

func (s *Server) partialPoolNodes(c *gin.Context) {
	statuses := s.proxyPool.Status()
	c.Writer.Header().Set("Content-Type", "text/html")
	for _, ps := range statuses {
		dot := "dot-green"
		statusBadge := `<span class="badge badge-green">active</span>`
		if !ps.Active {
			dot = "dot-red"
			statusBadge = `<span class="badge badge-red">cooldown</span>`
		}
		name := strings.TrimPrefix(ps.URL, "http://")
		name = strings.Split(name, ":")[0]
		fmt.Fprintf(c.Writer, `
		<div class="node-row">
			<div>
				<div class="flex items-center gap-8" style="margin-bottom:4px">
					<span class="node-dot %s"></span>
					<span style="font-weight:600;font-size:14px">%s</span>
					%s
				</div>
				<div class="node-stats">
					<span>✓ success: %d</span>
					<span>⚠ soft: %d</span>
					<span>✗ hard: %d</span>
				</div>
			</div>
		</div>`, dot, name, statusBadge, ps.Success, ps.Failures, ps.HardFailures)
	}
}

func (s *Server) partialPoolSummary(c *gin.Context) {
	statuses := s.proxyPool.Status()
	active, total := 0, len(statuses)
	totalSuccess, totalFail, totalHard := 0, 0, 0
	for _, ps := range statuses {
		if ps.Active {
			active++
		}
		totalSuccess += ps.Success
		totalFail += ps.Failures
		totalHard += ps.HardFailures
	}
	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `
	<div class="node-row">
		<span class="text-muted">Active nodes</span>
		<span style="font-weight:600;color:var(--green)">%d / %d</span>
	</div>
	<div class="node-row">
		<span class="text-muted">Total successful scrapes</span>
		<span style="font-weight:600;color:var(--green)">%d</span>
	</div>
	<div class="node-row">
		<span class="text-muted">Total soft failures</span>
		<span style="font-weight:600;color:var(--yellow)">%d</span>
	</div>
	<div class="node-row">
		<span class="text-muted">Total hard failures</span>
		<span style="font-weight:600;color:var(--red)">%d</span>
	</div>
	`, active, total, totalSuccess, totalFail, totalHard)
}

func (s *Server) partialRecentJobs(c *gin.Context) {
	jobs, _, _ := s.store.List(1, 5, "")
	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `<div class="card-title">Recent Jobs</div>`)
	if len(jobs) == 0 {
		fmt.Fprintf(c.Writer, `<div class="text-muted">No jobs yet.</div>`)
		return
	}
	for _, job := range jobs {
		badgeClass, badgeText := statusBadge(job.Status)
		shortURL := job.URL
		if len(shortURL) > 35 {
			shortURL = shortURL[:35] + "…"
		}
		fmt.Fprintf(c.Writer, `
		<div class="node-row">
			<div>
				<div style="font-size:13px;font-weight:500;margin-bottom:2px">%s</div>
				<div class="text-muted">%s</div>
			</div>
			<div class="flex items-center gap-8">
				<span class="badge badge-%s">%s</span>
				<span class="text-muted" style="font-size:11px">%s</span>
			</div>
		</div>`, shortURL, job.ID, badgeClass, badgeText,
			job.CreatedAt.Format("15:04:05"))
	}
}

func (s *Server) partialJobsTable(c *gin.Context) {
	status := c.Query("status")
	page := 1
	jobs, total, _ := s.store.List(page, 20, status)
	c.Writer.Header().Set("Content-Type", "text/html")

	if len(jobs) == 0 {
		fmt.Fprintf(c.Writer, `<div class="text-muted" style="padding:16px">No jobs found.</div>`)
		return
	}

	fmt.Fprintf(c.Writer, `
	<div class="table-wrap">
		<table>
			<thead>
				<tr>
					<th>ID</th>
					<th>URL</th>
					<th>Strategy</th>
					<th>Status</th>
					<th>Result</th>
					<th>Created</th>
					<th>Actions</th>
				</tr>
			</thead>
			<tbody>`)

	for _, job := range jobs {
		badgeClass, badgeText := statusBadge(job.Status)
		shortURL := job.URL
		if len(shortURL) > 30 {
			shortURL = shortURL[:30] + "…"
		}
		title := job.ResultTitle
		if len(title) > 30 {
			title = title[:30] + "…"
		}

		retryBtn := ""
		if job.Status == "failed" {
			retryBtn = fmt.Sprintf(`
			<button class="btn btn-ghost btn-sm"
				hx-post="/ui/actions/retry/%s"
				hx-target="closest tr"
				hx-swap="outerHTML">↺</button>`, job.ID)
		}

		fmt.Fprintf(c.Writer, `
			<tr>
				<td><code style="font-size:11px">%s</code></td>
				<td><span class="truncate" title="%s">%s</span></td>
				<td><span class="badge badge-purple">%s</span></td>
				<td><span class="badge badge-%s">%s</span></td>
				<td class="text-muted" style="font-size:12px">%s</td>
				<td class="text-muted" style="font-size:12px">%s</td>
				<td class="flex gap-8">
					%s
					<button class="btn btn-danger btn-sm"
						hx-delete="/ui/actions/job/%s"
						hx-target="closest tr"
						hx-swap="outerHTML"
						hx-confirm="Delete this job?">🗑</button>
				</td>
			</tr>`,
			job.ID, job.URL, shortURL,
			job.Strategy,
			badgeClass, badgeText,
			title,
			job.CreatedAt.Format("01/02 15:04"),
			retryBtn,
			job.ID)
	}

	fmt.Fprintf(c.Writer, `</tbody></table>`)
	fmt.Fprintf(c.Writer, `<div class="text-muted" style="padding:12px 0;font-size:12px">%d total jobs</div>`, total)
	fmt.Fprintf(c.Writer, `</div>`)
}

// ── Actions ───────────────────────────────────────────────────────────────────

func (s *Server) actionScrape(c *gin.Context) {
	url := c.PostForm("url")
	strategy := c.PostForm("strategy")
	if strategy == "" {
		strategy = "title"
	}

	selectors := buildSelectors(c, "sel_key[]", "sel_value[]")
	if strategy == "custom" && len(selectors) == 0 {
		c.Writer.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(c.Writer, `<div class="alert alert-error">Custom strategy requires at least one selector.</div>`)
		return
	}

	jobID := generateID()
	newJob := &storage.DBJob{
		ID:        jobID,
		URL:       url,
		Status:    "pending",
		Strategy:  strategy,
		Selectors: storage.EncodeSelectors(selectors),
		CreatedAt: time.Now(),
	}
	if err := s.store.Create(newJob); err != nil {
		c.Writer.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(c.Writer, `<div class="alert alert-error">Failed to create job.</div>`)
		return
	}
	s.pool.Submit(newJob)

	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `
	<div class="alert alert-success">
		Job queued — <strong>%s</strong><br>
		<a href="/ui/jobs" style="color:var(--green)">View in Jobs →</a>
	</div>`, jobID)
}

func (s *Server) actionScrapeTest(c *gin.Context) {
	url := c.PostForm("url")
	strategy := c.PostForm("strategy")
	if strategy == "" {
		strategy = "title"
	}

	selectors := buildSelectors(c, "test_sel_key[]", "test_sel_value[]")

	px, err := s.proxyPool.GetNextProxy()
	if err != nil {
		c.Writer.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(c.Writer, `<div class="alert alert-error">No active proxies available.</div>`)
		return
	}

	strat := scraper.ResolveStrategy(strategy, selectors)
	result, outboundIP, err := s.scraper.Scrape(c.Request.Context(), url, px, strat)
	if err != nil {
		c.Writer.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(c.Writer, `<div class="alert alert-error">Scrape failed: %s</div>`, err.Error())
		return
	}

	data := result.Data
	if data == "" {
		data = "—"
	}

	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `
	<div class="alert alert-success" style="margin-bottom:12px">Scrape successful via <strong>%s</strong></div>
	<div class="card" style="font-size:13px">
		<div class="node-row"><span class="text-muted">Exit IP</span><strong>%s</strong></div>
		<div class="node-row"><span class="text-muted">Title</span><strong>%s</strong></div>
		<div class="node-row"><span class="text-muted">Data</span><span style="word-break:break-all">%s</span></div>
		<div class="node-row"><span class="text-muted">Scraped at</span><span>%s</span></div>
	</div>`,
		px, outboundIP, result.Title, data,
		result.ScrapedAt.Format("2006-01-02 15:04:05"))
}

func (s *Server) actionPoolRotate(c *gin.Context) {
	results := s.proxyPool.Rotate()
	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `<div class="alert alert-info"><strong>Circuit rotation complete</strong><br>`)
	for _, r := range results {
		icon := "✓"
		if strings.Contains(r, "failed") || strings.Contains(r, "unreachable") {
			icon = "✗"
		}
		fmt.Fprintf(c.Writer, `<div style="font-size:12px;margin-top:4px">%s %s</div>`, icon, r)
	}
	fmt.Fprintf(c.Writer, `</div>`)
}

func (s *Server) actionPoolReset(c *gin.Context) {
	s.proxyPool.Reset()
	// return fresh pool nodes partial
	s.partialPoolNodes(c)
}

func (s *Server) actionDeleteAllJobs(c *gin.Context) {
	count, _ := s.store.DeleteAll()
	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `<div class="text-muted" style="padding:16px">%d jobs deleted.</div>`, count)
}

func (s *Server) actionRetryJob(c *gin.Context) {
	id := c.Param("id")
	job, err := s.store.Retry(id)
	if err != nil {
		c.Writer.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(c.Writer, `<tr><td colspan="7" class="text-muted">Retry failed: %s</td></tr>`, err.Error())
		return
	}
	s.pool.Submit(job)
	badgeClass, badgeText := statusBadge(job.Status)
	shortURL := job.URL
	if len(shortURL) > 30 {
		shortURL = shortURL[:30] + "…"
	}
	c.Writer.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(c.Writer, `
	<tr>
		<td><code style="font-size:11px">%s</code></td>
		<td><span class="truncate">%s</span></td>
		<td><span class="badge badge-purple">%s</span></td>
		<td><span class="badge badge-%s">%s</span></td>
		<td class="text-muted">—</td>
		<td class="text-muted" style="font-size:12px">%s</td>
		<td>
			<button class="btn btn-danger btn-sm"
				hx-delete="/ui/actions/job/%s"
				hx-target="closest tr"
				hx-swap="outerHTML"
				hx-confirm="Delete?">🗑</button>
		</td>
	</tr>`,
		job.ID, shortURL, job.Strategy,
		badgeClass, badgeText,
		job.CreatedAt.Format("01/02 15:04"),
		job.ID)
}

func (s *Server) actionDeleteJob(c *gin.Context) {
	id := c.Param("id")
	s.store.Delete(id)
	// return empty — HTMX removes the row
	c.Writer.WriteHeader(http.StatusOK)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func statusBadge(status string) (string, string) {
	switch status {
	case "completed":
		return "green", "completed"
	case "failed":
		return "red", "failed"
	case "processing":
		return "blue", "processing"
	case "pending":
		return "yellow", "pending"
	default:
		return "purple", status
	}
}

func buildSelectors(c *gin.Context, keyField, valueField string) map[string]string {
	keys := c.PostFormArray(keyField)
	values := c.PostFormArray(valueField)
	result := map[string]string{}
	for i, k := range keys {
		if k != "" && i < len(values) && values[i] != "" {
			result[k] = values[i]
		}
	}
	return result
}

// keep json import used for potential future partials
var _ = json.Marshal
