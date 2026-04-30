package aiwright

import "time"

// BoundingBox represents a UI element's screen-space coordinates.
type BoundingBox struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Center returns the center point of the bounding box.
func (b BoundingBox) Center() (int, int) {
	return b.X + b.Width/2, b.Y + b.Height/2
}

// Area returns the pixel area of the bounding box.
func (b BoundingBox) Area() int {
	return b.Width * b.Height
}

// Contains reports whether point (px, py) falls inside the bounding box.
func (b BoundingBox) Contains(px, py int) bool {
	return px >= b.X && px < b.X+b.Width && py >= b.Y && py < b.Y+b.Height
}

// Overlaps reports whether two bounding boxes share any area.
func (b BoundingBox) Overlaps(other BoundingBox) bool {
	return b.X < other.X+other.Width && b.X+b.Width > other.X &&
		b.Y < other.Y+other.Height && b.Y+b.Height > other.Y
}

// ElementType categorizes detected UI elements.
type ElementType string

const (
	ElementButton   ElementType = "button"
	ElementInput    ElementType = "input"
	ElementLink     ElementType = "link"
	ElementText     ElementType = "text"
	ElementImage    ElementType = "image"
	ElementDropdown ElementType = "dropdown"
	ElementCheckbox ElementType = "checkbox"
	ElementUnknown  ElementType = "unknown"
)

// SOMElement is a single numbered UI element detected from a screenshot.
type SOMElement struct {
	ID          int               `json:"id"`
	Label       string            `json:"label"`
	Type        ElementType       `json:"type"`
	BoundingBox BoundingBox       `json:"bounding_box"`
	Confidence  float64           `json:"confidence"`
	Attributes  map[string]string `json:"attributes,omitempty"`
}

// SOMResult is the structured response from the SOM annotation endpoint.
type SOMResult struct {
	Elements       []SOMElement  `json:"elements"`
	AnnotatedImage string        `json:"annotated_image,omitempty"`
	ScreenWidth    int           `json:"screen_width"`
	ScreenHeight   int           `json:"screen_height"`
	ModelVersion   string        `json:"model_version,omitempty"`
	Latency        time.Duration `json:"-"`
}

// FindByID returns the element with the given SOM ID, or nil.
func (r SOMResult) FindByID(id int) *SOMElement {
	for i := range r.Elements {
		if r.Elements[i].ID == id {
			return &r.Elements[i]
		}
	}
	return nil
}

// FindByType returns all elements matching the given type.
func (r SOMResult) FindByType(t ElementType) []SOMElement {
	var result []SOMElement
	for _, e := range r.Elements {
		if e.Type == t {
			result = append(result, e)
		}
	}
	return result
}

// FindByLabel returns the first element whose label contains the substring.
func (r SOMResult) FindByLabel(substr string) *SOMElement {
	for i := range r.Elements {
		if len(r.Elements[i].Label) > 0 && contains(r.Elements[i].Label, substr) {
			return &r.Elements[i]
		}
	}
	return nil
}

func contains(s, substr string) bool {
	return len(substr) <= len(s) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
