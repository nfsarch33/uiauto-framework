package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		target  TargetConfig
		wantErr bool
	}{
		{
			"valid",
			TargetConfig{
				Name:    "Sauce Demo",
				BaseURL: "https://www.saucedemo.com",
				Pages:   []PageConfig{{Name: "Login", Path: "/"}},
			},
			false,
		},
		{
			"missing name",
			TargetConfig{BaseURL: "https://example.com", Pages: []PageConfig{{Name: "P", Path: "/"}}},
			true,
		},
		{
			"missing url",
			TargetConfig{Name: "Test", Pages: []PageConfig{{Name: "P", Path: "/"}}},
			true,
		},
		{
			"no pages",
			TargetConfig{Name: "Test", BaseURL: "https://example.com"},
			true,
		},
		{
			"page missing name",
			TargetConfig{Name: "Test", BaseURL: "https://example.com", Pages: []PageConfig{{Path: "/"}}},
			true,
		},
		{
			"page missing path",
			TargetConfig{Name: "Test", BaseURL: "https://example.com", Pages: []PageConfig{{Name: "P"}}},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.target.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestFullURL(t *testing.T) {
	tc := TargetConfig{BaseURL: "https://www.saucedemo.com"}
	page := PageConfig{Path: "/inventory.html"}
	got := tc.FullURL(page)
	want := "https://www.saucedemo.com/inventory.html"
	if got != want {
		t.Errorf("FullURL = %q, want %q", got, want)
	}
}

func TestLoadTargets(t *testing.T) {
	dir := t.TempDir()
	data := `[
		{
			"name": "Sauce Demo",
			"base_url": "https://www.saucedemo.com",
			"pages": [
				{"name": "Login", "path": "/", "selectors": {"username": "#user-name", "password": "#password"}},
				{"name": "Inventory", "path": "/inventory.html", "selectors": {"cart": ".shopping_cart_link"}}
			]
		},
		{
			"name": "D2L Brightspace",
			"base_url": "https://d2l.deakin.edu.au",
			"auth": {"type": "sso", "token_env": "D2L_TOKEN"},
			"pages": [
				{"name": "Dashboard", "path": "/d2l/home", "selectors": {"nav": ".d2l-navigation"}}
			]
		}
	]`

	path := filepath.Join(dir, "targets.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	targets, err := LoadTargets(path)
	if err != nil {
		t.Fatalf("LoadTargets: %v", err)
	}

	if len(targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(targets))
	}
	if targets[0].Name != "Sauce Demo" {
		t.Errorf("targets[0].Name = %q", targets[0].Name)
	}
	if targets[1].Auth == nil {
		t.Error("targets[1].Auth should not be nil")
	}
	if targets[1].Auth.TokenEnv != "D2L_TOKEN" {
		t.Errorf("Auth.TokenEnv = %q", targets[1].Auth.TokenEnv)
	}
}

func TestLoadTargetsInvalid(t *testing.T) {
	dir := t.TempDir()

	t.Run("bad json", func(t *testing.T) {
		path := filepath.Join(dir, "bad.json")
		os.WriteFile(path, []byte("not json"), 0o644)
		_, err := LoadTargets(path)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := LoadTargets(filepath.Join(dir, "nonexistent.json"))
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		path := filepath.Join(dir, "invalid.json")
		os.WriteFile(path, []byte(`[{"name":"","base_url":"x","pages":[{"name":"P","path":"/"}]}]`), 0o644)
		_, err := LoadTargets(path)
		if err == nil {
			t.Error("expected validation error")
		}
	})
}

func TestLoadTarget(t *testing.T) {
	dir := t.TempDir()
	data := `{
		"name": "WooCommerce",
		"base_url": "https://shop.example.com",
		"pages": [
			{"name": "Shop", "path": "/shop", "selectors": {"product": ".product-item"}},
			{"name": "Cart", "path": "/cart", "selectors": {"checkout": ".checkout-button"}}
		],
		"tags": ["ecommerce", "woocommerce"],
		"metadata": {"platform": "wordpress"}
	}`

	path := filepath.Join(dir, "woocommerce.json")
	os.WriteFile(path, []byte(data), 0o644)

	target, err := LoadTarget(path)
	if err != nil {
		t.Fatalf("LoadTarget: %v", err)
	}
	if target.Name != "WooCommerce" {
		t.Errorf("Name = %q", target.Name)
	}
	if len(target.Tags) != 2 {
		t.Errorf("Tags = %v", target.Tags)
	}
	if target.Metadata["platform"] != "wordpress" {
		t.Errorf("Metadata = %v", target.Metadata)
	}
}
