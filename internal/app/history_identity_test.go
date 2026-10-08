package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

// Model the native boundary with opaque IDs, including fresh IDs after moves.
// Stale IDs always fail, so successful replay cannot silently target an old event.
type identityBackend struct {
	scopeCaptureBackend
	records map[string]contract.Event
	alarms  map[string]*time.Duration
	nextID  int
}

func newIdentityBackend() *identityBackend {
	return &identityBackend{records: map[string]contract.Event{}, alarms: map[string]*time.Duration{}}
}

func (b *identityBackend) ListEvents(context.Context, backend.EventFilter) ([]contract.Event, error) {
	var events []contract.Event
	for _, event := range b.records {
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
	return events, nil
}

func (b *identityBackend) GetEventByID(_ context.Context, id string) (*contract.Event, error) {
	event, ok := b.records[id]
	if !ok {
		return nil, fmt.Errorf("event %q does not exist", id)
	}
	return &event, nil
}

func (b *identityBackend) AddEvent(_ context.Context, in backend.EventCreateInput) (*contract.Event, error) {
	b.nextID++
	event := contract.Event{ID: fmt.Sprintf("native-%d", b.nextID), CalendarID: "calendar-id-" + in.Calendar, CalendarName: in.Calendar, Title: in.Title, Start: in.Start, End: in.End, AllDay: in.AllDay, Location: in.Location, Notes: in.Notes, URL: in.URL}
	b.records[event.ID] = event
	b.alarms[event.ID] = in.ReminderOffset
	return &event, nil
}

func (b *identityBackend) UpdateEvent(ctx context.Context, id string, in backend.EventUpdateInput) (*contract.Event, error) {
	event, err := b.GetEventByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		event.Title = *in.Title
	}
	if in.Start != nil && !event.Start.Equal(*in.Start) {
		event.Start = *in.Start
		b.nextID++
		event.ID = fmt.Sprintf("native-%d", b.nextID)
	}
	if in.End != nil {
		event.End = *in.End
	}
	if in.AllDay != nil {
		event.AllDay = *in.AllDay
	}
	alarm := b.alarms[id]
	if in.ClearReminder {
		alarm = nil
	} else if in.ReminderOffset != nil {
		value := *in.ReminderOffset
		alarm = &value
	}
	delete(b.records, id)
	delete(b.alarms, id)
	b.records[event.ID] = *event
	b.alarms[event.ID] = alarm
	return event, nil
}

func (b *identityBackend) GetReminderOffset(ctx context.Context, id string) (*time.Duration, error) {
	if _, err := b.GetEventByID(ctx, id); err != nil {
		return nil, err
	}
	return b.alarms[id], nil
}

func (b *identityBackend) DeleteEvent(ctx context.Context, id string, _ backend.RecurrenceScope) error {
	if _, err := b.GetEventByID(ctx, id); err != nil {
		return err
	}
	delete(b.records, id)
	delete(b.alarms, id)
	return nil
}

func mutationCommand[T any](t *testing.T, be backend.Backend, args ...string) T {
	t.Helper()
	out, err := runReminderHistoryCommand(t, be, append(args, "--json")...)
	if err != nil {
		t.Fatalf("%v failed: %v\n%s", args, err, out)
	}
	var response struct{ Data T }
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatalf("decode %v: %v\n%s", args, err, out)
	}
	return response.Data
}

func addIdentityEvent(t *testing.T, be backend.Backend, calendar, title string) contract.Event {
	t.Helper()
	return mutationCommand[contract.Event](t, be, "events", "add", "--calendar", calendar, "--title", title, "--start", "2026-10-09T10:00:00Z", "--duration", "1h")
}

func TestHistoryRecreationPreservesUndoRedoChain(t *testing.T) {
	setupReminderHistory(t)
	be := newIdentityBackend()
	addIdentityEvent(t, be, "Unrelated", "Keep me")
	event := addIdentityEvent(t, be, "Work", "Original")
	event = mutationCommand[contract.Event](t, be, "events", "move", event.ID, "--to", "2026-10-10T10:00:00Z")
	mutationCommand[contract.Event](t, be, "events", "remind", event.ID, "--at", "15m")
	event = mutationCommand[contract.Event](t, be, "events", "move", event.ID, "--to", "2026-10-11T10:00:00Z")
	mutationCommand[map[string]any](t, be, "events", "delete", event.ID, "--force")
	for cycle := 0; cycle < 2; cycle++ {
		// Undo deletion, both moves, reminder, and creation; unrelated history stays.
		for i := 0; i < 5; i++ {
			mutationCommand[map[string]any](t, be, "history", "undo")
		}
		events := mutationCommand[[]contract.Event](t, be, "events", "list")
		if len(events) != 1 || events[0].Title != "Keep me" || events[0].CalendarName != "Unrelated" {
			t.Fatalf("after undo: %+v", events)
		}
		for i := 0; i < 4; i++ {
			mutationCommand[map[string]any](t, be, "history", "redo")
		}
		events = mutationCommand[[]contract.Event](t, be, "events", "list")
		var restored *contract.Event
		for _, event := range events {
			if event.CalendarName == "Work" {
				copy := event
				restored = &copy
			}
		}
		if len(events) != 2 || restored == nil || restored.Title != "Original" || restored.Start.UTC().Format(time.RFC3339) != "2026-10-11T10:00:00Z" {
			t.Fatalf("after redo moves: %+v", events)
		}
		mutationCommand[map[string]any](t, be, "history", "redo")
	}
}

func TestHistoryRecreationDoesNotRetargetDifferentCalendar(t *testing.T) {
	setupReminderHistory(t)
	// Legacy storage can reuse an ID across calendars, even calendars with the
	// same display name. Their recorded native calendar IDs keep them distinct.
	stored := `{"type":"add","independent":true,"event_id":"reused","created":{"id":"reused","calendar_id":"calendar-a","calendar_name":"Shared","title":"Other calendar"}}
{"type":"delete","independent":true,"event_id":"reused","deleted":{"id":"reused","calendar_id":"calendar-b","calendar_name":"Shared","title":"Restore me","start":"2026-10-09T10:00:00Z","end":"2026-10-09T11:00:00Z"}}
`
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "acal", "history.jsonl")
	if err := os.WriteFile(path, []byte(stored), 0600); err != nil {
		t.Fatal(err)
	}
	be := newIdentityBackend()
	mutationCommand[map[string]any](t, be, "history", "undo")
	entries := mutationCommand[[]struct {
		EventID string `json:"event_id"`
		Created contract.Event
	}](t, be, "history", "list")
	if len(entries) != 1 || entries[0].EventID != "reused" || entries[0].Created.ID != "reused" || entries[0].Created.CalendarID != "calendar-a" {
		t.Fatalf("unrelated calendar history changed: %+v", entries)
	}
}
