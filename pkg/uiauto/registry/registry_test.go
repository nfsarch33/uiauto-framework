package registry

import (
	"sync"
	"testing"
)

func TestRegisterAndLookup(t *testing.T) {
	reg := New()

	err := reg.Register(PagePattern{
		Route: `/products/\d+`,
		Name:  "Product Detail",
		Selectors: []SelectorGroup{
			{ElementName: "add-to-cart", Primary: "#add-to-cart-btn", Strategy: "id"},
			{ElementName: "price", Primary: ".product-price", Fallbacks: []string{"[data-price]"}, Strategy: "css"},
		},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	match := reg.Lookup("/products/123")
	if match == nil {
		t.Fatal("expected match for /products/123")
	}
	if match.Pattern.Name != "Product Detail" {
		t.Errorf("Name = %q, want %q", match.Pattern.Name, "Product Detail")
	}
	if len(match.Pattern.Selectors) != 2 {
		t.Errorf("Selectors = %d, want 2", len(match.Pattern.Selectors))
	}
}

func TestLookupNoMatch(t *testing.T) {
	reg := New()
	_ = reg.Register(PagePattern{Route: `/products/\d+`, Name: "Product"})

	match := reg.Lookup("/about")
	if match != nil {
		t.Errorf("expected no match for /about, got %v", match)
	}
}

func TestLookupBestMatch(t *testing.T) {
	reg := New()
	_ = reg.Register(PagePattern{Route: `/.*`, Name: "Catch All"})
	_ = reg.Register(PagePattern{Route: `/products/\d+`, Name: "Product Detail"})

	match := reg.Lookup("/products/42")
	if match == nil {
		t.Fatal("expected match")
	}
	if match.Pattern.Name != "Product Detail" {
		t.Errorf("expected Product Detail (more specific), got %q", match.Pattern.Name)
	}
}

func TestRegisterInvalidRegex(t *testing.T) {
	reg := New()
	err := reg.Register(PagePattern{Route: `[invalid`})
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestRemove(t *testing.T) {
	reg := New()
	_ = reg.Register(PagePattern{Route: `/test`, Name: "Test"})

	if reg.Size() != 1 {
		t.Errorf("Size = %d, want 1", reg.Size())
	}

	ok := reg.Remove("/test")
	if !ok {
		t.Error("expected Remove to return true")
	}
	if reg.Size() != 0 {
		t.Errorf("Size = %d after remove, want 0", reg.Size())
	}

	ok = reg.Remove("/nonexistent")
	if ok {
		t.Error("expected Remove to return false for nonexistent")
	}
}

func TestAll(t *testing.T) {
	reg := New()
	_ = reg.Register(PagePattern{Route: `/a`, Name: "A"})
	_ = reg.Register(PagePattern{Route: `/b`, Name: "B"})

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("All() = %d, want 2", len(all))
	}
}

func TestHitCount(t *testing.T) {
	reg := New()
	_ = reg.Register(PagePattern{Route: `/test`, Name: "Test"})

	for i := 0; i < 5; i++ {
		reg.Lookup("/test")
	}

	match := reg.Lookup("/test")
	if match.Pattern.HitCount != 6 {
		t.Errorf("HitCount = %d, want 6", match.Pattern.HitCount)
	}
}

func TestConcurrentAccess(t *testing.T) {
	reg := New()
	_ = reg.Register(PagePattern{Route: `/products/\d+`, Name: "Product"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg.Lookup("/products/1")
		}()
	}
	wg.Wait()
}

func TestUpdateExisting(t *testing.T) {
	reg := New()
	_ = reg.Register(PagePattern{Route: `/test`, Name: "V1"})
	_ = reg.Register(PagePattern{Route: `/test`, Name: "V2"})

	if reg.Size() != 1 {
		t.Errorf("Size = %d, want 1 (should update in place)", reg.Size())
	}

	match := reg.Lookup("/test")
	if match.Pattern.Name != "V2" {
		t.Errorf("Name = %q, want V2", match.Pattern.Name)
	}
}

func TestMatchScore(t *testing.T) {
	exact := matchScore("/test", "/test")
	if exact != 1.0 {
		t.Errorf("exact match score = %f, want 1.0", exact)
	}

	anchored := matchScore(`^/test$`, "/test")
	if anchored < 0.5 {
		t.Errorf("anchored score = %f, want >= 0.5", anchored)
	}
}
