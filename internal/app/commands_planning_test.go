package app

import (
	"bytes"
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
