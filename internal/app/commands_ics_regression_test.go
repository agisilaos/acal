package app

import (
	"bytes"
	"encoding/json"
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
