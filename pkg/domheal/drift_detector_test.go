package domheal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDriftDetector(t *testing.T) {
	tempDir := t.TempDir()
	dd := NewDriftDetector(tempDir)

	// CheckAndUpdate new page
	drift, err := dd.CheckAndUpdate("page1", "<html><body>Hello</body></html>")
	if err != nil {
		t.Fatalf("CheckAndUpdate failed: %v", err)
	}
	if drift {
		t.Error("expected no drift for new page")
	}

	// CheckAndUpdate same content
	drift, err = dd.CheckAndUpdate("page1", "<html><body>Hello</body></html>")
	if err != nil {
		t.Fatalf("CheckAndUpdate failed: %v", err)
	}
	if drift {
		t.Error("expected no drift for same content")
	}

	// CheckAndUpdate changed content
	drift, err = dd.CheckAndUpdate("page1", "<html><body>World</body></html>")
	if err != nil {
		t.Fatalf("CheckAndUpdate failed: %v", err)
	}
	if !drift {
		t.Error("expected drift for changed content")
	}

	// Save and Load
	if err := dd.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	dd2 := NewDriftDetector(tempDir)
	if err := dd2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if dd2.HashCount() != 1 {
		t.Errorf("expected 1 hash, got %d", dd2.HashCount())
	}

	// CheckAndUpdate with loaded detector
	drift, err = dd2.CheckAndUpdate("page1", "<html><body>World</body></html>")
	if err != nil {
		t.Fatalf("CheckAndUpdate failed: %v", err)
	}
	if drift {
		t.Error("expected no drift for loaded same content")
	}
}

func TestDriftDetector_InMemory(t *testing.T) {
	dd := NewDriftDetector("")
	if err := dd.Save(); err != nil {
		t.Errorf("Save should not fail for in-memory detector: %v", err)
	}
	if err := dd.Load(); err != nil {
		t.Errorf("Load should not fail for in-memory detector: %v", err)
	}
}

func TestDriftDetector_LoadNotExist(t *testing.T) {
	tempDir := t.TempDir()
	dd := NewDriftDetector(tempDir)
	if err := dd.Load(); err != nil {
		t.Errorf("Load should ignore not exist error: %v", err)
	}
}

func TestDriftDetector_SaveError(t *testing.T) {
	tempDir := t.TempDir()
	// Create a file where the directory should be to force MkdirAll to fail
	badPath := filepath.Join(tempDir, "bad")
	os.WriteFile(badPath, []byte("test"), 0644)

	dd := NewDriftDetector(badPath)
	err := dd.Save()
	if err == nil {
		t.Error("expected Save to fail when baseDir cannot be created")
	}
}

func TestDriftDetector_LoadErrors(t *testing.T) {
	tempDir := t.TempDir()
	dd := NewDriftDetector(tempDir)

	// Bad JSON
	path := filepath.Join(tempDir, "drift-hashes.json")
	os.WriteFile(path, []byte("bad json"), 0644)
	if err := dd.Load(); err == nil {
		t.Error("expected Load to fail on bad JSON")
	}

	// Permission denied / is directory
	os.Remove(path)
	os.MkdirAll(path, 0755)
	if err := dd.Load(); err == nil {
		t.Error("expected Load to fail on directory")
	}
}
