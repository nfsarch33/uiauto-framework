package omniparser

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
)

// AnnotateScreenshot overlays bounding boxes and labels on a screenshot PNG.
// Returns an annotated image. Each detected element gets a coloured rectangle
// and an ID label at the top-left corner of its bounding box.
func AnnotateScreenshot(screenshot []byte, result *ParseResult) (image.Image, error) {
	r := pngReader(screenshot)
	src, err := png.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("annotate: decode png: %w", err)
	}

	bounds := src.Bounds()
	canvas := image.NewRGBA(bounds)
	draw.Draw(canvas, bounds, src, image.Point{}, draw.Src)

	for _, elem := range result.Elements {
		bb := elem.BoundingBox
		c := pickColor(elem.ID)

		drawRect(canvas, bb.X, bb.Y, bb.X+bb.Width, bb.Y+bb.Height, c, 2)
		drawLabel(canvas, bb.X, bb.Y-2, fmt.Sprintf("[%d] %s", elem.ID, truncate(elem.Text, 30)), c)
	}

	return canvas, nil
}

// SaveAnnotatedPNG writes the annotated image to a file.
func SaveAnnotatedPNG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("annotate: create file: %w", err)
	}
	defer f.Close()
	return png.Encode(f, img)
}

func pngReader(data []byte) io.Reader {
	return &byteReader{data: data, pos: 0}
}

type byteReader struct {
	data []byte
	pos  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func pickColor(id int) color.RGBA {
	colors := []color.RGBA{
		{124, 58, 237, 200},  // purple
		{6, 182, 212, 200},   // cyan
		{245, 158, 11, 200},  // amber
		{167, 139, 250, 200}, // light purple
		{16, 185, 129, 200},  // emerald
		{239, 68, 68, 200},   // red
		{59, 130, 246, 200},  // blue
	}
	return colors[id%len(colors)]
}

func drawRect(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA, thickness int) {
	bounds := img.Bounds()
	for t := 0; t < thickness; t++ {
		for x := x1 - t; x <= x2+t; x++ {
			if x >= bounds.Min.X && x < bounds.Max.X {
				if y1-t >= bounds.Min.Y && y1-t < bounds.Max.Y {
					img.SetRGBA(x, y1-t, c)
				}
				if y2+t >= bounds.Min.Y && y2+t < bounds.Max.Y {
					img.SetRGBA(x, y2+t, c)
				}
			}
		}
		for y := y1 - t; y <= y2+t; y++ {
			if y >= bounds.Min.Y && y < bounds.Max.Y {
				if x1-t >= bounds.Min.X && x1-t < bounds.Max.X {
					img.SetRGBA(x1-t, y, c)
				}
				if x2+t >= bounds.Min.X && x2+t < bounds.Max.X {
					img.SetRGBA(x2+t, y, c)
				}
			}
		}
	}
}

// drawLabel renders a solid-background label above a bounding box.
func drawLabel(img *image.RGBA, x, y int, text string, c color.RGBA) {
	bounds := img.Bounds()
	labelH := 14
	labelW := len(text)*7 + 4
	bgColor := color.RGBA{0, 0, 0, 180}

	ly := y - labelH
	if ly < bounds.Min.Y {
		ly = y + 2
	}

	for dy := 0; dy < labelH; dy++ {
		for dx := 0; dx < labelW; dx++ {
			px, py := x+dx, ly+dy
			if px >= bounds.Min.X && px < bounds.Max.X && py >= bounds.Min.Y && py < bounds.Max.Y {
				img.SetRGBA(px, py, bgColor)
			}
		}
	}

	for i, ch := range text {
		cx := x + 2 + i*7
		cy := ly + 2
		drawChar(img, cx, cy, byte(ch), c)
	}
}

// drawChar renders a minimal 5x7 bitmap character.
func drawChar(img *image.RGBA, x, y int, ch byte, c color.RGBA) {
	bounds := img.Bounds()
	for dy := 0; dy < 7; dy++ {
		for dx := 0; dx < 5; dx++ {
			px, py := x+dx, y+dy
			if px >= bounds.Min.X && px < bounds.Max.X && py >= bounds.Min.Y && py < bounds.Max.Y {
				if charPixel(ch, dx, dy) {
					img.SetRGBA(px, py, c)
				}
			}
		}
	}
}

// charPixel returns whether a pixel in a minimal font should be set.
// Simplified: just fills a small rectangle for non-space characters.
func charPixel(ch byte, dx, dy int) bool {
	if ch == ' ' {
		return false
	}
	if dx == 0 || dx == 4 || dy == 0 || dy == 6 {
		return true
	}
	if ch == '[' || ch == ']' {
		return dx <= 1 || dx >= 3
	}
	return dy == 3
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
