package backend

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestGetEventPreservesLiteralText(t *testing.T) {
	dbPath := buildSQLiteFixture(t, 1)
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const title = "  Review\t\n"
	const location = "\n  Room 4\t"
	const notes = "  Keep indentation\nsecond line\n"
	const url = " https://example.com/path "
	const calendar = "  Work  "
	if _, err := db.Exec(`UPDATE CalendarItem SET summary = ?, description = ?, url = ?`, title, notes, url); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE Location SET title = ?`, location); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE Calendar SET title = ?`, calendar); err != nil {
		t.Fatal(err)
	}
	useCalendarFixture(t, dbPath)
	stubLookupAppleScript(t, "unexpected fallback", true)
	event, err := NewOsaScriptBackend().GetEventByID(context.Background(), "uid-1@1")
	if err != nil {
		t.Fatal(err)
	}
	if event.Title != title || event.Location != location || event.Notes != notes || event.URL != url || event.CalendarName != calendar {
		t.Fatalf("event text changed during lookup: %+v", event)
	}
}

func TestListEventsHonorsFractionalStartBeforeLimit(t *testing.T) {
	start := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for _, source := range []string{"SQLite", "AppleScript fallback"} {
		t.Run(source, func(t *testing.T) {
			dbPath := buildSQLiteFixture(t, 3)
			db, err := sql.Open("sqlite", "file:"+dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`UPDATE OccurrenceCache SET occurrence_start_date = ? + event_id - 1, occurrence_end_date = ? + event_id`, start.Unix()-cocoaEpochOffset, start.Unix()-cocoaEpochOffset); err != nil {
				t.Fatal(err)
			}
			if source == "AppleScript fallback" {
				dbPath = filepath.Join(t.TempDir(), "empty.db")
				if err := os.WriteFile(dbPath, nil, 0600); err != nil {
					t.Fatal(err)
				}
				var rows strings.Builder
				for i := 1; i <= 3; i++ {
					fmt.Fprintf(&rows, "uid-%d\tcal-1\tWork\tEvent\t%d\t%d\tfalse\t\t\t\n", i, start.Unix()+int64(i-1), start.Unix()+int64(i))
				}
				stubLookupAppleScript(t, fixtureReadRows(t, rows.String()), false)
			} else {
				stubLookupAppleScript(t, "unexpected fallback", true)
			}
			useCalendarFixture(t, dbPath)
			for _, offset := range []time.Duration{time.Nanosecond, 500 * time.Millisecond} {
				items, err := NewOsaScriptBackend().ListEvents(context.Background(), EventFilter{
					From: start.Add(offset), To: start.Add(2 * time.Second), Limit: 1,
				})
				if err != nil || len(items) != 1 || items[0].ID != "uid-2@812030401" {
					t.Errorf("from offset %s: got events=%+v error=%v; want uid-2@812030401", offset, items, err)
				}
			}
		})
	}
}
