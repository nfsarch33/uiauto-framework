package eval

// The listing-verify executor: the published-listing verification.
// After publish, a run opens the live listing, checks
// title, price, image count and stock against the record, and writes a
// PASS or FAIL row with a screenshot path into the nightly ledger — never
// a silent pass. The four fields come from the page's schema.org
// Product+Offer structured data (what a published listing actually emits)
// plus the gallery image count; every mismatch is NAMED in the row's
// errors, and a page that times out is a FAIL with the reason.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto"
)

// ListingSpec is the scenario's `verify` block: the publishgate record the
// live page is checked against. Stock is the normalised token
// (in_stock / out_of_stock).
type ListingSpec struct {
	Title      string `yaml:"title"`
	Price      string `yaml:"price"`
	ImageCount int    `yaml:"image_count"`
	Stock      string `yaml:"stock"`
}

// Listing is what the live page actually carries.
type Listing struct {
	Title      string `json:"title"`
	Price      string `json:"price"`
	ImageCount int    `json:"image_count"`
	Stock      string `json:"stock"`
}

var (
	jsonldRe  = regexp.MustCompile(`(?is)<script[^>]*type="application/ld\+json"[^>]*>(.*?)</script>`)
	galleryRe = regexp.MustCompile(`(?is)<div[^>]*class="[^"]*gallery[^"]*"[^>]*>(.*?)</div>`)
	imgRe     = regexp.MustCompile(`(?i)<img[\s>]`)
)

type jsonldProduct struct {
	Type   string `json:"@type"`
	Name   string `json:"name"`
	Offers *struct {
		Price        string `json:"price"`
		Availability string `json:"availability"`
	} `json:"offers"`
}

// ExtractListing pulls the four record fields out of a listing page's
// HTML: the first schema.org Product JSON-LD block supplies title, price
// and availability; the gallery div supplies the image count. Problems are
// returned as named errors — an unreadable record is never a guess.
func ExtractListing(html string) (Listing, []string) {
	var out Listing
	var errs []string
	for _, m := range jsonldRe.FindAllStringSubmatch(html, -1) {
		var p jsonldProduct
		if err := json.Unmarshal([]byte(m[1]), &p); err != nil {
			continue // a non-Product (or unparsable) block is skipped, not fatal
		}
		if p.Type != "Product" {
			continue
		}
		out.Title = strings.TrimSpace(p.Name)
		if p.Offers != nil {
			out.Price = strings.TrimSpace(p.Offers.Price)
			out.Stock = availabilityToken(p.Offers.Availability)
		}
		break
	}
	if out.Title == "" {
		errs = append(errs, "extraction: no schema.org Product JSON-LD on the page")
	}
	if g := galleryRe.FindStringSubmatch(html); g != nil {
		out.ImageCount = len(imgRe.FindAllStringIndex(g[1], -1))
	} else {
		errs = append(errs, "extraction: no gallery div on the page")
	}
	return out, errs
}

func availabilityToken(url string) string {
	switch {
	case strings.HasSuffix(url, "InStock"), strings.HasSuffix(url, "InStoreOnly"), strings.HasSuffix(url, "LimitedAvailability"):
		return "in_stock"
	case strings.HasSuffix(url, "OutOfStock"), strings.HasSuffix(url, "SoldOut"), strings.HasSuffix(url, "Discontinued"):
		return "out_of_stock"
	case url == "":
		return ""
	default:
		return strings.ToLower(strings.TrimPrefix(url, "https://schema.org/"))
	}
}

// CompareListing names every field that disagrees with the record.
func CompareListing(want ListingSpec, got Listing) []string {
	var ms []string
	if want.Title != got.Title {
		ms = append(ms, fmt.Sprintf("title: record %q page %q", want.Title, got.Title))
	}
	if want.Price != got.Price {
		ms = append(ms, fmt.Sprintf("price: record %q page %q", want.Price, got.Price))
	}
	if want.ImageCount != got.ImageCount {
		ms = append(ms, fmt.Sprintf("image_count: record %d page %d", want.ImageCount, got.ImageCount))
	}
	if want.Stock != got.Stock {
		ms = append(ms, fmt.Sprintf("stock: record %q page %q", want.Stock, got.Stock))
	}
	return ms
}

// ListingVerifyExecutor runs one verification against a live listing page.
// The screenshot is written through the shared CDP session when one is
// configured (ChromeDebug, the same remote-first pattern as laya-decide);
// a screenshot that cannot be taken is NAMED in the row, never dropped.
type ListingVerifyExecutor struct {
	ChromeDebug   string // optional shared CDP debug URL
	ScreenshotDir string // where shot files land (created on demand)
	Client        *http.Client
}

func (e *ListingVerifyExecutor) Name() string { return "listing-verify" }

// Healthy: the page under test is the dependency; there is no service to
// probe, so the executor is always healthy to schedule.
func (e *ListingVerifyExecutor) Healthy(ctx context.Context) error { return nil }

func (e *ListingVerifyExecutor) Run(ctx context.Context, sc Scenario, attempt int) RunRecord {
	rec := RunRecord{ScenarioID: sc.ID, Executor: e.Name(), Attempt: attempt}
	started := time.Now()
	defer func() { rec.DurationSec = time.Since(started).Seconds() }()

	if sc.Verify == nil {
		rec.Errors = []string{"listing-verify: the scenario carries no verify record"}
		rec.Evidence = verdictEvidence("FAIL", nil, nil, "unavailable: no record to check against")
		return rec
	}

	body, err := e.fetch(ctx, sc.URL)
	if err != nil {
		// rev373 finding 4: a failed fetch must not navigate again — the
		// screenshot is named unavailable and the runner moves on.
		rec.Errors = append(rec.Errors, fmt.Sprintf("listing page: %v", err))
		rec.Evidence = verdictEvidence("FAIL", sc.Verify, nil, "unavailable: page fetch failed")
		return rec
	}
	shot := e.screenshot(ctx, sc, attempt)
	got, extractErrs := ExtractListing(body)
	rec.Errors = append(rec.Errors, extractErrs...)
	rec.Errors = append(rec.Errors, CompareListing(*sc.Verify, got)...)
	rec.OK = len(rec.Errors) == 0
	verdict := "FAIL"
	if rec.OK {
		verdict = "PASS"
	}
	rec.Evidence = verdictEvidence(verdict, sc.Verify, &got, shot)
	return rec
}

func verdictEvidence(verdict string, want *ListingSpec, got *Listing, shot string) map[string]any {
	ev := map[string]any{
		"verdict":    verdict,
		"screenshot": shot,
	}
	if want != nil {
		ev["record"] = want
	}
	if got != nil {
		ev["page"] = got
	}
	return ev
}

func (e *ListingVerifyExecutor) fetch(ctx context.Context, url string) (string, error) {
	if url == "" {
		return "", errors.New("empty url")
	}
	cl := e.Client
	if cl == nil {
		cl = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// screenshot navigates a tab to the listing and captures the viewport. The
// path lands in the row on success; a named "unavailable: <reason>"
// otherwise — the verdict never rides on the screenshot, and a missing one
// is never silent.
func (e *ListingVerifyExecutor) screenshot(ctx context.Context, sc Scenario, attempt int) string {
	if err := ctx.Err(); err != nil {
		return "unavailable: context: " + err.Error()
	}
	agent, err := e.attach()
	if err != nil {
		return "unavailable: " + err.Error()
	}
	defer agent.Close()
	if err := agent.Navigate(sc.URL); err != nil {
		return "unavailable: navigate: " + err.Error()
	}
	if err := ctx.Err(); err != nil {
		return "unavailable: context: " + err.Error()
	}
	png, err := agent.CaptureScreenshot()
	if err != nil {
		return "unavailable: capture: " + err.Error()
	}
	dir := e.ScreenshotDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "uiauto-listing-verify")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "unavailable: mkdir: " + err.Error()
	}
	p := filepath.Join(dir, fmt.Sprintf("%s-attempt%d-%d.png", sc.ID, attempt, time.Now().Unix()))
	if err := os.WriteFile(p, png, 0o644); err != nil {
		return "unavailable: write: " + err.Error()
	}
	return p
}

func (e *ListingVerifyExecutor) attach() (*uiauto.BrowserAgent, error) {
	if e.ChromeDebug != "" {
		return uiauto.NewBrowserAgentWithRemote(e.ChromeDebug)
	}
	return uiauto.NewBrowserAgentWithChromePath(uiauto.ResolveChromePath(), true)
}
