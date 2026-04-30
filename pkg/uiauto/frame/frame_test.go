package frame

import (
	"context"
	"fmt"
	"testing"
)

func TestFrameTreeBasic(t *testing.T) {
	tree := NewFrameTree()
	if tree.Root == nil {
		t.Fatal("root should not be nil")
	}
	if tree.Count() != 1 {
		t.Errorf("Count = %d, want 1", tree.Count())
	}
	if tree.MaxDepth() != 0 {
		t.Errorf("MaxDepth = %d, want 0", tree.MaxDepth())
	}
}

func TestFrameTreeNested(t *testing.T) {
	tree := NewFrameTree()

	f1, err := tree.AddFrame("top", "iframe-1", "iframe.content", "content", "https://example.com", FrameIFrame)
	if err != nil {
		t.Fatal(err)
	}
	if f1.Depth != 1 {
		t.Errorf("f1.Depth = %d, want 1", f1.Depth)
	}

	f2, err := tree.AddFrame("iframe-1", "iframe-2", "iframe.nested", "nested", "https://example.com/inner", FrameIFrame)
	if err != nil {
		t.Fatal(err)
	}
	if f2.Depth != 2 {
		t.Errorf("f2.Depth = %d, want 2", f2.Depth)
	}

	if tree.Count() != 3 {
		t.Errorf("Count = %d, want 3", tree.Count())
	}
	if tree.MaxDepth() != 2 {
		t.Errorf("MaxDepth = %d, want 2", tree.MaxDepth())
	}
}

func TestFrameContextPath(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe#outer", "", "", FrameIFrame)
	tree.AddFrame("f1", "f2", "iframe#inner", "", "", FrameIFrame)

	f2 := tree.Frames["f2"]
	path := f2.Path()
	if len(path) != 2 {
		t.Fatalf("path length = %d, want 2", len(path))
	}
	if path[0] != "iframe#outer" || path[1] != "iframe#inner" {
		t.Errorf("path = %v, want [iframe#outer, iframe#inner]", path)
	}
}

func TestFrameContextIsNested(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe", "", "", FrameIFrame)
	tree.AddFrame("f1", "f2", "iframe", "", "", FrameIFrame)

	if tree.Frames["f1"].IsNested() {
		t.Error("depth-1 frame should not be nested")
	}
	if !tree.Frames["f2"].IsNested() {
		t.Error("depth-2 frame should be nested")
	}
}

func TestFrameContextRoot(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe", "", "", FrameIFrame)
	tree.AddFrame("f1", "f2", "iframe", "", "", FrameIFrame)

	root := tree.Frames["f2"].Root()
	if root.ID != "top" {
		t.Errorf("Root().ID = %q, want top", root.ID)
	}
}

func TestFrameContextFindChild(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe.a", "", "", FrameIFrame)
	tree.AddFrame("top", "f2", "iframe.b", "", "", FrameIFrame)

	if c := tree.Root.FindChild("iframe.a"); c == nil || c.ID != "f1" {
		t.Error("FindChild('iframe.a') should return f1")
	}
	if c := tree.Root.FindChild("iframe.c"); c != nil {
		t.Error("FindChild('iframe.c') should return nil")
	}
}

func TestAddFrameDuplicateID(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe", "", "", FrameIFrame)
	_, err := tree.AddFrame("top", "f1", "iframe2", "", "", FrameIFrame)
	if err == nil {
		t.Error("duplicate ID should return error")
	}
}

func TestAddFrameMissingParent(t *testing.T) {
	tree := NewFrameTree()
	_, err := tree.AddFrame("nonexistent", "f1", "iframe", "", "", FrameIFrame)
	if err == nil {
		t.Error("missing parent should return error")
	}
}

func TestFrameTypeString(t *testing.T) {
	tests := []struct {
		ft   FrameType
		want string
	}{
		{FrameTop, "top"},
		{FrameIFrame, "iframe"},
		{FrameShadow, "shadow"},
		{FrameType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.ft.String(); got != tt.want {
			t.Errorf("FrameType(%d) = %q, want %q", tt.ft, got, tt.want)
		}
	}
}

// --- Frame healer tests with mocks ---

type mockNavigator struct {
	enteredPaths [][]string
	domHTML      string
	enterErr     error
	domErr       error
}

func (m *mockNavigator) EnterFrame(_ context.Context, path []string) error {
	m.enteredPaths = append(m.enteredPaths, path)
	return m.enterErr
}

func (m *mockNavigator) ExitToTop(_ context.Context) error { return nil }

func (m *mockNavigator) CaptureFrameDOM(_ context.Context) (string, error) {
	return m.domHTML, m.domErr
}

type mockResolver struct {
	candidates []SelectorCandidate
	err        error
}

func (m *mockResolver) ResolveCandidates(_ context.Context, _ string, _ string) ([]SelectorCandidate, error) {
	return m.candidates, m.err
}

func TestFrameHealerSuccess(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe#login", "", "", FrameIFrame)
	tree.AddFrame("f1", "f2", "iframe#form", "", "", FrameIFrame)

	nav := &mockNavigator{domHTML: "<html><input name='user'/></html>"}
	resolver := &mockResolver{candidates: []SelectorCandidate{
		{Selector: "[name='user']", Confidence: 0.92, Method: "attribute", FrameID: "f2"},
		{Selector: "input:first-child", Confidence: 0.65, Method: "structural", FrameID: "f2"},
	}}

	healer := NewFrameHealer(tree, nav, resolver)
	result, err := healer.Heal(context.Background(), "f2", "#username")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.BestCandidate.Selector != "[name='user']" {
		t.Errorf("best = %q, want [name='user']", result.BestCandidate.Selector)
	}
	if len(result.FramePath) != 2 {
		t.Errorf("FramePath length = %d, want 2", len(result.FramePath))
	}
}

func TestFrameHealerNoCandidates(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe", "", "", FrameIFrame)

	nav := &mockNavigator{domHTML: "<html></html>"}
	resolver := &mockResolver{candidates: nil}

	healer := NewFrameHealer(tree, nav, resolver)
	result, err := healer.Heal(context.Background(), "f1", "#missing")
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("should fail with no candidates")
	}
}

func TestFrameHealerNavigationError(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe", "", "", FrameIFrame)

	nav := &mockNavigator{enterErr: fmt.Errorf("frame detached")}
	resolver := &mockResolver{}

	healer := NewFrameHealer(tree, nav, resolver)
	_, err := healer.Heal(context.Background(), "f1", "#test")
	if err == nil {
		t.Error("expected navigation error")
	}
}

func TestFrameHealerUnknownFrame(t *testing.T) {
	tree := NewFrameTree()
	nav := &mockNavigator{}
	resolver := &mockResolver{}

	healer := NewFrameHealer(tree, nav, resolver)
	_, err := healer.Heal(context.Background(), "nonexistent", "#test")
	if err == nil {
		t.Error("expected unknown frame error")
	}
}

func TestFrameHealerDOMError(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe", "", "", FrameIFrame)

	nav := &mockNavigator{domErr: fmt.Errorf("dom capture failed")}
	resolver := &mockResolver{}

	healer := NewFrameHealer(tree, nav, resolver)
	_, err := healer.Heal(context.Background(), "f1", "#test")
	if err == nil {
		t.Error("expected DOM capture error")
	}
}

func TestFrameHealerLowConfidence(t *testing.T) {
	tree := NewFrameTree()
	tree.AddFrame("top", "f1", "iframe", "", "", FrameIFrame)

	nav := &mockNavigator{domHTML: "<html></html>"}
	resolver := &mockResolver{candidates: []SelectorCandidate{
		{Selector: "div.maybe", Confidence: 0.3, Method: "guess"},
	}}

	healer := NewFrameHealer(tree, nav, resolver)
	result, err := healer.Heal(context.Background(), "f1", "#test")
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Error("low confidence should not be success")
	}
}
