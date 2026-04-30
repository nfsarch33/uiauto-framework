package domheal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepairLog(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "repair_log.json")
	rl := NewRepairLog(logPath)

	// Read empty log should return error
	_, err := rl.Read()
	if err == nil {
		t.Error("expected error reading non-existent log")
	}

	// Append suggestion
	s1 := RepairSuggestion{
		ElementType: "button",
		OldSelector: "#submit",
		NewSelector: ".btn-submit",
		Confidence:  0.9,
		Method:      "fingerprint",
	}
	if err := rl.Append(s1); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Read log
	entries, err := rl.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].NewSelector != ".btn-submit" {
		t.Errorf("expected .btn-submit, got %s", entries[0].NewSelector)
	}

	// Append another
	s2 := RepairSuggestion{
		ElementType: "input",
		OldSelector: "#email",
		NewSelector: "input[name='email']",
		Confidence:  0.8,
		Method:      "structural",
	}
	if err := rl.Append(s2); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Read log again
	entries, err = rl.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestRepairLog_BadJSON(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "repair_log.json")
	os.WriteFile(logPath, []byte("bad json"), 0644)

	rl := NewRepairLog(logPath)
	_, err := rl.Read()
	if err == nil {
		t.Error("expected error reading bad JSON")
	}

	// Append should overwrite/ignore bad JSON
	s1 := RepairSuggestion{
		ElementType: "button",
	}
	if err := rl.Append(s1); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	entries, err := rl.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after overwriting bad JSON, got %d", len(entries))
	}
}
