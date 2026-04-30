package visual

import (
	"context"
	"fmt"
	"image/color"
	"testing"
)

func TestSemanticDiffHighSimilarity(t *testing.T) {
	vlm := &StubVLMProvider{Similarity: 0.98, Description: "nearly identical pages"}
	d := NewSemanticDiffer(vlm)

	img := GenerateTestPNG(10, 10, color.White)
	result, err := d.Compare(context.Background(), img, img)
	if err != nil {
		t.Fatal(err)
	}
	if result.Similarity != 0.98 {
		t.Errorf("Similarity = %f, want 0.98", result.Similarity)
	}
	if result.Level != DiffMinor {
		t.Errorf("Level = %v, want minor for 0.98", result.Level)
	}
}

func TestSemanticDiffLowSimilarity(t *testing.T) {
	vlm := &StubVLMProvider{Similarity: 0.40, Description: "completely different layouts"}
	d := NewSemanticDiffer(vlm)

	img := GenerateTestPNG(10, 10, color.White)
	result, err := d.Compare(context.Background(), img, img)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != DiffCritical {
		t.Errorf("Level = %v, want critical for 0.40", result.Level)
	}
}

func TestSemanticDiffVLMError(t *testing.T) {
	vlm := &StubVLMProvider{Err: fmt.Errorf("VLM timeout")}
	d := NewSemanticDiffer(vlm)

	img := GenerateTestPNG(10, 10, color.White)
	_, err := d.Compare(context.Background(), img, img)
	if err == nil {
		t.Error("expected VLM error")
	}
}

func TestCompositeVerifierPixelOnly(t *testing.T) {
	pixel := NewPixelDiffer()
	v := NewCompositeVerifier(pixel, nil, DefaultConfig())

	img := GenerateTestPNG(50, 50, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	result, err := v.Compare(context.Background(), img, img)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pass {
		t.Error("identical images should pass")
	}
	if result.Score < 0.99 {
		t.Errorf("Score = %f, want ~1.0 for identical", result.Score)
	}
	if result.Semantic != nil {
		t.Error("Semantic should be nil without VLM")
	}
}

func TestCompositeVerifierWithSemantic(t *testing.T) {
	pixel := NewPixelDiffer()
	vlm := &StubVLMProvider{Similarity: 0.95, Description: "minor styling changes"}
	semantic := NewSemanticDiffer(vlm)
	cfg := DefaultConfig()
	v := NewCompositeVerifier(pixel, semantic, cfg)

	img := GenerateTestPNG(50, 50, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	result, err := v.Compare(context.Background(), img, img)
	if err != nil {
		t.Fatal(err)
	}
	if result.Semantic == nil {
		t.Fatal("Semantic should be populated")
	}
	if !result.Pass {
		t.Error("should pass with identical pixels and 0.95 semantic")
	}
	expected := cfg.PixelWeight*1.0 + cfg.SemanticWeight*0.95
	if diff := result.Score - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("Score = %f, want %f", result.Score, expected)
	}
}

func TestCompositeVerifierFailsOnPixelThreshold(t *testing.T) {
	pixel := NewPixelDiffer()
	vlm := &StubVLMProvider{Similarity: 0.99}
	semantic := NewSemanticDiffer(vlm)
	cfg := DefaultConfig()
	cfg.PixelThreshold = 1.0
	v := NewCompositeVerifier(pixel, semantic, cfg)

	baseline := GenerateGradientPNG(100, 100)
	current := GeneratePartialDiffPNG(100, 100, 0.05) // 5% diff > 1% threshold

	result, err := v.Compare(context.Background(), baseline, current)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass {
		t.Error("should fail: pixel diff exceeds threshold")
	}
}

func TestCompositeVerifierFailsOnSemanticThreshold(t *testing.T) {
	pixel := NewPixelDiffer()
	vlm := &StubVLMProvider{Similarity: 0.70}
	semantic := NewSemanticDiffer(vlm)
	cfg := DefaultConfig()
	cfg.SemanticThreshold = 0.85
	v := NewCompositeVerifier(pixel, semantic, cfg)

	img := GenerateTestPNG(50, 50, color.White)
	result, err := v.Compare(context.Background(), img, img)
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass {
		t.Error("should fail: semantic similarity below threshold")
	}
}

func TestCompositeVerifierSemanticErrorFallback(t *testing.T) {
	pixel := NewPixelDiffer()
	vlm := &StubVLMProvider{Err: fmt.Errorf("vlm down")}
	semantic := NewSemanticDiffer(vlm)
	cfg := DefaultConfig()
	v := NewCompositeVerifier(pixel, semantic, cfg)

	img := GenerateTestPNG(50, 50, color.White)
	result, err := v.Compare(context.Background(), img, img)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score < 0.99 {
		t.Errorf("pixel-only fallback Score = %f, want ~1.0", result.Score)
	}
}

func TestClassifySemanticDiff(t *testing.T) {
	tests := []struct {
		sim  float64
		want DiffLevel
	}{
		{1.00, DiffNone},
		{0.99, DiffNone},
		{0.97, DiffMinor},
		{0.90, DiffModerate},
		{0.75, DiffMajor},
		{0.50, DiffCritical},
	}
	for _, tt := range tests {
		if got := classifySemanticDiff(tt.sim); got != tt.want {
			t.Errorf("classifySemanticDiff(%f) = %v, want %v", tt.sim, got, tt.want)
		}
	}
}

func TestCompareSematicNilProvider(t *testing.T) {
	pixel := NewPixelDiffer()
	v := NewCompositeVerifier(pixel, nil, DefaultConfig())

	img := GenerateTestPNG(10, 10, color.White)
	_, err := v.CompareSemantic(context.Background(), img, img)
	if err == nil {
		t.Error("expected error for nil semantic provider")
	}
}
