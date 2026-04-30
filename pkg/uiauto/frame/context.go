package frame

import (
	"fmt"
	"log/slog"
)

// FrameType classifies frame origins.
type FrameType int

const (
	FrameTop    FrameType = iota // top-level document
	FrameIFrame                  // <iframe> element
	FrameShadow                  // shadow DOM root
)

func (f FrameType) String() string {
	switch f {
	case FrameTop:
		return "top"
	case FrameIFrame:
		return "iframe"
	case FrameShadow:
		return "shadow"
	default:
		return "unknown"
	}
}

// FrameContext describes a frame in the page hierarchy.
type FrameContext struct {
	ID       string
	Type     FrameType
	Selector string // CSS selector of the iframe element (empty for top)
	Name     string // frame name attribute
	Src      string // frame src URL
	Depth    int
	Parent   *FrameContext
	Children []*FrameContext
}

// Path returns the selector chain from top to this frame.
func (fc *FrameContext) Path() []string {
	if fc.Parent == nil {
		return nil
	}
	return append(fc.Parent.Path(), fc.Selector)
}

// IsNested reports whether this frame is inside another iframe.
func (fc *FrameContext) IsNested() bool {
	return fc.Depth > 1
}

// Root walks up the tree to find the top-level frame.
func (fc *FrameContext) Root() *FrameContext {
	if fc.Parent == nil {
		return fc
	}
	return fc.Parent.Root()
}

// FindChild returns the first child matching the given selector.
func (fc *FrameContext) FindChild(selector string) *FrameContext {
	for _, c := range fc.Children {
		if c.Selector == selector {
			return c
		}
	}
	return nil
}

// FrameTree holds the complete frame hierarchy for a page.
type FrameTree struct {
	Root   *FrameContext
	Frames map[string]*FrameContext
	logger *slog.Logger
}

// NewFrameTree creates a tree rooted at the top-level document.
func NewFrameTree() *FrameTree {
	root := &FrameContext{
		ID:   "top",
		Type: FrameTop,
	}
	return &FrameTree{
		Root:   root,
		Frames: map[string]*FrameContext{"top": root},
		logger: slog.Default(),
	}
}

// AddFrame registers a child frame under the specified parent.
func (ft *FrameTree) AddFrame(parentID, id, selector, name, src string, frameType FrameType) (*FrameContext, error) {
	parent, ok := ft.Frames[parentID]
	if !ok {
		return nil, fmt.Errorf("frame: parent %q not found", parentID)
	}
	if _, exists := ft.Frames[id]; exists {
		return nil, fmt.Errorf("frame: %q already exists", id)
	}

	child := &FrameContext{
		ID:       id,
		Type:     frameType,
		Selector: selector,
		Name:     name,
		Src:      src,
		Depth:    parent.Depth + 1,
		Parent:   parent,
	}
	parent.Children = append(parent.Children, child)
	ft.Frames[id] = child

	ft.logger.Info("frame added",
		slog.String("id", id),
		slog.String("parent", parentID),
		slog.Int("depth", child.Depth),
	)
	return child, nil
}

// MaxDepth returns the deepest nesting level in the tree.
func (ft *FrameTree) MaxDepth() int {
	max := 0
	for _, f := range ft.Frames {
		if f.Depth > max {
			max = f.Depth
		}
	}
	return max
}

// Count returns the total number of frames.
func (ft *FrameTree) Count() int {
	return len(ft.Frames)
}
