package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func previewICSImport(t *testing.T, raw string) []backend.EventCreateInput {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	originalFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &scopeCaptureBackend{}, nil }
	t.Cleanup(func() { backendFactory = originalFactory })
	path := filepath.Join(t.TempDir(), "events.ics")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "import", "--file", path, "--calendar", "Work", "--dry-run", "--strict", "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("import preview: %v; output: %s", err, out.String())
	}
	var response struct {
		Data []backend.EventCreateInput `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data
}

func TestEventsImportPreservesEventTextWithAlarms(t *testing.T) {
	raw := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\n" +
		"DTSTART:20261101T090000Z\r\nDTEND:20261101T100000Z\r\n" +
		"SUMMARY:Project review\r\nDESCRIPTION:Bring the project plan\r\n" +
		"BEGIN:VALARM\r\nACTION:EMAIL\r\nTRIGGER:-PT15M\r\n" +
		"ATTENDEE:mailto:review@example.com\r\nSUMMARY:Reminder subject\r\n" +
		"DESCRIPTION:Reminder notification\r\nEND:VALARM\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	items := previewICSImport(t, raw)
	if len(items) != 1 {
		t.Fatalf("got %d events, want 1", len(items))
	}
	if items[0].Title != "Project review" || items[0].Notes != "Bring the project plan" {
		t.Fatalf("event text replaced by alarm: title=%q notes=%q", items[0].Title, items[0].Notes)
	}
}

func TestEventsExportImportPreservesURL(t *testing.T) {
	const originalURL = "https://example.com/a;b?x=1,2"
	originalFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) {
		return &scopeCaptureBackend{events: []contract.Event{{
			ID: "url-event", Title: "Review", URL: originalURL,
			Start: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
			End:   time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
		}}}, nil
	}
	t.Cleanup(func() { backendFactory = originalFactory })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "export.ics")
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "export", "--from", "2026-11-01", "--to", "2026-11-01", "--tz", "UTC", "--out", path})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	items := previewICSImport(t, string(raw))
	if len(items) != 1 || items[0].URL != originalURL {
		t.Fatalf("export/import changed URL: %+v", items)
	}
}

func TestEventsImportPreservesTextWithQuotedALTREP(t *testing.T) {
	raw := "BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\n" +
		"DTSTART:20261101T090000Z\nDTEND:20261101T100000Z\n" +
		"SUMMARY;ALTREP=\"https://example.test/title\":Project review\n" +
		"LOCATION;ALTREP=\"https://example.test/room\":Meeting room\n" +
		"DESCRIPTION;ALTREP=\"https://example.test:8443/agenda\":Bring the project plan\n" +
		"END:VEVENT\nEND:VCALENDAR\n"
	items := previewICSImport(t, raw)
	if len(items) != 1 {
		t.Fatalf("got %d events, want 1", len(items))
	}
	if items[0].Title != "Project review" || items[0].Location != "Meeting room" || items[0].Notes != "Bring the project plan" {
		t.Fatalf("quoted parameter changed event text: %+v", items[0])
	}
}

func TestEventsImportResolvesDSTUsingICalendarRules(t *testing.T) {
	// RFC 5545 section 3.3.5 selects the first occurrence of a repeated
	// local time and the pre-transition offset for a nonexistent local time.
	for _, tc := range []struct {
		name, zone, start, end, wantStart, wantEnd string
	}{
		{"Berlin fold", "Europe/Berlin", "20261025T023000", "20261025T033000", "2026-10-25T00:30:00Z", "2026-10-25T02:30:00Z"},
		{"New York gap", "America/New_York", "20260308T023000", "20260308T040000", "2026-03-08T07:30:00Z", "2026-03-08T08:00:00Z"},
		{"Berlin gap", "Europe/Berlin", "20260329T023000", "20260329T040000", "2026-03-29T01:30:00Z", "2026-03-29T02:00:00Z"},
		{"New York fold", "America/New_York", "20261101T013000", "20261101T030000", "2026-11-01T05:30:00Z", "2026-11-01T08:00:00Z"},
		{"half hour fold", "Australia/Lord_Howe", "20260405T014500", "20260405T023000", "2026-04-04T14:45:00Z", "2026-04-04T16:00:00Z"},
		{"half hour gap", "Australia/Lord_Howe", "20261004T021500", "20261004T030000", "2026-10-03T15:45:00Z", "2026-10-03T16:00:00Z"},
		{"skipped date", "Pacific/Apia", "20111230T120000", "20111231T130000", "2011-12-30T22:00:00Z", "2011-12-30T23:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf("BEGIN:VCALENDAR\nVERSION:2.0\nBEGIN:VEVENT\nDTSTART;TZID=%s:%s\nDTEND;TZID=%s:%s\nEND:VEVENT\nEND:VCALENDAR\n", tc.zone, tc.start, tc.zone, tc.end)
			items := previewICSImport(t, raw)
			if len(items) != 1 {
				t.Fatalf("got %d events, want 1", len(items))
			}
			if start, end := items[0].Start.UTC().Format(time.RFC3339), items[0].End.UTC().Format(time.RFC3339); start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("imported interval %s to %s, want %s to %s", start, end, tc.wantStart, tc.wantEnd)
			}
		})
	}
}
