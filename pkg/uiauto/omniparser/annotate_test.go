package omniparser

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestAnnotateScreenshot(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.White)
		}
	}

	tmpFile := filepath.Join(t.TempDir(), "test.png")
	f, err := os.Create(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	screenshot, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	result := &ParseResult{
		Elements: []UIElement{
			{
				ID:           0,
				Type:         "button",
				Text:         "Accept Call",
				BoundingBox:  BoundingBox{X: 20, Y: 30, Width: 80, Height: 25},
				Confidence:   0.95,
				Interactable: true,
			},
			{
				ID:           1,
				Type:         "button",
				Text:         "Decline",
				BoundingBox:  BoundingBox{X: 110, Y: 30, Width: 60, Height: 25},
				Confidence:   0.88,
				Interactable: true,
			},
		},
	}

	annotated, err := AnnotateScreenshot(screenshot, result)
	if err != nil {
		t.Fatalf("AnnotateScreenshot: %v", err)
	}
	if annotated == nil {
		t.Fatal("annotated image is nil")
	}

	bounds := annotated.Bounds()
	if bounds.Dx() != 200 || bounds.Dy() != 200 {
		t.Errorf("expected 200x200, got %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestSaveAnnotatedPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))

	outPath := filepath.Join(t.TempDir(), "annotated.png")
	if err := SaveAnnotatedPNG(img, outPath); err != nil {
		t.Fatalf("SaveAnnotatedPNG: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}
}

func TestAnnotateScreenshot_EmptyElements(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	tmpFile := filepath.Join(t.TempDir(), "small.png")
	f, _ := os.Create(tmpFile)
	_ = png.Encode(f, img)
	f.Close()
	screenshot, _ := os.ReadFile(tmpFile)

	annotated, err := AnnotateScreenshot(screenshot, &ParseResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if annotated == nil {
		t.Fatal("annotated should not be nil for empty elements")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a very long label", 15, "this is a ve..."},
		{"abc", 3, "abc"},
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.max)
		if got != tc.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.expected)
		}
	}
}

func TestPickColor(t *testing.T) {
	c0 := pickColor(0)
	c1 := pickColor(1)
	if c0 == c1 {
		t.Error("expected different colors for different IDs")
	}

	cWrap := pickColor(7)
	if cWrap != pickColor(0) {
		t.Error("expected color to wrap around after 7")
	}
}
