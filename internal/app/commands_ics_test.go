package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestBuildICSContainsCalendarAndEvent(t *testing.T) {
	items := []contract.Event{{ID: "e1", Title: "Standup", Start: time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 20, 9, 30, 0, 0, time.UTC)}}
	got := buildICS(items)
	if !strings.Contains(got, "BEGIN:VCALENDAR") || !strings.Contains(got, "BEGIN:VEVENT") {
		t.Fatalf("invalid ICS output: %q", got)
	}
}

func TestEventsExportWritesFile(t *testing.T) {
	fb := &scopeCaptureBackend{events: []contract.Event{{ID: "e1", Title: "Standup", Start: time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 20, 9, 30, 0, 0, time.UTC)}}}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	out := filepath.Join(t.TempDir(), "out.ics")
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "export", "--from", "2026-02-20", "--to", "2026-02-21", "--tz", "UTC", "--out", out, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if !strings.Contains(string(raw), "BEGIN:VCALENDAR") {
		t.Fatalf("expected ICS content")
	}
}

func TestEventsExportJSONContainsICS(t *testing.T) {
	fb := &scopeCaptureBackend{events: []contract.Event{{ID: "e1", Title: "Standup", Start: time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 20, 9, 30, 0, 0, time.UTC)}}}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "export", "--from", "2026-02-20", "--to", "2026-02-21", "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var got struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	ics, _ := got.Data["ics"].(string)
	if !strings.Contains(ics, "BEGIN:VCALENDAR") {
		t.Fatalf("expected ICS in json output")
	}
}

func TestParseICS(t *testing.T) {
	raw := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Plan\r\nDTSTART:20260220T090000Z\r\nDTEND:20260220T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	items, warnings := parseICS(raw, "Work", time.UTC)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}
	if items[0].Title != "Plan" || items[0].Calendar != "Work" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestEventsImportDryRun(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "in.ics")
	raw := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Plan\r\nDTSTART:20260220T090000Z\r\nDTEND:20260220T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if err := os.WriteFile(f, []byte(raw), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "import", "--file", f, "--calendar", "Work", "--dry-run", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.addCalls != 0 {
		t.Fatalf("expected no add calls in dry-run")
	}
}
