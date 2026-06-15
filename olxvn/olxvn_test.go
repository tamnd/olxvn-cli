package olxvn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(srv *httptest.Server) *Client {
	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 0
	return NewClientWithConfig(cfg)
}

func sampleListingPageHTML(slug, title string, price float64, negotiable bool) string {
	ld, _ := json.Marshal(map[string]any{
		"@type":       "Product",
		"name":        title,
		"description": "Hàng cần bán gấp",
		"offers": map[string]any{
			"@type": "Offer",
			"price": price,
		},
	})
	negText := ""
	if negotiable {
		negText = `<span>Thương lượng</span>`
	}
	return `<!DOCTYPE html><html><head>
<script type="application/ld+json">` + string(ld) + `</script>
</head><body>` + negText + `<h1>` + title + `</h1></body></html>`
}

func sampleCategoryPageHTML(n int) string {
	html := `<!DOCTYPE html><html><body><ul>`
	for i := 0; i < n; i++ {
		slug := "xe-may-yamaha-" + string(rune('a'+i)) + "bc123"
		html += `<li><a href="/item/` + slug + `/">` + slug + `</a></li>`
	}
	html += `</ul></body></html>`
	return html
}

func TestGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.BaseURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 5
	c := NewClientWithConfig(cfg)

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestGetListing(t *testing.T) {
	html := sampleListingPageHTML("xe-may-yamaha-abc123", "Xe máy Yamaha Exciter 2020", 25000000, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	l, err := c.GetListing(context.Background(), "xe-may-yamaha-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if l.Slug != "xe-may-yamaha-abc123" {
		t.Errorf("Slug = %q", l.Slug)
	}
	if l.Title != "Xe máy Yamaha Exciter 2020" {
		t.Errorf("Title = %q", l.Title)
	}
	if l.Price != 25000000 {
		t.Errorf("Price = %v, want 25000000", l.Price)
	}
}

func TestGetListingNegotiable(t *testing.T) {
	html := sampleListingPageHTML("some-laptop-def456", "Laptop cũ", 0, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	l, err := c.GetListing(context.Background(), "some-laptop-def456")
	if err != nil {
		t.Fatal(err)
	}
	if !l.PriceNegotiable {
		t.Error("PriceNegotiable = false, want true")
	}
}

func TestListListings(t *testing.T) {
	html := sampleCategoryPageHTML(4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	listings, err := c.ListListings(context.Background(), "xe-may-xe-dap", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 4 {
		t.Fatalf("got %d listings, want 4", len(listings))
	}
}

func TestListListingsLimit(t *testing.T) {
	html := sampleCategoryPageHTML(8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	c := newTestClient(srv)
	listings, err := c.ListListings(context.Background(), "xe-may-xe-dap", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 3 {
		t.Errorf("got %d listings, want 3 (limit)", len(listings))
	}
}

func TestExtractSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://www.olx.vn/item/xe-may-yamaha-abc123/", "xe-may-yamaha-abc123"},
		{"https://www.olx.vn/item/xe-may-yamaha-abc123.html", "xe-may-yamaha-abc123"},
		{"xe-may-yamaha-abc123", "xe-may-yamaha-abc123"},
		{"", ""},
	}
	for _, tc := range cases {
		got := extractSlug(tc.in)
		if got != tc.want {
			t.Errorf("extractSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseListingPageNoJSONLD(t *testing.T) {
	html := `<html><body><h1>No JSON-LD</h1></body></html>`
	l := parseListingPage([]byte(html), "some-slug", baseURL)
	if l != nil {
		t.Errorf("expected nil for page without JSON-LD, got %+v", l)
	}
}
