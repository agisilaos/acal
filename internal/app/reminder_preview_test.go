package app

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
)

func TestReminderPreview(t *testing.T) {
	for _, mode := range []string{"--json", "--jsonl", "--plain"} {
		for _, clear := range []bool{false, true} {
			operation := "set"
			if clear {
				operation = "clear"
			}
			t.Run(mode+"/"+operation, func(t *testing.T) {
				setupReminderHistory(t)
				if err := writeHistory([]historyEntry{{Type: "add", EventID: "older"}}); err != nil {
					t.Fatal(err)
				}
				if err := writeRedoHistory([]historyEntry{{Type: "add", EventID: "redo"}}); err != nil {
					t.Fatal(err)
				}
				historyBefore, _ := os.ReadFile(historyFilePath())
				redoBefore, _ := os.ReadFile(redoFilePath())
				offset := -30 * time.Minute
				fb := &scopeCaptureBackend{reminder: &offset}
				args := []string{"events", "remind", "review@812538000", "--dry-run", mode}
				if clear {
					args = append(args, "--clear")
				} else {
					args = append(args, "--at=-15m")
				}
				output, err := runReminderHistoryCommand(t, fb, args...)
				if err != nil {
					t.Fatal(err)
				}
				if fb.updateCalls != 0 || fb.remindCalls != 0 {
					t.Fatalf("preview called backend write/read: updates=%d reminders=%d", fb.updateCalls, fb.remindCalls)
				}
				assertReminderOffset(t, fb.reminder, &offset)
				assertHistoryFiles(t, historyBefore, redoBefore)
				data := json.RawMessage(output)
				if mode == "--json" {
					var envelope struct {
						Schema string          `json:"schema_version"`
						Data   json.RawMessage `json:"data"`
						Meta   map[string]any  `json:"meta"`
					}
					if err := json.Unmarshal([]byte(output), &envelope); err != nil {
						t.Fatal(err)
					}
					if envelope.Schema != "v1" || envelope.Meta["dry_run"] != true || envelope.Meta["verified"] != false || envelope.Meta["count"] != float64(1) {
						t.Fatalf("bad preview envelope: %s", output)
					}
					if clear {
						if envelope.Meta["clear_requested"] != true || envelope.Meta["cleared"] != true {
							t.Fatalf("bad clear metadata: %s", output)
						}
					} else if envelope.Meta["offset"] != "-15m0s" {
						t.Fatalf("bad offset metadata: %s", output)
					}
					data = envelope.Data
				}
				var preview struct {
					backend.EventUpdateInput
					DryRun bool `json:"dry_run"`
				}
				if err := json.Unmarshal(data, &preview); err != nil {
					t.Fatal(err)
				}
				if !preview.DryRun || preview.ClearReminder != clear || preview.Scope != backend.ScopeAuto {
					t.Fatalf("bad preview: %s", output)
				}
				if clear {
					assertReminderOffset(t, preview.ReminderOffset, nil)
				} else {
					want := -15 * time.Minute
					assertReminderOffset(t, preview.ReminderOffset, &want)
				}
			})
		}
	}
}
