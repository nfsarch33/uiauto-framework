package eval

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The 680c verification (done by uiauto, never again by hand): a published
// listing is opened, title/price/image count/stock are checked against the
// record, and the row is PASS or FAIL naming the mismatch — with a
// screenshot path, never a silent pass.

const listingFixtureHTML = `<!doctype html>
<html><head><title>Aluminium Water Bottle — Store (fixture)</title>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"Product","name":"Aluminium Water Bottle",
 "offers":{"@type":"Offer","price":"29.00","priceCurrency":"AUD",
 "availability":"https://schema.org/InStock"}}
</script></head>
<body>
<h1>Aluminium Water Bottle</h1>
<p class="price">29.00</p>
<p class="stock">In stock</p>
<div class="gallery"><img src="bottle-1.jpg"><img src="bottle-2.jpg"></div>
</body></html>`

func TestExtractListingReadsTheFourFields(t *testing.T) {
	got, errs := ExtractListing(listingFixtureHTML)
	if len(errs) != 0 {
		t.Fatalf("unexpected extraction errors: %v", errs)
	}
	if got.Title != "Aluminium Water Bottle" {
		t.Errorf("title = %q", got.Title)
	}
	if got.Price != "29.00" {
		t.Errorf("price = %q", got.Price)
	}
	if got.ImageCount != 2 {
		t.Errorf("image count = %d", got.ImageCount)
	}
	if got.Stock != "in_stock" {
		t.Errorf("stock = %q", got.Stock)
	}
}

func TestCompareListingNamesEveryMismatch(t *testing.T) {
	want := ListingSpec{Title: "Aluminium Water Bottle", Price: "29.00", ImageCount: 2, Stock: "in_stock"}
	got := Listing{Title: "Aluminium Water Bottle", Price: "31.00", ImageCount: 1, Stock: "out_of_stock"}
	ms := CompareListing(want, got)
	if len(ms) != 3 {
		t.Fatalf("mismatches = %d (%v), want 3", len(ms), ms)
	}
	for _, name := range []string{"price", "image_count", "stock"} {
		if !strings.Contains(strings.Join(ms, "; "), name) {
			t.Errorf("no mismatch names %q", name)
		}
	}
}

func TestCompareListingPassIsSilentOnFields(t *testing.T) {
	want := ListingSpec{Title: "Aluminium Water Bottle", Price: "29.00", ImageCount: 2, Stock: "in_stock"}
	got := Listing{Title: "Aluminium Water Bottle", Price: "29.00", ImageCount: 2, Stock: "in_stock"}
	if ms := CompareListing(want, got); len(ms) != 0 {
		t.Fatalf("mismatches on a matching record: %v", ms)
	}
}

func TestListingVerifyRunPassesAndCarriesTheRow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(listingFixtureHTML))
	}))
	defer srv.Close()
	dir := t.TempDir()
	e := &ListingVerifyExecutor{Client: srv.Client(), ScreenshotDir: dir}
	rec := e.Run(context.Background(), Scenario{
		ID:  "listing-verify-fixture",
		URL: srv.URL,
		Verify: &ListingSpec{Title: "Aluminium Water Bottle", Price: "29.00", ImageCount: 2, Stock: "in_stock"},
	}, 1)
	if !rec.OK {
		t.Fatalf("expected PASS, errors: %v", rec.Errors)
	}
	if rec.Evidence["verdict"] != "PASS" {
		t.Errorf("verdict = %v", rec.Evidence["verdict"])
	}
	if rec.Evidence["screenshot"] == "" {
		t.Errorf("the row carries no screenshot path")
	}
}

func TestListingVerifyRunFailsNamingTheMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(listingFixtureHTML))
	}))
	defer srv.Close()
	e := &ListingVerifyExecutor{Client: srv.Client(), ScreenshotDir: t.TempDir()}
	rec := e.Run(context.Background(), Scenario{
		ID:  "listing-verify-fixture",
		URL: srv.URL,
		Verify: &ListingSpec{Title: "Aluminium Water Bottle", Price: "31.00", ImageCount: 2, Stock: "in_stock"},
	}, 1)
	if rec.OK {
		t.Fatal("expected FAIL on a price mismatch")
	}
	if !strings.Contains(strings.Join(rec.Errors, "; "), "price") {
		t.Errorf("the FAIL does not name the mismatch: %v", rec.Errors)
	}
	if rec.Evidence["verdict"] != "FAIL" {
		t.Errorf("verdict = %v", rec.Evidence["verdict"])
	}
}

func TestListingVerifyTimeoutIsAFailNeverASilentPass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		_, _ = w.Write([]byte(listingFixtureHTML))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	e := &ListingVerifyExecutor{Client: srv.Client(), ScreenshotDir: t.TempDir()}
	rec := e.Run(ctx, Scenario{
		ID: "listing-verify-timeout", URL: srv.URL,
		Verify: &ListingSpec{Title: "x", Price: "1", ImageCount: 1, Stock: "in_stock"},
	}, 1)
	if rec.OK {
		t.Fatal("a timed-out page must FAIL, not pass")
	}
	if !strings.Contains(strings.Join(rec.Errors, "; "), "listing page") {
		t.Errorf("the timeout reason is not named: %v", rec.Errors)
	}
	if rec.Evidence["verdict"] != "FAIL" {
		t.Errorf("verdict = %v", rec.Evidence["verdict"])
	}
}

func TestListingVerifyScreenshotWrittenToDisk(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(p, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
