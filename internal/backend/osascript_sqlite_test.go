package backend

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func TestListEventsViaSQLiteReadsRows(t *testing.T) {
	dbPath := buildSQLiteFixture(t, 3)
	q := buildListEventsQuery(1, 10, EventFilter{})

	items, err := listEventsViaSQLite(context.Background(), dbPath, q)
	if err != nil {
		t.Fatalf("listEventsViaSQLite failed: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("item count mismatch: got=%d want=3", len(items))
	}
	if items[0].Title != "event-1" {
		t.Fatalf("first title mismatch: got=%q want=event-1", items[0].Title)
	}
	if items[2].Location != "room-3" {
		t.Fatalf("third location mismatch: got=%q want=room-3", items[2].Location)
	}
}

func BenchmarkListEventsViaSQLite(b *testing.B) {
	dbPath := buildSQLiteFixture(b, 250)
	q := buildListEventsQuery(1, 1000, EventFilter{})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := listEventsViaSQLite(ctx, dbPath, q)
		if err != nil {
			b.Fatalf("listEventsViaSQLite failed: %v", err)
		}
		if len(items) != 250 {
			b.Fatalf("item count mismatch: got=%d want=250", len(items))
		}
	}
}

func TestOpenCalendarReadDBCachesByPath(t *testing.T) {
	dbPath := buildSQLiteFixture(t, 1)
	db1, err := openCalendarReadDB(dbPath)
	if err != nil {
		t.Fatalf("openCalendarReadDB first call failed: %v", err)
	}
	db2, err := openCalendarReadDB(dbPath)
	if err != nil {
		t.Fatalf("openCalendarReadDB second call failed: %v", err)
	}
	if db1 != db2 {
		t.Fatalf("expected cached database handle reuse")
	}
}

func buildSQLiteFixture(tb testing.TB, rows int) string {
	tb.Helper()
	dir := tb.TempDir()
	dbPath := filepath.Join(dir, "Calendar.sqlitedb")

	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		tb.Fatalf("open fixture db: %v", err)
	}
	defer db.Close()

	schema := []string{
		`CREATE TABLE Calendar (ROWID INTEGER PRIMARY KEY, UUID TEXT, title TEXT)`,
		`CREATE TABLE CalendarItem (
			ROWID INTEGER PRIMARY KEY,
			unique_identifier TEXT,
			UUID TEXT,
			summary TEXT,
			all_day INTEGER,
			description TEXT,
			url TEXT,
			sequence_num INTEGER,
			last_modified INTEGER
		)`,
		`CREATE TABLE OccurrenceCache (
			event_id INTEGER,
			calendar_id INTEGER,
			occurrence_start_date INTEGER,
			occurrence_end_date INTEGER,
			next_reminder_date INTEGER
		)`,
		`CREATE TABLE Location (item_owner_id INTEGER, title TEXT)`,
	}
	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			tb.Fatalf("create schema: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO Calendar (ROWID, UUID, title) VALUES (1, 'cal-1', 'Work')`); err != nil {
		tb.Fatalf("seed calendar: %v", err)
	}

	for i := 1; i <= rows; i++ {
		if _, err := db.Exec(
			`INSERT INTO CalendarItem (ROWID, unique_identifier, UUID, summary, all_day, description, url, sequence_num, last_modified)
			 VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?)`,
			i, fmt.Sprintf("uid-%d", i), fmt.Sprintf("uuid-%d", i), fmt.Sprintf("event-%d", i), fmt.Sprintf("note-%d", i), fmt.Sprintf("https://e/%d", i), i, i+100,
		); err != nil {
			tb.Fatalf("seed calendar item: %v", err)
		}
		if _, err := db.Exec(
			`INSERT INTO OccurrenceCache (event_id, calendar_id, occurrence_start_date, occurrence_end_date, next_reminder_date)
			 VALUES (?, 1, ?, ?, NULL)`,
			i, i, i+1,
		); err != nil {
			tb.Fatalf("seed occurrence cache: %v", err)
		}
		if _, err := db.Exec(`INSERT INTO Location (item_owner_id, title) VALUES (?, ?)`, i, fmt.Sprintf("room-%d", i)); err != nil {
			tb.Fatalf("seed location: %v", err)
		}
	}

	return dbPath
}
