package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agis/acal/internal/contract"
)

func TestBatchMovedEventResultCanBeUsedByNextCommand(t *testing.T) {
	setupReminderHistory(t)
	be := newIdentityBackend()
	event := addIdentityEvent(t, be, "Work", "Batch move")
	row, err := json.Marshal(map[string]string{"op": "update", "id": event.ID, "start": "2026-10-11T10:00:00Z", "duration": "1h"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "operations.jsonl")
	if err := os.WriteFile(path, row, 0600); err != nil {
		t.Fatal(err)
	}
	results := mutationCommand[[]struct {
		ID string
		OK bool
	}](t, be, "events", "batch", "--file", path)
	if len(results) != 1 || !results[0].OK {
		t.Fatalf("batch result: %+v", results)
	}
	shown := mutationCommand[contract.Event](t, be, "events", "show", results[0].ID)
	if shown.Title != "Batch move" || shown.Start.UTC().Format(time.RFC3339) != "2026-10-11T10:00:00Z" {
		t.Fatalf("result ID resolved to wrong event: %+v", shown)
	}
	history := mutationCommand[[]struct {
		EventID string `json:"event_id"`
	}](t, be, "history", "list", "--limit", "1")
	if len(history) != 1 || history[0].EventID != shown.ID {
		t.Fatalf("history=%+v; moved event=%s", history, shown.ID)
	}
}
