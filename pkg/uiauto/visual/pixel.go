package visual

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"math"
	"time"
)

// PixelDiffer performs pixel-level image comparison.
type PixelDiffer struct {
	tolerance uint8 // per-channel difference threshold before counting as diff
	logger    *slog.Logger
}

// PixelDifferOption configures a PixelDiffer.
type PixelDifferOption func(*PixelDiffer)

// WithTolerance sets the per-channel pixel diff tolerance (0-255).
func WithTolerance(t uint8) PixelDifferOption {
	return func(d *PixelDiffer) { d.tolerance = t }
}

// WithPixelLogger sets a structured logger.
func WithPixelLogger(l *slog.Logger) PixelDifferOption {
	return func(d *PixelDiffer) { d.logger = l }
}

// NewPixelDiffer creates a pixel comparator with optional tolerance.
func NewPixelDiffer(opts ...PixelDifferOption) *PixelDiffer {
	d := &PixelDiffer{
		tolerance: 10,
		logger:    slog.Default(),
	}
	for _, o := range opts {
		o(d)
	}
	return d
}

// Compare decodes two PNGs and returns pixel-level diff metrics.
func (d *PixelDiffer) Compare(ctx context.Context, baseline, current []byte) (*PixelDiffResult, error) {
	start := time.Now()

	imgA, err := png.Decode(bytes.NewReader(baseline))
	if err != nil {
		return nil, fmt.Errorf("pixel: decode baseline: %w", err)
	}
	imgB, err := png.Decode(bytes.NewReader(current))
	if err != nil {
		return nil, fmt.Errorf("pixel: decode current: %w", err)
	}

	boundsA, boundsB := imgA.Bounds(), imgB.Bounds()
	if boundsA.Dx() != boundsB.Dx() || boundsA.Dy() != boundsB.Dy() {
		return nil, fmt.Errorf("pixel: size mismatch: %dx%d vs %dx%d",
			boundsA.Dx(), boundsA.Dy(), boundsB.Dx(), boundsB.Dy())
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	width, height := boundsA.Dx(), boundsA.Dy()
	total := width * height
	diffCount := 0
	minX, minY := width, height
	maxX, maxY := 0, 0

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if d.pixelDiff(imgA.At(boundsA.Min.X+x, boundsA.Min.Y+y),
				imgB.At(boundsB.Min.X+x, boundsB.Min.Y+y)) {
				diffCount++
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}

	pct := 0.0
	if total > 0 {
		pct = float64(diffCount) / float64(total) * 100.0
	}

	var region BBox
	if diffCount > 0 {
		region = BBox{X: minX, Y: minY, W: maxX - minX + 1, H: maxY - minY + 1}
	}

	result := &PixelDiffResult{
		TotalPixels:    total,
		DiffPixels:     diffCount,
		DiffPercentage: pct,
		Level:          classifyDiff(pct),
		MaxRegionDiff:  region,
		Duration:       time.Since(start),
	}

	d.logger.Info("pixel diff",
		slog.Float64("diff_pct", pct),
		slog.String("level", result.Level.String()),
		slog.Duration("duration", result.Duration),
	)

	return result, nil
}

func (d *PixelDiffer) pixelDiff(a, b color.Color) bool {
	ra, ga, ba, _ := a.RGBA()
	rb, gb, bb, _ := b.RGBA()
	tol := uint32(d.tolerance) * 257 // scale 8-bit to 16-bit range
	return absDiff(ra, rb) > tol || absDiff(ga, gb) > tol || absDiff(ba, bb) > tol
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func classifyDiff(pct float64) DiffLevel {
	switch {
	case pct <= 0:
		return DiffNone
	case pct < 1:
		return DiffMinor
	case pct < 5:
		return DiffModerate
	case pct < 20:
		return DiffMajor
	default:
		return DiffCritical
	}
}

// GenerateTestPNG creates a solid-color PNG for testing.
func GenerateTestPNG(width, height int, c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}

// GenerateGradientPNG creates a gradient PNG for testing.
func GenerateGradientPNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r := uint8(float64(x) / float64(width) * 255)
			g := uint8(float64(y) / float64(height) * 255)
			img.Set(x, y, color.RGBA{R: r, G: g, B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// GeneratePartialDiffPNG modifies a fraction of a gradient PNG to create a controlled diff.
func GeneratePartialDiffPNG(width, height int, diffFraction float64) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	diffCols := int(math.Round(float64(width) * diffFraction))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < diffCols {
				img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
			} else {
				r := uint8(float64(x) / float64(width) * 255)
				g := uint8(float64(y) / float64(height) * 255)
				img.Set(x, y, color.RGBA{R: r, G: g, B: 128, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
