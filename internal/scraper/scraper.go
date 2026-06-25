package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

type ScrapeResult struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Data      string    `json:"data"`
	ScrapedAt time.Time `json:"scraped_at"`
}

type Scraper interface {
	Scrape(ctx context.Context, url string, proxyURL string) (*ScrapeResult, string, error)
}

type DefaultScraper struct {
	userAgent string
}

func NewDefaultScraper() *DefaultScraper {
	return &DefaultScraper{
		userAgent: "GoScrapeBot/1.0 (+https://github.com/yourusername/goscrape-api)",
	}
}

func (ds *DefaultScraper) Scrape(ctx context.Context, targetURL string, proxyURL string) (*ScrapeResult, string, error) {
	c := colly.NewCollector(
		colly.UserAgent(ds.userAgent),
	)

	t := &http.Transport{
		DisableKeepAlives: true, // Forces HAProxy round-robin rotation per request
	}

	if proxyURL != "" {
		parsedProxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, "Proxy-Error", fmt.Errorf("failed to parse proxy URL: %w", err)
		}
		t.Proxy = http.ProxyURL(parsedProxy)
	}

	c.WithTransport(t)
	c.SetRequestTimeout(25 * time.Second)

	var result ScrapeResult
	result.URL = targetURL
	result.ScrapedAt = time.Now()

	_ = c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Delay:       2 * time.Second,
		RandomDelay: 1 * time.Second,
		Parallelism: 2,
	})

	c.OnHTML("title", func(e *colly.HTMLElement) {
		result.Title = strings.TrimSpace(e.Text)
	})

	c.OnResponse(func(r *colly.Response) {
		if result.Title == "" {
			result.Title = "Raw Document Snippet"
		}
	})

	c.OnHTML("meta[name=description]", func(e *colly.HTMLElement) {
		result.Data = e.Attr("content")
	})

	var scrapeErr error
	c.OnError(func(r *colly.Response, err error) {
		scrapeErr = fmt.Errorf("scraping failed: %w", err)
	})

	err := c.Visit(targetURL)
	if err != nil {
		return nil, "Tor-Circuit", err
	}

	if scrapeErr != nil {
		return nil, "Tor-Circuit", scrapeErr
	}

	if result.Title == "" {
		result.Title = "Unknown Domain Document"
	}

	// Returns a clean placeholder for the terminal status trace
	return &result, "Rotated-Tor-IP", nil
}
