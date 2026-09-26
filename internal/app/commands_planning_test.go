package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestBuildBusyBlocksMergesRanges(t *testing.T) {
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	items := []contract.Event{
		{Start: base, End: base.Add(45 * time.Minute)},
		{Start: base.Add(30 * time.Minute), End: base.Add(90 * time.Minute)},
		{Start: base.Add(2 * time.Hour), End: base.Add(3 * time.Hour)},
	}
	blocks := buildBusyBlocks(items, false)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if got := blocks[0].Minutes; got != 90 {
		t.Fatalf("expected first block 90 minutes, got %d", got)
	}
}

func TestSlotsCommandFindsGaps(t *testing.T) {
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	fb := &scopeCaptureBackend{events: []contract.Event{
		{ID: "e1", Start: base, End: base.Add(30 * time.Minute)},
		{ID: "e2", Start: base.Add(60 * time.Minute), End: base.Add(90 * time.Minute)},
	}}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"slots", "--from", "2026-02-20T09:00", "--to", "2026-02-20T12:00", "--between", "09:00-12:00", "--duration", "30m", "--step", "30m", "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var got struct {
		Data []slotRow `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Data) == 0 {
		t.Fatalf("expected at least one slot")
	}
	if got.Data[0].Start.Format(time.RFC3339) != "2026-02-20T09:30:00Z" {
		t.Fatalf("unexpected first slot start: %s", got.Data[0].Start.Format(time.RFC3339))
	}
}

func TestFreebusyCommandJSON(t *testing.T) {
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	fb := &scopeCaptureBackend{events: []contract.Event{
		{ID: "e1", Start: base, End: base.Add(45 * time.Minute)},
		{ID: "e2", Start: base.Add(30 * time.Minute), End: base.Add(90 * time.Minute)},
	}}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"freebusy", "--from", "2026-02-20", "--to", "2026-02-21", "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var got struct {
		Data []busyBlock `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got.Data) != 1 {
		t.Fatalf("expected one merged busy block, got %d", len(got.Data))
	}
	if got.Data[0].Minutes != 90 {
		t.Fatalf("expected 90 merged minutes, got %d", got.Data[0].Minutes)
	}
}

// Record the range sent to the backend without changing the shared test backend.
type slotsFilterBackend struct {
	scopeCaptureBackend
}

func (b *slotsFilterBackend) ListEvents(ctx context.Context, f backend.EventFilter) ([]contract.Event, error) {
	b.lastFilter = f
	return b.scopeCaptureBackend.ListEvents(ctx, f)
}

func TestSlotsCommandResolvedRange(t *testing.T) {
	tests := []struct {
		name       string
		from, to   string
		tz         string
		wantStarts []string
	}{
		{
			name: "same date includes daily window",
			from: "2026-02-20", to: "2026-02-20", tz: "UTC",
			wantStarts: []string{"2026-02-20T09:00:00Z", "2026-02-20T09:30:00Z"},
		},
		{
			name: "multiple dates include terminal day",
			from: "2026-02-20", to: "2026-02-21", tz: "UTC",
			wantStarts: []string{
				"2026-02-20T09:00:00Z", "2026-02-20T09:30:00Z",
				"2026-02-21T09:00:00Z", "2026-02-21T09:30:00Z",
			},
		},
		{
			name: "explicit timestamps clip both ends",
			from: "2026-02-20T09:15", to: "2026-02-20T09:55", tz: "UTC",
			wantStarts: []string{"2026-02-20T09:15:00Z"},
		},
		{
			name: "explicit midnight retains filter expansion",
			from: "2026-02-20T00:00", to: "2026-02-20T00:00", tz: "UTC",
			wantStarts: []string{"2026-02-20T09:00:00Z", "2026-02-20T09:30:00Z"},
		},
		{
			name: "spring DST preserves local daily windows",
			from: "2026-03-28", to: "2026-03-30", tz: "Europe/Berlin",
			wantStarts: []string{
				"2026-03-28T09:00:00+01:00", "2026-03-28T09:30:00+01:00",
				"2026-03-29T09:00:00+02:00", "2026-03-29T09:30:00+02:00",
				"2026-03-30T09:00:00+02:00", "2026-03-30T09:30:00+02:00",
			},
		},
		{
			name: "autumn DST preserves local daily windows",
			from: "2026-10-24", to: "2026-10-26", tz: "Europe/Berlin",
			wantStarts: []string{
				"2026-10-24T09:00:00+02:00", "2026-10-24T09:30:00+02:00",
				"2026-10-25T09:00:00+01:00", "2026-10-25T09:30:00+01:00",
				"2026-10-26T09:00:00+01:00", "2026-10-26T09:30:00+01:00",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb := &slotsFilterBackend{}
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return fb, nil }
			t.Cleanup(func() { backendFactory = origFactory })

			cmd := NewRootCommand()
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"slots", "--from", tt.from, "--to", tt.to, "--between", "09:00-10:00", "--duration", "30m", "--step", "30m", "--limit", "1", "--tz", tt.tz, "--json"})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute failed: %v", err)
			}
			var got struct {
				Data []slotRow `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if len(got.Data) != len(tt.wantStarts) {
				t.Fatalf("got %d slots, want %d: %+v", len(got.Data), len(tt.wantStarts), got.Data)
			}
			for i, slot := range got.Data {
				if start := slot.Start.Format(time.RFC3339); start != tt.wantStarts[i] {
					t.Errorf("slot %d starts at %s, want %s", i, start, tt.wantStarts[i])
				}
				if !slot.End.Equal(slot.Start.Add(30*time.Minute)) || slot.Minutes != 30 {
					t.Errorf("slot %d has unexpected duration: %+v", i, slot)
				}
				if slot.Start.Before(fb.lastFilter.From) || slot.End.After(fb.lastFilter.To) {
					t.Errorf("slot %d falls outside fetched range: %+v, filter: %+v", i, slot, fb.lastFilter)
				}
			}
			if fb.lastFilter.Limit != 1 {
				t.Errorf("backend limit = %d, want 1 (events scanned, not slots returned)", fb.lastFilter.Limit)
			}
			// Slot generation must use exactly the range used to fetch events.
			want := buildSlots(nil, fb.lastFilter.From, fb.lastFilter.To, 9, 0, 10, 0, 30*time.Minute, 30*time.Minute)
			if len(got.Data) != len(want) {
				t.Fatalf("got %d slots, fetched range produces %d", len(got.Data), len(want))
			}
			for i := range want {
				if !got.Data[i].Start.Equal(want[i].Start) || !got.Data[i].End.Equal(want[i].End) {
					t.Errorf("slot %d = %+v, fetched range produces %+v", i, got.Data[i], want[i])
				}
			}
		})
	}
}
