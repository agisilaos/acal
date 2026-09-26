package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestUpdateOutcomeConsumersPreserveStacks(t *testing.T) {
	for _, name := range []string{"update", "move", "batch", "remind", "undo", "redo", "reminder undo", "reminder redo"} {
		for _, applied := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/applied=%v", name, applied), func(t *testing.T) {
				setupReminderHistory(t)
				start := time.Date(2040, 1, 2, 9, 0, 0, 0, time.UTC)
				ev := &contract.Event{ID: "evt@12345", Title: "Original", Start: start, End: start.Add(time.Hour)}
				entry := historyEntry{Type: "update", EventID: ev.ID, Prev: ev, Next: ev}
				if strings.HasPrefix(name, "reminder ") {
					entry = historyEntry{Type: "reminder", EventID: ev.ID, ReminderBefore: &reminderSnapshot{}, ReminderAfter: &reminderSnapshot{}}
				}
				if err := writeHistory([]historyEntry{entry}); err != nil {
					t.Fatal(err)
				}
				if err := writeRedoHistory([]historyEntry{entry}); err != nil {
					t.Fatal(err)
				}
				before, _ := os.ReadFile(historyFilePath())
				redoBefore, _ := os.ReadFile(redoFilePath())
				fb := &scopeCaptureBackend{getEvent: ev, updateErr: &backend.UpdateOutcomeError{Applied: applied, Err: context.DeadlineExceeded}}
				var args []string
				switch name {
				case "update":
					args = []string{"events", "update", ev.ID, "--title", "Changed"}
				case "move":
					args = []string{"events", "move", ev.ID, "--by", "1h"}
				case "remind":
					args = []string{"events", "remind", ev.ID, "--at", "15m"}
				case "batch":
					path := filepath.Join(t.TempDir(), "batch.jsonl")
					if err := os.WriteFile(path, []byte(`{"op":"update","id":"evt@12345","title":"Changed"}`), 0600); err != nil {
						t.Fatal(err)
					}
					args = []string{"events", "batch", "--file", path}
				default:
					args = []string{"history", strings.TrimPrefix(name, "reminder ")}
				}
				original := backendFactory
				backendFactory = func(string) (backend.Backend, error) { return fb, nil }
				cmd := NewRootCommand()
				var output bytes.Buffer
				cmd.SetOut(&output)
				cmd.SetErr(&output)
				cmd.SetArgs(append(args, "--json"))
				err := cmd.Execute()
				backendFactory = original
				out := output.String()
				if ExitCode(err) != 1 || fb.updateCalls != 1 {
					t.Fatalf("err=%v writes=%d output=%s", err, fb.updateCalls, out)
				}
				var body map[string]any
				if err := json.Unmarshal([]byte(out), &body); err != nil {
					t.Fatal(err)
				}
				kind := "update_outcome_unknown"
				if applied {
					kind = "update_applied_unverified"
				}
				if !strings.Contains(out, kind) || !strings.Contains(out, "Inspect Calendar before retrying") || strings.Contains(out, "Retry with") || strings.Contains(out, "Retry command") {
					t.Fatalf("unsafe/missing outcome: %s", out)
				}
				after, _ := os.ReadFile(historyFilePath())
				redoAfter, _ := os.ReadFile(redoFilePath())
				if string(after) != string(before) || string(redoAfter) != string(redoBefore) {
					t.Fatal("unverified write changed history")
				}
			})
		}
	}
}

type movingHistoryBackend struct {
	scopeCaptureBackend
	event contract.Event
	ids   []string
}

func (b *movingHistoryBackend) UpdateEvent(_ context.Context, id string, in backend.EventUpdateInput) (*contract.Event, error) {
	b.ids = append(b.ids, id)
	if id != b.event.ID {
		return nil, fmt.Errorf("wrong occurrence %s; current %s", id, b.event.ID)
	}
	if in.Start != nil {
		b.event.Start = *in.Start
	}
	if in.End != nil {
		b.event.End = *in.End
	}
	b.event.ID = fmt.Sprintf("moving@%d", b.event.Start.Unix()-978307200)
	ev := b.event
	return &ev, nil
}
func TestMovedUpdateUndoRedoUsesReturnedIdentity(t *testing.T) {
	setupReminderHistory(t)
	start := time.Date(2040, 1, 2, 9, 0, 0, 0, time.UTC)
	before := contract.Event{ID: fmt.Sprintf("moving@%d", start.Unix()-978307200), Start: start, End: start.Add(time.Hour)}
	after := before
	after.Start = start.Add(time.Hour)
	after.End = start.Add(2 * time.Hour)
	after.ID = fmt.Sprintf("moving@%d", after.Start.Unix()-978307200)
	// Also cover legacy records whose EventID still names the pre-move occurrence.
	if err := writeHistory([]historyEntry{{Type: "update", EventID: before.ID, Prev: &before, Next: &after}}); err != nil {
		t.Fatal(err)
	}
	fb := &movingHistoryBackend{event: after}
	for _, command := range []string{"undo", "redo", "undo", "redo"} {
		if _, err := runReminderHistoryCommand(t, fb, "history", command, "--json"); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
	if len(fb.ids) != 4 || fb.ids[0] != after.ID || fb.ids[1] != before.ID || fb.event.ID != after.ID {
		t.Fatalf("ids=%v current=%s", fb.ids, fb.event.ID)
	}
}

type canceledUpdateBackend struct{ scopeCaptureBackend }

func (*canceledUpdateBackend) UpdateEvent(ctx context.Context, _ string, _ backend.EventUpdateInput) (*contract.Event, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestUpdateTimeoutHasUnknownOutcome(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := updateEventWithTimeout(ctx, &canceledUpdateBackend{}, "id", backend.EventUpdateInput{})
	meta := backendErrorMeta(err)
	if meta["kind"] != "update_outcome_unknown" || meta["applied"] != nil {
		t.Fatalf("meta=%v err=%v", meta, err)
	}
}
