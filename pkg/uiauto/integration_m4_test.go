package uiauto_test

import (
	"context"
	"encoding/json"
	"image/color"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/action"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/aiwright"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/frame"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/omniparser"
	"github.com/nfsarch33/uiauto-framework/pkg/uiauto/visual"
)

func TestM4IntegrationAiWrightToVisualDiff(t *testing.T) {
	// Mock ai-wright SOM server
	somSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		json.NewEncoder(w).Encode(aiwright.SOMResult{
			ScreenWidth: 1920, ScreenHeight: 1080,
			Elements: []aiwright.SOMElement{
				{ID: 1, Label: "Username", Type: aiwright.ElementInput, BoundingBox: aiwright.BoundingBox{X: 760, Y: 340, Width: 400, Height: 40}, Confidence: 0.97},
				{ID: 2, Label: "Log In", Type: aiwright.ElementButton, BoundingBox: aiwright.BoundingBox{X: 860, Y: 470, Width: 200, Height: 44}, Confidence: 0.99},
			},
		})
	}))
	defer somSrv.Close()

	// 1. SOM annotation
	client := aiwright.NewClient(somSrv.URL)
	result, err := client.Annotate(context.Background(), []byte("fake-screenshot"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Elements) != 2 {
		t.Fatalf("SOM elements = %d, want 2", len(result.Elements))
	}

	// 2. Visual regression on identical images
	baseline := visual.GenerateTestPNG(100, 100, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	differ := visual.NewPixelDiffer()
	pixelResult, err := differ.Compare(context.Background(), baseline, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if pixelResult.Level != visual.DiffNone {
		t.Errorf("pixel diff level = %v, want none", pixelResult.Level)
	}

	// 3. Composite verify with stub VLM
	vlm := &visual.StubVLMProvider{Similarity: 0.99, Description: "identical"}
	semantic := visual.NewSemanticDiffer(vlm)
	verifier := visual.NewCompositeVerifier(differ, semantic, visual.DefaultConfig())
	comp, err := verifier.Compare(context.Background(), baseline, baseline)
	if err != nil {
		t.Fatal(err)
	}
	if !comp.Pass {
		t.Error("identical images should pass composite verification")
	}

	t.Logf("M4 integration: SOM=%d elements, pixel=%s, composite=%.2f", len(result.Elements), pixelResult.Level, comp.Score)
}

func TestM4IntegrationActionSequenceWithFrames(t *testing.T) {
	// Build frame tree for 2-level nested iframe
	tree := frame.NewFrameTree()
	tree.AddFrame("top", "outer", "iframe#outer", "", "", frame.FrameIFrame)
	tree.AddFrame("outer", "inner", "iframe#inner", "", "", frame.FrameIFrame)

	if tree.MaxDepth() != 2 {
		t.Errorf("max depth = %d, want 2", tree.MaxDepth())
	}

	// Build and run an action sequence that simulates frame navigation
	var steps []string
	seq := action.NewSequence("frame-nav-test").
		Setup("init-browser", func(_ context.Context) error {
			steps = append(steps, "init")
			return nil
		}).
		Execute("enter-outer", func(_ context.Context) error {
			steps = append(steps, "outer")
			return nil
		}).
		Execute("enter-inner", func(_ context.Context) error {
			steps = append(steps, "inner")
			return nil
		}).
		Verify("check-element", func(_ context.Context) error {
			steps = append(steps, "verify")
			return nil
		}).
		Teardown("cleanup", func(_ context.Context) error {
			steps = append(steps, "cleanup")
			return nil
		}).
		Build()

	if err := seq.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	expected := []string{"init", "outer", "inner", "verify", "cleanup"}
	if len(steps) != len(expected) {
		t.Fatalf("steps = %v, want %v", steps, expected)
	}
	for i := range expected {
		if steps[i] != expected[i] {
			t.Errorf("step %d = %q, want %q", i, steps[i], expected[i])
		}
	}
}

func TestM4IntegrationOmniParserMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		json.NewEncoder(w).Encode(omniparser.ParseResult{
			Layout: omniparser.LayoutInfo{Width: 1920, Height: 1080, PageType: "dashboard", Regions: 5},
			Elements: []omniparser.UIElement{
				{ID: 1, Type: "button", Text: "Submit", Confidence: 0.95, Interactable: true},
				{ID: 2, Type: "input", Text: "Search", Confidence: 0.93, Interactable: true},
				{ID: 3, Type: "text", Text: "Welcome", Confidence: 0.87, Interactable: false},
			},
		})
	}))
	defer srv.Close()

	client := omniparser.NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Parse(ctx, []byte("screenshot"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Elements) != 3 {
		t.Errorf("elements = %d, want 3", len(result.Elements))
	}

	inter := result.FindInteractable()
	if len(inter) != 2 {
		t.Errorf("interactable = %d, want 2", len(inter))
	}
}

func TestM4IntegrationFrameHealerWithActionSequence(t *testing.T) {
	tree := frame.NewFrameTree()
	tree.AddFrame("top", "f1", "iframe#content", "", "", frame.FrameIFrame)

	nav := &stubNavigator{html: "<html><input data-testid='email'/></html>"}
	resolver := &stubResolver{candidates: []frame.SelectorCandidate{
		{Selector: "[data-testid='email']", Confidence: 0.94, Method: "attribute"},
	}}

	healer := frame.NewFrameHealer(tree, nav, resolver)

	var healedSelector string
	seq := action.NewSequence("heal-and-interact").
		Setup("navigate", func(_ context.Context) error { return nil }).
		Execute("heal-selector", func(ctx context.Context) error {
			result, err := healer.Heal(ctx, "f1", "#old-email")
			if err != nil {
				return err
			}
			if result.BestCandidate != nil {
				healedSelector = result.BestCandidate.Selector
			}
			return nil
		}).
		Verify("check-healed", func(_ context.Context) error {
			if healedSelector != "[data-testid='email']" {
				t.Errorf("healed = %q, want [data-testid='email']", healedSelector)
			}
			return nil
		}).
		Build()

	if err := seq.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type stubNavigator struct {
	html string
}

func (s *stubNavigator) EnterFrame(_ context.Context, _ []string) error    { return nil }
func (s *stubNavigator) ExitToTop(_ context.Context) error                 { return nil }
func (s *stubNavigator) CaptureFrameDOM(_ context.Context) (string, error) { return s.html, nil }

type stubResolver struct {
	candidates []frame.SelectorCandidate
}

func (s *stubResolver) ResolveCandidates(_ context.Context, _ string, _ string) ([]frame.SelectorCandidate, error) {
	return s.candidates, nil
}
