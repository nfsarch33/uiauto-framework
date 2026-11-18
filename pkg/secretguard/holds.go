package secretguard

import (
	"encoding/json"
	"os"
	"sync"
)

// fileHolds is the durable half of the secret hold: a JSON set of open card
// ids on local disk. A restart reopens it and the holds stand; only a
// resolved card (Lift) removes one. The board card is the second source -
// EITHER one saying open keeps capture closed.
type fileHolds struct {
	mu   sync.Mutex
	path string
	open map[string]bool
}

// OpenFileHolds opens (or creates) the hold file at path.
func OpenFileHolds(path string) (*fileHolds, error) {
	h := &fileHolds{path: path, open: map[string]bool{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return h, nil
		}
		return nil, err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &h.open); err != nil {
			return nil, err
		}
	}
	return h, nil
}

func (h *fileHolds) Open(cardID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.open[cardID] = true
	return h.flush()
}

func (h *fileHolds) Lift(cardID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.open, cardID)
	return h.flush()
}

func (h *fileHolds) AnyOpen() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, v := range h.open {
		if v {
			return true
		}
	}
	return false
}

func (h *fileHolds) flush() error {
	b, err := json.Marshal(h.open)
	if err != nil {
		return err
	}
	return os.WriteFile(h.path, b, 0o600)
}
