package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/agis/acal/internal/backend"
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
