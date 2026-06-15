// Package olxvn is the library behind the olxvn command line:
// the HTTP client, HTML scraping, and typed data models for OLX Vietnam
// (olx.vn), a top-10 classifieds platform in Vietnam.
//
// Listing detail pages embed JSON-LD Product/Offer schema for core fields.
// Category listing pages use ?page=N pagination. Listing URLs follow the
// pattern: https://www.olx.vn/item/{slug}/.
package olxvn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Host is the canonical site hostname.
const Host = "www.olx.vn"

// baseURL is the site root.
const baseURL = "https://www.olx.vn"

// DefaultUserAgent mimics a real browser.
const DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

// Config holds the tunable knobs for the HTTP client.
type Config struct {
	BaseURL   string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
	UserAgent string
}

// DefaultConfig returns sensible production defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   baseURL,
		Rate:      3 * time.Second,
		Retries:   3,
		Timeout:   30 * time.Second,
		UserAgent: DefaultUserAgent,
	}
}

// Client talks to the OLX Vietnam website over HTTP.
type Client struct {
	cfg  Config
	http *http.Client
	last time.Time
}

// NewClient returns a Client from DefaultConfig.
func NewClient() *Client { return NewClientWithConfig(DefaultConfig()) }

// NewClientWithConfig returns a Client built from cfg.
func NewClientWithConfig(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

// Get fetches rawURL and returns the body bytes, pacing and retrying on transient errors.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) ([]byte, bool, error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/json,*/*")
	req.Header.Set("Referer", baseURL+"/")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	return b, err != nil, err
}

func (c *Client) pace() {
	if c.cfg.Rate <= 0 {
		return
	}
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- wire JSON-LD types ---

type wireJSONLD struct {
	Type   string          `json:"@type"`
	Name   string          `json:"name"`
	Desc   string          `json:"description"`
	Offers wireJSONLDOffer `json:"offers"`
}

type wireJSONLDOffer struct {
	Type         string          `json:"@type"`
	Price        json.RawMessage `json:"price"`
	PriceCurr    string          `json:"priceCurrency"`
	Availability string          `json:"availability"`
}

// --- public types ---

// Listing is one OLX Vietnam classified ad.
type Listing struct {
	// Slug is the item slug from the URL: the canonical ID.
	Slug             string   `json:"slug"                    kit:"id" table:"slug"`
	Title            string   `json:"title"                            table:"title"`
	URL              string   `json:"url,omitempty"                    table:"url,url"`
	Price            float64  `json:"price,omitempty"                  table:"price"`
	PriceNegotiable  bool     `json:"price_negotiable,omitempty"       table:"negotiable"`
	Description      string   `json:"description,omitempty"            table:"-"`
	Category         string   `json:"category,omitempty"               table:"category"`
	City             string   `json:"city,omitempty"                   table:"city"`
	Condition        string   `json:"condition,omitempty"              table:"condition"`
	SellerName       string   `json:"seller_name,omitempty"            table:"seller"`
	PostedAt         string   `json:"posted_at,omitempty"              table:"posted_at"`
	FetchedAt        string   `json:"fetched_at,omitempty"             table:"fetched_at"`
}

// --- regexps ---

var jsonLdRE = regexp.MustCompile(`(?is)<script[^>]+type="application/ld\+json"[^>]*>([\s\S]*?)</script>`)

// listingLinkRE finds listing links in category pages.
// OLX VN listing URLs: /item/{slug}/
var listingLinkRE = regexp.MustCompile(`href="(?:https://www\.olx\.vn)?/item/([a-z0-9][a-z0-9-]+-[a-z0-9]+)(?:\.html|/)?"`)

// cityRE extracts city from listing location text.
var cityRE = regexp.MustCompile(`(?i)(?:Tp\. |TP\. |Thành phố |Tỉnh )?([^\n<,]{3,40})`)

// --- client methods ---

// GetListing fetches a listing detail page by its slug.
func (c *Client) GetListing(ctx context.Context, slug string) (*Listing, error) {
	base := c.cfg.BaseURL
	if base == "" {
		base = baseURL
	}
	slug = strings.Trim(slug, "/")
	pageURL := base + "/item/" + slug + "/"
	body, err := c.Get(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", slug, err)
	}
	l := parseListingPage(body, slug, base)
	if l == nil {
		return &Listing{Slug: slug, URL: pageURL, FetchedAt: time.Now().UTC().Format(time.RFC3339)}, nil
	}
	return l, nil
}

// ListListings fetches listing links from a category page.
func (c *Client) ListListings(ctx context.Context, categorySlug string, page, limit int) ([]*Listing, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	base := c.cfg.BaseURL
	if base == "" {
		base = baseURL
	}
	pageURL := base + "/" + categorySlug + "/?page=" + strconv.Itoa(page)
	body, err := c.Get(ctx, pageURL)
	if err != nil {
		return nil, fmt.Errorf("category %s: %w", categorySlug, err)
	}
	return parseListingListPage(body, limit, base), nil
}

// --- parsers ---

func parseListingPage(body []byte, slug, base string) *Listing {
	html := string(body)
	now := time.Now().UTC().Format(time.RFC3339)

	for _, m := range jsonLdRE.FindAllStringSubmatch(html, -1) {
		if len(m) < 2 {
			continue
		}
		var ld wireJSONLD
		if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
			continue
		}
		if ld.Type != "Product" {
			continue
		}
		price := parseJSONPrice(ld.Offers.Price)
		negotiable := strings.Contains(html, "Thương lượng") || strings.Contains(html, "thuong-luong")
		if price == 0 {
			negotiable = true
		}

		return &Listing{
			Slug:            slug,
			Title:           ld.Name,
			URL:             base + "/item/" + slug + "/",
			Price:           price,
			PriceNegotiable: negotiable,
			Description:     ld.Desc,
			FetchedAt:       now,
		}
	}
	return nil
}

func parseListingListPage(body []byte, limit int, base string) []*Listing {
	html := string(body)
	matches := listingLinkRE.FindAllStringSubmatch(html, -1)
	seen := map[string]bool{}
	var out []*Listing

	for _, m := range matches {
		if len(out) >= limit {
			break
		}
		if len(m) < 2 {
			continue
		}
		slug := m[1]
		if seen[slug] {
			continue
		}
		seen[slug] = true
		out = append(out, &Listing{
			Slug:      slug,
			URL:       base + "/item/" + slug + "/",
			FetchedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return out
}

// parseJSONPrice parses a JSON price field that may be a string or number.
func parseJSONPrice(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0
	}
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, ".", "")
	f, _ = strconv.ParseFloat(s, 64)
	return f
}

// extractSlug extracts the listing slug from an OLX VN URL.
func extractSlug(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if i := strings.Index(rawURL, "olx.vn/item/"); i >= 0 {
		rawURL = rawURL[i+len("olx.vn/item/"):]
	} else if i := strings.Index(rawURL, "/item/"); i >= 0 {
		rawURL = rawURL[i+len("/item/"):]
	}
	// Strip query, .html suffix, and trailing slashes.
	if i := strings.Index(rawURL, "?"); i >= 0 {
		rawURL = rawURL[:i]
	}
	rawURL = strings.TrimRight(rawURL, "/")
	rawURL = strings.TrimSuffix(rawURL, ".html")
	rawURL = strings.TrimRight(rawURL, "/")
	if rawURL == "" || strings.Contains(rawURL, "/") {
		return ""
	}
	return rawURL
}
