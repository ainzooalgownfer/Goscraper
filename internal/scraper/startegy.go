package scraper

import (
	"strings"

	"github.com/gocolly/colly/v2"
)

type ScrapeStrategy func(c *colly.Collector, result *ScrapeResult)

// TitleStrategy — default, grabs title and meta description
func TitleStrategy() ScrapeStrategy {
	return func(c *colly.Collector, result *ScrapeResult) {
		c.OnHTML("title", func(e *colly.HTMLElement) {
			result.Title = strings.TrimSpace(e.Text)
		})
		c.OnHTML("meta[name=description]", func(e *colly.HTMLElement) {
			result.Data = e.Attr("content")
		})
	}
}

// NewsStrategy — grabs headline and article body
func NewsStrategy() ScrapeStrategy {
	return func(c *colly.Collector, result *ScrapeResult) {
		c.OnHTML("title", func(e *colly.HTMLElement) {
			result.Title = strings.TrimSpace(e.Text)
		})
		c.OnHTML("h1", func(e *colly.HTMLElement) {
			if result.Data == "" {
				result.Data = strings.TrimSpace(e.Text)
			}
		})
		c.OnHTML("article p", func(e *colly.HTMLElement) {
			result.Data += strings.TrimSpace(e.Text) + " "
		})
	}
}

// EcommerceStrategy — grabs product name, price, availability
func EcommerceStrategy() ScrapeStrategy {
	return func(c *colly.Collector, result *ScrapeResult) {
		c.OnHTML("title", func(e *colly.HTMLElement) {
			result.Title = strings.TrimSpace(e.Text)
		})
		c.OnHTML("[itemprop='name']", func(e *colly.HTMLElement) {
			if result.Data == "" {
				result.Data = "name:" + strings.TrimSpace(e.Text)
			}
		})
		c.OnHTML("[itemprop='price']", func(e *colly.HTMLElement) {
			result.Data += " price:" + e.Attr("content")
		})
		c.OnHTML("[itemprop='availability']", func(e *colly.HTMLElement) {
			result.Data += " availability:" + e.Attr("content")
		})
	}
}

// CustomStrategy — caller passes CSS selectors at request time
func CustomStrategy(selectors map[string]string) ScrapeStrategy {
	return func(c *colly.Collector, result *ScrapeResult) {
		c.OnHTML("title", func(e *colly.HTMLElement) {
			result.Title = strings.TrimSpace(e.Text)
		})
		for key, selector := range selectors {
			k := key
			sel := selector
			c.OnHTML(sel, func(e *colly.HTMLElement) {
				result.Data += k + ":" + strings.TrimSpace(e.Text) + " | "
			})
		}
	}
}

// ResolveStrategy maps a strategy name from the API request to the correct func
func ResolveStrategy(name string, selectors map[string]string) ScrapeStrategy {
	switch name {
	case "news":
		return NewsStrategy()
	case "ecommerce":
		return EcommerceStrategy()
	case "custom":
		return CustomStrategy(selectors)
	default:
		return TitleStrategy()
	}
}