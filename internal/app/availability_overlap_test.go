package app

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type availabilityBackend struct{ scopeCaptureBackend }

func (b *availabilityBackend) ListEvents(_ context.Context, f backend.EventFilter) ([]contract.Event, error) {
	b.lastFilter = f
	var items []contract.Event
	for _, e := range b.events {
		selected := !e.Start.Before(f.From) && !e.Start.After(f.To)
		if f.Overlap {
			selected = f.From.Before(f.To) && e.Start.Before(f.To) && e.End.After(f.From)
		}
		if selected {
			items = append(items, e)
		}
	}
	if f.Limit > 0 && len(items) > f.Limit {
		items = items[:f.Limit]
	}
	return items, nil
}

func TestAvailabilityOverlapCommands(t *testing.T) {
	base := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	for _, command := range []string{"freebusy", "slots", "conflicts", "list"} {
		for _, mode := range []string{"normal", "empty", "all day excluded", "all day included", "limited"} {
			t.Run(command+"/"+mode, func(t *testing.T) {
				b := &availabilityBackend{scopeCaptureBackend: scopeCaptureBackend{events: []contract.Event{
					{ID: "ongoing", Start: base.Add(-time.Hour), End: base.Add(time.Hour)},
					{ID: "containing", Start: base.Add(-30 * time.Minute), End: base.Add(3 * time.Hour), AllDay: true},
					{ID: "inside", Start: base.Add(30 * time.Minute), End: base.Add(90 * time.Minute)},
					{ID: "at end", Start: base.Add(2 * time.Hour), End: base.Add(3 * time.Hour)},
				}}}
				if mode == "all day excluded" || mode == "all day included" {
					b.events = b.events[1:2]
				}
				original := backendFactory
				backendFactory = func(string) (backend.Backend, error) { return b, nil }
				defer func() { backendFactory = original }()
				args := []string{command}
				if command == "conflicts" || command == "list" {
					args = []string{"events", command}
				}
				to := "2026-02-20T12:00"
				if mode == "empty" {
					to = "2026-02-20T10:00"
				}
				args = append(args, "--from", "2026-02-20T10:00", "--to", to, "--tz", "UTC", "--json")
				if mode == "all day included" && command != "list" {
					args = append(args, "--include-all-day")
				}
				if mode == "limited" {
					args = append(args, "--limit", "1")
				}
				if command == "slots" {
					args = append(args, "--duration", "30m", "--step", "30m", "--between", "10:00-12:00")
				}
				cmd := NewRootCommand()
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&out)
				cmd.SetArgs(args)
				if err := cmd.Execute(); err != nil {
					t.Fatalf("%v: %s", err, out.String())
				}
				var got struct {
					Data []struct {
						ID             string
						Start, End     time.Time
						Minutes        int64
						OverlapStart   time.Time `json:"overlap_start"`
						OverlapEnd     time.Time `json:"overlap_end"`
						OverlapMinutes int64     `json:"overlap_minutes"`
					}
					Meta struct {
						EventsScanned int `json:"events_scanned"`
					}
				}
				if err := json.Unmarshal(out.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if b.lastFilter.Overlap != (command != "list") {
					t.Fatalf("wrong selection mode: %+v", b.lastFilter)
				}
				if mode == "empty" {
					if len(got.Data) != 0 {
						t.Fatalf("nonempty: %s", out.String())
					}
					return
				}
				if command == "list" {
					if mode == "normal" && (len(got.Data) != 2 || got.Data[1].ID != "at end") {
						t.Fatalf("listing changed: %s", out.String())
					}
					return
				}
				scanned := 3
				if mode == "limited" || mode == "all day excluded" || mode == "all day included" {
					scanned = 1
				}
				if got.Meta.EventsScanned != scanned {
					t.Fatalf("scan count: %s", out.String())
				}
				count := 1
				if command == "conflicts" && mode != "normal" {
					count = 0
				}
				if mode == "all day excluded" {
					count = 0
					if command == "slots" {
						count = 4
					}
				}
				if mode == "all day included" && command == "slots" {
					count = 0
				}
				if mode == "limited" && command == "slots" {
					count = 2
				}
				if len(got.Data) != count {
					t.Fatalf("want %d results: %s", count, out.String())
				}
				if count == 0 {
					return
				}
				row := got.Data[0]
				if command == "freebusy" {
					minutes := int64(90)
					if mode == "limited" {
						minutes = 60
					}
					if mode == "all day included" {
						minutes = 120
					}
					if !row.Start.Equal(base) || row.Minutes != minutes {
						t.Fatalf("unclipped busy: %s", out.String())
					}
				}
				if command == "slots" && mode == "normal" && !row.Start.Equal(base.Add(90*time.Minute)) {
					t.Fatalf("ongoing event did not block slot: %s", out.String())
				}
				if command == "conflicts" && (!row.OverlapStart.Equal(base.Add(30*time.Minute)) || row.OverlapMinutes != 30) {
					t.Fatalf("wrong conflict: %s", out.String())
				}
			})
		}
	}
}

func TestClipEventsToRangePreservesSources(t *testing.T) {
	base := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	source := contract.Event{ID: "original", Start: base.Add(-time.Hour), End: base.Add(3 * time.Hour)}
	items := []contract.Event{source}
	clipped := clipEventsToRange(items, base, base.Add(time.Hour))
	if len(clipped) != 1 || clipped[0].ID != source.ID || !clipped[0].Start.Equal(base) || !clipped[0].End.Equal(base.Add(time.Hour)) {
		t.Fatalf("wrong clipping: %+v", clipped)
	}
	if items[0] != source {
		t.Fatal("source mutated")
	}
}

func TestConflictsClipBothEndpoints(t *testing.T) {
	base := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	items := []contract.Event{
		{ID: "a", Start: base.Add(-2 * time.Hour), End: base.Add(4 * time.Hour)},
		{ID: "b", Start: base.Add(-time.Hour), End: base.Add(3 * time.Hour)},
	}
	stdout, _ := runConflicts(t, items, "--from", "2026-02-20T10:00", "--to", "2026-02-20T12:00", "--json")
	var got struct {
		Data []struct {
			LeftID  string    `json:"left_id"`
			RightID string    `json:"right_id"`
			Start   time.Time `json:"overlap_start"`
			End     time.Time `json:"overlap_end"`
			Minutes int64     `json:"overlap_minutes"`
		}
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one conflict: %s", stdout)
	}
	row := got.Data[0]
	if row.LeftID != "a" || row.RightID != "b" || !row.Start.Equal(base) || !row.End.Equal(base.Add(2*time.Hour)) || row.Minutes != 120 {
		t.Fatalf("incorrect clipped conflict: %s", stdout)
	}
}
