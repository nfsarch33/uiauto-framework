package domheal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DriftDetector tracks page content hashes and detects structural changes.
type DriftDetector struct {
	baseDir string
	hashes  map[string]string
	mu      sync.RWMutex
}

// NewDriftDetector creates a drift detector that persists hashes in baseDir.
func NewDriftDetector(baseDir string) *DriftDetector {
	return &DriftDetector{
		baseDir: baseDir,
		hashes:  make(map[string]string),
	}
}

// CheckAndUpdate compares the current HTML content against the stored snapshot.
// Returns true if drift was detected (content changed), false otherwise.
func (dd *DriftDetector) CheckAndUpdate(pageID, html string) (bool, error) {
	hash := simpleHash(html)
	dd.mu.Lock()
	prev, exists := dd.hashes[pageID]
	dd.hashes[pageID] = hash
	dd.mu.Unlock()

	if !exists {
		return false, nil
	}
	return prev != hash, nil
}

// Save persists the current hash map to a JSON file in baseDir.
func (dd *DriftDetector) Save() error {
	if dd.baseDir == "" {
		return nil // In-memory only
	}
	if err := os.MkdirAll(dd.baseDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dd.baseDir, "drift-hashes.json")
	dd.mu.RLock()
	data, _ := json.MarshalIndent(dd.hashes, "", "  ")
	dd.mu.RUnlock()
	return os.WriteFile(path, data, 0644)
}

// Load reads the hash map from the JSON file in baseDir.
func (dd *DriftDetector) Load() error {
	if dd.baseDir == "" {
		return nil
	}
	path := filepath.Join(dd.baseDir, "drift-hashes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // OK if file doesn't exist yet
		}
		return err
	}
	dd.mu.Lock()
	defer dd.mu.Unlock()
	return json.Unmarshal(data, &dd.hashes)
}

// HashCount returns the number of tracked page hashes.
func (dd *DriftDetector) HashCount() int {
	dd.mu.RLock()
	defer dd.mu.RUnlock()
	return len(dd.hashes)
}

func simpleHash(content string) string {
	normalized := strings.TrimSpace(strings.ToLower(content))
	return fmt.Sprintf("%x", len(normalized)*31+hashString(normalized))
}

func hashString(s string) int {
	h := 0
	for _, c := range s {
		h = 31*h + int(c)
	}
	return h
}
