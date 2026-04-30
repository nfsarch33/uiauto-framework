package visual

import (
	"context"
	"image/color"
	"testing"
)

func TestPixelDiffIdentical(t *testing.T) {
	img := GenerateTestPNG(100, 100, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	d := NewPixelDiffer()
	result, err := d.Compare(context.Background(), img, img)
	if err != nil {
		t.Fatal(err)
	}
	if result.DiffPixels != 0 {
		t.Errorf("identical images: DiffPixels = %d, want 0", result.DiffPixels)
	}
	if result.Level != DiffNone {
		t.Errorf("Level = %v, want none", result.Level)
	}
	if result.TotalPixels != 10000 {
		t.Errorf("TotalPixels = %d, want 10000", result.TotalPixels)
	}
}

func TestPixelDiffSmallChange(t *testing.T) {
	baseline := GenerateGradientPNG(200, 100)
	current := GeneratePartialDiffPNG(200, 100, 0.005) // 0.5% diff

	d := NewPixelDiffer()
	result, err := d.Compare(context.Background(), baseline, current)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != DiffMinor {
		t.Errorf("0.5%% diff: Level = %v, want minor", result.Level)
	}
	if result.DiffPercentage >= 1.0 {
		t.Errorf("DiffPercentage = %f, want < 1.0", result.DiffPercentage)
	}
}

func TestPixelDiffModerateChange(t *testing.T) {
	baseline := GenerateGradientPNG(200, 100)
	current := GeneratePartialDiffPNG(200, 100, 0.03) // ~3% diff

	d := NewPixelDiffer()
	result, err := d.Compare(context.Background(), baseline, current)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != DiffModerate {
		t.Errorf("3%% diff: Level = %v, want moderate (pct=%.2f)", result.Level, result.DiffPercentage)
	}
}

func TestPixelDiffMajorChange(t *testing.T) {
	baseline := GenerateGradientPNG(100, 100)
	current := GeneratePartialDiffPNG(100, 100, 0.10) // 10% diff

	d := NewPixelDiffer()
	result, err := d.Compare(context.Background(), baseline, current)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != DiffMajor {
		t.Errorf("10%% diff: Level = %v, want major (pct=%.2f)", result.Level, result.DiffPercentage)
	}
}

func TestPixelDiffCriticalChange(t *testing.T) {
	a := GenerateTestPNG(100, 100, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	b := GenerateTestPNG(100, 100, color.RGBA{R: 255, G: 255, B: 255, A: 255})

	d := NewPixelDiffer()
	result, err := d.Compare(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if result.Level != DiffCritical {
		t.Errorf("100%% diff: Level = %v, want critical", result.Level)
	}
	if result.DiffPercentage < 99.0 {
		t.Errorf("DiffPercentage = %f, want ~100", result.DiffPercentage)
	}
}

func TestPixelDiffSizeMismatch(t *testing.T) {
	a := GenerateTestPNG(100, 100, color.White)
	b := GenerateTestPNG(200, 100, color.White)

	d := NewPixelDiffer()
	_, err := d.Compare(context.Background(), a, b)
	if err == nil {
		t.Error("expected size mismatch error")
	}
}

func TestPixelDiffInvalidPNG(t *testing.T) {
	valid := GenerateTestPNG(10, 10, color.White)
	d := NewPixelDiffer()

	_, err := d.Compare(context.Background(), []byte("not-a-png"), valid)
	if err == nil {
		t.Error("expected decode error for invalid baseline")
	}

	_, err = d.Compare(context.Background(), valid, []byte("not-a-png"))
	if err == nil {
		t.Error("expected decode error for invalid current")
	}
}

func TestPixelDiffTolerance(t *testing.T) {
	a := GenerateTestPNG(10, 10, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	b := GenerateTestPNG(10, 10, color.RGBA{R: 105, G: 100, B: 100, A: 255})

	strict := NewPixelDiffer(WithTolerance(0))
	result, err := strict.Compare(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if result.DiffPixels == 0 {
		t.Error("strict tolerance should detect 5-unit channel diff")
	}

	lenient := NewPixelDiffer(WithTolerance(10))
	result, err = lenient.Compare(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if result.DiffPixels != 0 {
		t.Errorf("lenient tolerance should ignore 5-unit diff, got %d diff pixels", result.DiffPixels)
	}
}

func TestPixelDiffRegionTracking(t *testing.T) {
	baseline := GenerateGradientPNG(100, 100)
	current := GeneratePartialDiffPNG(100, 100, 0.10)

	d := NewPixelDiffer()
	result, err := d.Compare(context.Background(), baseline, current)
	if err != nil {
		t.Fatal(err)
	}

	if result.MaxRegionDiff.X != 0 {
		t.Errorf("diff region X = %d, want 0 (diff starts at left)", result.MaxRegionDiff.X)
	}
	if result.MaxRegionDiff.W <= 0 {
		t.Error("diff region should have positive width")
	}
}

func TestPixelDiffContextCancellation(t *testing.T) {
	img := GenerateTestPNG(10, 10, color.White)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	d := NewPixelDiffer()
	_, err := d.Compare(ctx, img, img)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestDiffLevelString(t *testing.T) {
	tests := []struct {
		level DiffLevel
		want  string
	}{
		{DiffNone, "none"},
		{DiffMinor, "minor"},
		{DiffModerate, "moderate"},
		{DiffMajor, "major"},
		{DiffCritical, "critical"},
		{DiffLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.want {
			t.Errorf("DiffLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
		}
	}
}

func TestGenerateTestPNG(t *testing.T) {
	data := GenerateTestPNG(50, 30, color.White)
	if len(data) == 0 {
		t.Error("GenerateTestPNG returned empty")
	}
}

func TestGenerateGradientPNG(t *testing.T) {
	data := GenerateGradientPNG(50, 30)
	if len(data) == 0 {
		t.Error("GenerateGradientPNG returned empty")
	}
}
