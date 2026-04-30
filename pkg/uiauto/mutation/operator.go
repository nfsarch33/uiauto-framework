package mutation

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// OperatorType identifies the class of DOM mutation.
type OperatorType string

const (
	OpRenameClass     OperatorType = "rename_class"
	OpRemoveID        OperatorType = "remove_id"
	OpChangeTestID    OperatorType = "change_test_id"
	OpWrapElement     OperatorType = "wrap_element"
	OpReorderSiblings OperatorType = "reorder_siblings"
	OpChangeTag       OperatorType = "change_tag"
)

// Tier classifies mutation realism (A=high, B=medium, C=synthetic).
type Tier string

const (
	TierA Tier = "A"
	TierB Tier = "B"
	TierC Tier = "C"
)

// Operator applies a single mutation to matched elements.
type Operator struct {
	Type        OperatorType
	Tier        Tier
	Description string
	apply       func(doc *goquery.Document, selector string) (int, error)
}

// Apply runs the mutation on all elements matching selector.
// Returns the number of elements mutated.
func (o *Operator) Apply(doc *goquery.Document, selector string) (int, error) {
	return o.apply(doc, selector)
}

// RenameClass appends a suffix to all class names on matched elements.
func RenameClass(suffix string) *Operator {
	return &Operator{
		Type:        OpRenameClass,
		Tier:        TierA,
		Description: fmt.Sprintf("Rename CSS classes by appending '%s'", suffix),
		apply: func(doc *goquery.Document, selector string) (int, error) {
			count := 0
			doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
				if classes, exists := s.Attr("class"); exists {
					parts := strings.Fields(classes)
					for i, c := range parts {
						parts[i] = c + suffix
					}
					s.SetAttr("class", strings.Join(parts, " "))
					count++
				}
			})
			return count, nil
		},
	}
}

// RemoveID strips the id attribute from matched elements.
func RemoveID() *Operator {
	return &Operator{
		Type:        OpRemoveID,
		Tier:        TierA,
		Description: "Remove id attribute from elements",
		apply: func(doc *goquery.Document, selector string) (int, error) {
			count := 0
			doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
				if _, exists := s.Attr("id"); exists {
					s.RemoveAttr("id")
					count++
				}
			})
			return count, nil
		},
	}
}

// ChangeTestID modifies data-testid attributes by adding a prefix.
func ChangeTestID(prefix string) *Operator {
	return &Operator{
		Type:        OpChangeTestID,
		Tier:        TierA,
		Description: fmt.Sprintf("Change data-testid by prepending '%s'", prefix),
		apply: func(doc *goquery.Document, selector string) (int, error) {
			count := 0
			doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
				if val, exists := s.Attr("data-testid"); exists {
					s.SetAttr("data-testid", prefix+val)
					count++
				}
			})
			return count, nil
		},
	}
}

// WrapElement wraps each matched element in a new parent div.
func WrapElement(wrapperClass string) *Operator {
	return &Operator{
		Type:        OpWrapElement,
		Tier:        TierA,
		Description: fmt.Sprintf("Wrap elements in <div class='%s'>", wrapperClass),
		apply: func(doc *goquery.Document, selector string) (int, error) {
			count := 0
			doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
				s.WrapHtml(fmt.Sprintf(`<div class="%s"></div>`, wrapperClass))
				count++
			})
			return count, nil
		},
	}
}

// ReorderSiblings reverses the order of direct children of matched elements.
func ReorderSiblings() *Operator {
	return &Operator{
		Type:        OpReorderSiblings,
		Tier:        TierA,
		Description: "Reverse order of child elements",
		apply: func(doc *goquery.Document, selector string) (int, error) {
			count := 0
			doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
				children := s.Children()
				if children.Length() < 2 {
					return
				}
				// Collect HTML of children in reverse
				var reversed []string
				children.Each(func(_ int, child *goquery.Selection) {
					html, err := goquery.OuterHtml(child)
					if err == nil {
						reversed = append([]string{html}, reversed...)
					}
				})
				s.Children().Remove()
				s.AppendHtml(strings.Join(reversed, ""))
				count++
			})
			return count, nil
		},
	}
}

// ChangeTag replaces the tag name of matched elements (preserving attributes and children).
func ChangeTag(newTag string) *Operator {
	return &Operator{
		Type:        OpChangeTag,
		Tier:        TierA,
		Description: fmt.Sprintf("Change element tag to <%s>", newTag),
		apply: func(doc *goquery.Document, selector string) (int, error) {
			count := 0
			doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
				oldNode := s.Nodes[0]
				oldNode.Data = newTag
				count++
			})
			return count, nil
		},
	}
}
