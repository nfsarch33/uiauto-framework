// Package carousel renders design-system HTML templates to
// fixed-size JPEG slides in headless Chrome (the social-content
// engine's carousel lane). The pipeline is deterministic and local:
// tokens from a design-system tokens.json are injected as CSS custom
// properties, the template renders one slide, a dedicated headless
// Chrome (never the shared CDP session) captures the viewport at the
// slide's exact size, and the JPEG encode happens in Go so the output
// never depends on Chrome's encoder.
package carousel

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"text/template"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto"
)

// Slide is the content model every template receives. Templates stay
// structural (layout + classes); all copy comes from here.
type Slide struct {
	Title  string
	Kicker string
	Body   string
}

// Tokens is the design-system token map (tokens.json decoded).
type Tokens map[string]any

// RenderHTML executes one slide template. The template's data has two
// fields: .Tokens (the design-system map) and .Slide (the content).
// Token lookup failures are template errors, not blank output.
func RenderHTML(tmplSrc string, tokens Tokens, slide Slide) (string, error) {
	tmpl, err := template.New("slide").Option("missingkey=error").Parse(tmplSrc)
	if err != nil {
		return "", fmt.Errorf("carousel: parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, map[string]any{"Tokens": tokens, "Slide": slide}); err != nil {
		return "", fmt.Errorf("carousel: execute template: %w", err)
	}
	return buf.String(), nil
}

// Capture renders one HTML document to a PNG at exactly width x height
// in a dedicated headless Chrome. It never attaches a shared debug
// session: slide rendering is a batch job, not lane automation.
func Capture(ctx context.Context, html string, width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("carousel: slide size must be positive, got %dx%d", width, height)
	}
	// The agent APIs take no ctx, so a cancelled caller must not leave a
	// render running: the work runs under the ctx and the caller sees the
	// deadline (the same shape as the fusion lane's capture).
	type result struct {
		png []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		agent, err := uiauto.NewBrowserAgentWithChromePath(uiauto.ResolveChromePath(), true)
		if err != nil {
			done <- result{nil, err}
			return
		}
		defer agent.Close()
		if err := ctx.Err(); err != nil {
			done <- result{nil, err}
			return
		}
		if err := agent.SetViewport(width, height); err != nil {
			done <- result{nil, fmt.Errorf("carousel: viewport %dx%d: %w", width, height, err)}
			return
		}
		// The slide rides a data: URL: no server, no temp file, no
		// fixture port — the document IS the input.
		url := "data:text/html;charset=utf-8;base64," + base64.StdEncoding.EncodeToString([]byte(html))
		if err := agent.Navigate(url); err != nil {
			done <- result{nil, fmt.Errorf("carousel: navigate: %w", err)}
			return
		}
		if err := ctx.Err(); err != nil {
			done <- result{nil, err}
			return
		}
		png, err := agent.CaptureScreenshot()
		done <- result{png, err}
	}()
	select {
	case r := <-done:
		return r.png, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ToJPEG encodes a captured PNG as JPEG at the given quality (1-100).
// Encoding in Go keeps the bytes stable across Chrome versions.
func ToJPEG(png []byte, quality int) ([]byte, error) {
	if quality < 1 || quality > 100 {
		return nil, fmt.Errorf("carousel: jpeg quality must be 1-100, got %d", quality)
	}
	img, format, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		return nil, fmt.Errorf("carousel: decode capture: %w", err)
	}
	if format != "png" {
		return nil, fmt.Errorf("carousel: capture is %s, want png", format)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("carousel: encode jpeg: %w", err)
	}
	return out.Bytes(), nil
}

// PixelDiff returns the mean absolute per-pixel difference (0..1) of
// two decodable images of the SAME dimensions. Golden-image testing for
// JPEG needs a tolerance, not byte equality: headless Chrome's
// anti-aliasing differs slightly across binaries.
func PixelDiff(a, b []byte) (float64, error) {
	ia, _, err := image.Decode(bytes.NewReader(a))
	if err != nil {
		return 0, fmt.Errorf("carousel: decode a: %w", err)
	}
	ib, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return 0, fmt.Errorf("carousel: decode b: %w", err)
	}
	ba, bb := ia.Bounds(), ib.Bounds()
	if ba.Dx() != bb.Dx() || ba.Dy() != bb.Dy() {
		return 1, fmt.Errorf("carousel: dimensions differ: %dx%d vs %dx%d", ba.Dx(), ba.Dy(), bb.Dx(), bb.Dy())
	}
	var total uint64
	for y := ba.Min.Y; y < ba.Max.Y; y++ {
		for x := ba.Min.X; x < ba.Max.X; x++ {
			ra, ga, bla := colorAt(ia, x, y)
			rb, gb, blb := colorAt(ib, x, y)
			total += uint64(abs(int(ra)-int(rb)) + abs(int(ga)-int(gb)) + abs(int(bla)-int(blb)))
		}
	}
	return float64(total) / (float64(ba.Dx()*ba.Dy()) * 3 * 255), nil
}

func colorAt(img image.Image, x, y int) (uint8, uint8, uint8) {
	r, g, b, _ := img.At(x, y).RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
