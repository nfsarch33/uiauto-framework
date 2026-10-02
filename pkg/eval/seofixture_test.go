package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// The fixture shop's structured data is an eval surface: the SEO lane
// records Lighthouse scores against these pages and the GEO slice
// cites the same catalogue. A malformed Offer would silently degrade
// both, so the shape is pinned here: every ListItem is a Product with
// a name and an Offer whose price parses, currency is AUD and
// availability is a schema.org URL.
func TestFixtureShopStructuredDataValidates(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "woo-admin", "products.html"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	block := regexp.MustCompile(`(?s)<script type="application/ld\+json">\s*(\{.*?\})\s*</script>`).
		FindStringSubmatch(string(raw))
	if block == nil {
		t.Fatal("products.html carries no application/ld+json block")
	}
	var doc struct {
		Type            string `json:"@type"`
		ItemListElement []struct {
			Type string `json:"@type"`
			Item struct {
				Type   string `json:"@type"`
				Name   string `json:"name"`
				Offers struct {
					Type          string `json:"@type"`
					Price         string `json:"price"` // schema.org prices are strings; trailing zeros preserved
					PriceCurrency string `json:"priceCurrency"`
					Availability  string `json:"availability"`
				} `json:"offers"`
			} `json:"item"`
		} `json:"itemListElement"`
	}
	if err := json.Unmarshal([]byte(block[1]), &doc); err != nil {
		t.Fatalf("structured data is not valid JSON: %v", err)
	}
	if doc.Type != "ItemList" || len(doc.ItemListElement) == 0 {
		t.Fatalf("want a non-empty ItemList, got %q with %d items", doc.Type, len(doc.ItemListElement))
	}
	for i, li := range doc.ItemListElement {
		if li.Item.Type != "Product" {
			t.Fatalf("item %d: want @type Product, got %q", i, li.Item.Type)
		}
		if li.Item.Name == "" {
			t.Fatalf("item %d: Product has no name", i)
		}
		o := li.Item.Offers
		if o.Type != "Offer" {
			t.Fatalf("item %d (%s): offers.@type = %q, want Offer", i, li.Item.Name, o.Type)
		}
		price, err := strconv.ParseFloat(o.Price, 64)
		if err != nil || price <= 0 {
			t.Fatalf("item %d (%s): Offer price %q must parse positive", i, li.Item.Name, o.Price)
		}
		if o.PriceCurrency != "AUD" {
			t.Fatalf("item %d (%s): priceCurrency %q, want AUD", i, li.Item.Name, o.PriceCurrency)
		}
		if !regexp.MustCompile(`^https://schema\.org/(InStock|OutOfStock|PreOrder)$`).MatchString(o.Availability) {
			t.Fatalf("item %d (%s): availability %q is not a schema.org stock value", i, li.Item.Name, o.Availability)
		}
	}
}

// llms.txt (llmstxt.org): an H1, a blockquote summary, and at least one
// link per listed page. The GEO slice depends on this shape.
func TestFixtureShopLlmsTxtShape(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "woo-admin", "llms.txt"))
	if err != nil {
		t.Fatalf("read llms.txt: %v", err)
	}
	s := string(raw)
	if !regexp.MustCompile(`(?m)^# .+\S`).MatchString(s) {
		t.Fatal("llms.txt needs an H1 title line")
	}
	if !regexp.MustCompile(`(?m)^> .+\S`).MatchString(s) {
		t.Fatal("llms.txt needs a blockquote summary line")
	}
	links := regexp.MustCompile(`(?m)^- \[.+\]\(http.+\)`).FindAllString(s, -1)
	if len(links) < 3 {
		t.Fatalf("llms.txt lists %d page links, want at least 3:\n%s", len(links), s)
	}
}
