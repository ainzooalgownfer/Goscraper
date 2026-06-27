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

func (ds *DefaultScraper) Scrape(ctx context.Context, targetURL string, proxyURL string, strategy ScrapeStrategy) (*ScrapeResult, string, error) {
	var outboundIP string = "Circuit-Establishing"

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
	c.SetRequestTimeout(60 * time.Second)

	// IP detection
	ipChecker := c.Clone()
	ipChecker.SetRequestTimeout(10 * time.Second)
	ipChecker.OnResponse(func(r *colly.Response) {
		fetchedIP := strings.TrimSpace(string(r.Body))
		if fetchedIP != "" && !strings.Contains(fetchedIP, "<") && len(fetchedIP) <= 45 {
			outboundIP = fetchedIP
		}
	})
	_ = ipChecker.Visit("http://api.ipify.org")

	var result ScrapeResult
	result.URL = targetURL
	result.ScrapedAt = time.Now()

	_ = c.Limit(&colly.LimitRule{
		DomainGlob:  "*",
		Delay:       2 * time.Second,
		RandomDelay: 1 * time.Second,
		Parallelism: 2,
	})

	// inject strategy
	if strategy != nil {
		strategy(c, &result)
	} else {
		TitleStrategy()(c, &result)
	}

	c.OnResponse(func(r *colly.Response) {
		if result.Title == "" {
			result.Title = "Raw Document Snippet"
		}
	})

	var scrapeErr error
	c.OnError(func(r *colly.Response, err error) {
		scrapeErr = fmt.Errorf("scraping failed: %w", err)
	})

	err := c.Visit(targetURL)
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
