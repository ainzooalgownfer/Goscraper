package scraper

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

type ScrapeResult struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Data      string    `json:"data"`
	RawHTML   string    `json:"raw_html,omitempty"`
	ScrapedAt time.Time `json:"scraped_at"`
}

type Scraper interface {
	Scrape(ctx context.Context, url string, proxyURL string, strategy ScrapeStrategy) (*ScrapeResult, string, error)
}

type DefaultScraper struct {
	userAgent string
}

func NewDefaultScraper() *DefaultScraper {
	return &DefaultScraper{
		userAgent: "GoScrapeBot/1.0 (+https://github.com/yourusername/goscrape-api)",
	}
}

// timeoutTiers — first attempt fast, retry with progressively longer timeouts
// a slow Tor circuit gets more patience without making every job wait that long by default
var timeoutTiers = []time.Duration{
	45 * time.Second,
	90 * time.Second,
	150 * time.Second,
}

func (ds *DefaultScraper) Scrape(ctx context.Context, targetURL string, proxyURL string, strategy ScrapeStrategy) (*ScrapeResult, string, error) {
	var lastErr error
	var lastIP string = "Circuit-Establishing"

	for attempt, timeout := range timeoutTiers {
		result, ip, err := ds.attemptScrape(ctx, targetURL, proxyURL, strategy, timeout)
		if err == nil {
			return result, ip, nil
		}

		lastErr = err
		if ip != "" {
			lastIP = ip
		}

		// only retry on timeout-class errors — don't retry on 403/429, that's wasted effort
		if !isTimeoutErr(err) {
			return nil, lastIP, err
		}

		if attempt < len(timeoutTiers)-1 {
			// brief pause before retry, lets a fresh circuit form if NEWNYM fired
			time.Sleep(2 * time.Second)
		}
	}

	return nil, lastIP, fmt.Errorf("scrape failed after %d attempts, last error: %w", len(timeoutTiers), lastErr)
}

func (ds *DefaultScraper) attemptScrape(ctx context.Context, targetURL string, proxyURL string, strategy ScrapeStrategy, timeout time.Duration) (*ScrapeResult, string, error) {
	c := colly.NewCollector(
		colly.UserAgent(ds.userAgent),
	)

	t := &http.Transport{
		DisableKeepAlives: true,
	}

	if proxyURL != "" {
		parsedProxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, "Proxy-Error", fmt.Errorf("failed to parse proxy URL: %w", err)
		}
		t.Proxy = http.ProxyURL(parsedProxy)
	}

	c.WithTransport(t)
	c.SetRequestTimeout(timeout)

	_ = c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Delay:       2 * time.Second,
		RandomDelay: 1 * time.Second,
		Parallelism: 2,
	})

	var result ScrapeResult
	result.URL = targetURL
	result.ScrapedAt = time.Now()

	if strategy != nil {
		strategy(c, &result)
	} else {
		TitleStrategy()(c, &result)
	}

	c.OnResponse(func(r *colly.Response) {
		if result.Title == "" {
			result.Title = "Raw Document Snippet"
		}
		result.RawHTML = string(r.Body)
	})

	var scrapeErr error
	c.OnError(func(r *colly.Response, err error) {
		scrapeErr = fmt.Errorf("scraping failed: %w", err)
	})

	// fire IP check concurrently — don't block the main scrape on it
	var outboundIP string = "Circuit-Establishing"
	var ipWg sync.WaitGroup
	ipWg.Add(1)
	go func() {
		defer ipWg.Done()
		ipChecker := c.Clone()
		ipChecker.SetRequestTimeout(8 * time.Second)
		ipChecker.OnResponse(func(r *colly.Response) {
			fetchedIP := strings.TrimSpace(string(r.Body))
			if fetchedIP != "" && !strings.Contains(fetchedIP, "<") && len(fetchedIP) <= 45 {
				outboundIP = fetchedIP
			}
		})
		_ = ipChecker.Visit("http://api.ipify.org")
	}()

	err := c.Visit(targetURL)

	// give the IP check a moment to finish, but never wait more than 8s past the main scrape
	waitDone := make(chan struct{})
	go func() {
		ipWg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(8 * time.Second):
	}

	if err != nil {
		return nil, outboundIP, err
	}
	if scrapeErr != nil {
		return nil, outboundIP, scrapeErr
	}
	if result.Title == "" {
		result.Title = "Unknown Domain Document"
	}

	return &result, outboundIP, nil
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "Client.Timeout") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "context deadline")
}
