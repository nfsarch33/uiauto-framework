package carousel

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// skipWithoutBrowser is the repo's CI convention (cmd/ui-agent
// laya_decide_test.go): hosted runners have a Chrome binary on PATH but
// its chromedp websocket launch times out, so browser-driven tests run
// on dev hosts and skip under CI=1 / -short.
func skipWithoutBrowser(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") != "" || testing.Short() {
		t.Skip("browser-driven test: skipped on CI/-short (runner Chrome websocket launch times out)")
	}
}

var updateGoldens = flag.Bool("update", false, "regenerate golden slide images")

// requireChrome mirrors pkg/uiauto's test gate: chromedp defers the
// browser launch to the first action, so a missing binary must SKIP
// here, not hard-fail at Navigate.
func requireChrome(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "headless-shell"} {
		if _, err := exec.LookPath(bin); err == nil {
			return
		}
	}
	t.Skip("no Chrome-family binary on PATH; skipping browser-driven test")
}

const goldenTemplate = `<!doctype html>
<html><head><meta charset="utf-8"><style>
  * { margin: 0; box-sizing: border-box; }
  html, body { width: 1080px; height: 1350px; }
  body { background: {{.Tokens.bg}}; display: flex; align-items: center;
         justify-content: center; }
  .card { width: 880px; border-radius: 24px; padding: 64px;
          background: {{.Tokens.ink}}; }
  .kicker { color: {{.Tokens.amber}}; font: 700 40px sans-serif;
            letter-spacing: 4px; text-transform: uppercase; }
  .title  { color: {{.Tokens.bg}}; font: 700 96px sans-serif; }
  .rule   { height: 6px; width: 240px; background: {{.Tokens.amber}};
            border-radius: 3px; }
</style></head>
<body><div class="card">
  <p class="kicker">{{.Slide.Kicker}}</p>
  <div class="rule"></div>
  <h1 class="title">{{.Slide.Title}}</h1>
</div></body></html>`

var goldenTokens = Tokens{"ink": "#101828", "bg": "#ffffff", "amber": "#f59e0b"}

// Template execution: tokens and slide copy land in the document, and a
// missing token is an ERROR, not blank output (silent blanks would ship
// unstyled slides).
func TestRenderHTMLInjectsTokensAndSlide(t *testing.T) {
	html, err := RenderHTML(goldenTemplate, goldenTokens, Slide{Kicker: "Check", Title: "Grounded"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{"#101828", "#f59e0b", "Grounded", "Check"} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered doc missing %q:\n%s", want, html)
		}
	}

	if _, err := RenderHTML("<p>{{.Tokens.no_such_token}}</p>", goldenTokens, Slide{}); err == nil {
		t.Fatal("missing token must be a template error, not blank output")
	}
	if _, err := RenderHTML("{{.Slide.Title", goldenTokens, Slide{}); err == nil {
		t.Fatal("malformed template must be an error")
	}
}

// JPEG encoding: exact dimensions preserved, quality bounds enforced,
// and lower quality yields smaller bytes (the knob is real).
func TestToJPEGPreservesDimensionsAndQualityIsReal(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1080, 1350))
	for y := 0; y < 1350; y += 7 {
		for x := 0; x < 1080; x += 11 {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}

	if _, err := ToJPEG(raw.Bytes(), 0); err == nil {
		t.Fatal("quality 0 must be rejected")
	}
	if _, err := ToJPEG(raw.Bytes(), 101); err == nil {
		t.Fatal("quality 101 must be rejected")
	}
	if _, err := ToJPEG([]byte("not an image"), 90); err == nil {
		t.Fatal("garbage input must be rejected")
	}

	hi, err := ToJPEG(raw.Bytes(), 95)
	if err != nil {
		t.Fatal(err)
	}
	lo, err := ToJPEG(raw.Bytes(), 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(lo) >= len(hi) {
		t.Fatalf("quality knob is not real: q40=%d bytes >= q95=%d bytes", len(lo), len(hi))
	}
	decoded, err := jpeg.Decode(bytes.NewReader(hi))
	if err != nil {
		t.Fatalf("output not decodable JPEG: %v", err)
	}
	if got := decoded.Bounds(); got.Dx() != 1080 || got.Dy() != 1350 {
		t.Fatalf("dimensions changed in encode: %dx%d", got.Dx(), got.Dy())
	}
}

// PixelDiff: identical images are 0, a changed image is materially
// above 0, mismatched dimensions are an error.
func TestPixelDiffBasics(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < 8; i++ {
		img.Set(i, i, color.RGBA{R: 255, A: 255})
	}
	var a, b bytes.Buffer
	if err := png.Encode(&a, img); err != nil {
		t.Fatal(err)
	}
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if d, err := PixelDiff(a.Bytes(), a.Bytes()); err != nil || d != 0 {
		t.Fatalf("identical images: diff=%v err=%v", d, err)
	}
	d, err := PixelDiff(a.Bytes(), b.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if d <= 0 {
		t.Fatalf("changed image must diff above 0, got %v", d)
	}
	other := image.NewRGBA(image.Rect(0, 0, 4, 4))
	var c bytes.Buffer
	if err := png.Encode(&c, other); err != nil {
		t.Fatal(err)
	}
	if _, err := PixelDiff(a.Bytes(), c.Bytes()); err == nil {
		t.Fatal("dimension mismatch must be an error")
	}
}

// Golden image: the cylrl-token template renders through a REAL
// headless Chrome at exactly 1080x1350, encodes to JPEG, and matches
// the committed golden within the AA tolerance. -update regenerates.
// Mutant this kills: viewport params dropped or swapped (e.g. the
// classic width/height transposition) — the golden diff explodes or the
// dimension assert fails.
func TestGoldenSlide(t *testing.T) {
	requireChrome(t)
	skipWithoutBrowser(t)

	html, err := RenderHTML(goldenTemplate, goldenTokens, Slide{Kicker: "Case study", Title: "Approvals, not guesswork"})
	if err != nil {
		t.Fatal(err)
	}
	pngShot, err := Capture(context.Background(), html, 1080, 1350)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	jpg, err := ToJPEG(pngShot, 90)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := image.DecodeConfig(bytes.NewReader(jpg)); err != nil {
		t.Fatalf("jpeg decode config: %v", err)
	} else if got.Width != 1080 || got.Height != 1350 {
		t.Fatalf("slide is %dx%d, want exactly 1080x1350", got.Width, got.Height)
	}

	goldenPath := "testdata/golden_1080x1350.jpg"
	if *updateGoldens {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, jpg, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("golden updated: %s (%d bytes)", goldenPath, len(jpg))
		return
	}
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden missing (run go test ./pkg/carousel -update): %v", err)
	}
	diff, err := PixelDiff(jpg, golden)
	if err != nil {
		t.Fatal(err)
	}
	if diff > 0.01 {
		t.Fatalf("golden diff %v exceeds 0.01 tolerance — rendering changed (regenerate with -update only after reviewing WHY)", diff)
	}
}

// A cancelled ctx must surface as the ctx error — cmd.Context()
// cancellation is never silently ignored (the agent APIs take no ctx,
// so the deadline is honoured at the select and at the re-checks).
// Mutant this kills: the select-on-ctx.Done deleted — the cancelled
// call waits out a full agent launch instead of returning.
func TestCaptureHonoursCancelledCtx(t *testing.T) {
	requireChrome(t)
	skipWithoutBrowser(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Capture(ctx, "<p>x</p>", 10, 10)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled (a cancelled caller must get ITS deadline back, not a launch error or success)", err)
	}
}
