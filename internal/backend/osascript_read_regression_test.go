package backend

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func useCalendarFixture(t *testing.T, dbPath string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "Library", "Calendars")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dbPath, filepath.Join(dir, "Calendar.sqlitedb")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
}

func TestListEventsMatchesUnicodeBeforeLimit(t *testing.T) {
	dbPath := buildSQLiteFixture(t, 3)
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		`UPDATE Calendar SET title = 'Öffentlich'`,
		`INSERT INTO Calendar VALUES (2, 'cal-2', 'Other')`,
		`UPDATE OccurrenceCache SET calendar_id = 2 WHERE event_id = 1`,
		`UPDATE CalendarItem SET summary = 'Ärztetermin 100xyz' WHERE ROWID = 1`,
		`UPDATE CalendarItem SET summary = 'Ärztetermin 100%_\', description = 'ΕΛΛΑΔΑ' WHERE ROWID >= 2`,
		`UPDATE Location SET title = 'Ωmega' WHERE item_owner_id >= 2`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	useCalendarFixture(t, dbPath)
	stubLookupAppleScript(t, "unexpected fallback", true)
	for _, tc := range []struct {
		name, query, field string
		calendars          []string
	}{
		{name: "exact calendar", calendars: []string{"Öffentlich"}},
		{name: "folded calendar", calendars: []string{"öffentlich"}},
		{name: "title and literal LIKE characters", query: `Ärztetermin 100%_\`, field: "title"},
		{name: "folded title", query: `ärztetermin 100%_\`, field: "title"},
		{name: "notes", query: "ελλαδα", field: "notes"},
		{name: "location", query: "ωmega", field: "location"},
		{name: "all fields", query: "ελλαδα", field: "all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, err := NewOsaScriptBackend().ListEvents(context.Background(), EventFilter{
				From: time.Unix(cocoaEpochOffset, 0), To: time.Unix(cocoaEpochOffset+10, 0),
				Query: tc.query, Field: tc.field, Calendars: tc.calendars, Limit: 1,
			})
			if err != nil || len(items) != 1 || items[0].ID != "uid-2@2" {
				t.Fatalf("got events=%+v error=%v; want first matching occurrence uid-2@2", items, err)
			}
		})
	}
}
