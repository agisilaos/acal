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

func TestListEventsViaSQLiteReadsRows(t *testing.T) {
	dbPath := buildSQLiteFixture(t, 3)
	q := buildListEventsQuery(EventFilter{From: time.Unix(cocoaEpochOffset+1, 0), To: time.Unix(cocoaEpochOffset+10, 0)})

	items, err := listEventsViaSQLite(context.Background(), dbPath, q, 3)
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
	q := buildListEventsQuery(EventFilter{From: time.Unix(cocoaEpochOffset+1, 0), To: time.Unix(cocoaEpochOffset+1000, 0)})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items, err := listEventsViaSQLite(ctx, dbPath, q, 250)
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

func TestListEventsViaSQLiteSeesExternalCommits(t *testing.T) {
	for _, mode := range []string{"DELETE", "WAL"} {
		t.Run(mode, func(t *testing.T) {
			marker := stubLookupAppleScript(t, "unexpected fallback", true)
			dbPath := buildSQLiteFixture(t, 1)
			writer, err := sql.Open("sqlite", "file:"+dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			writer.SetMaxOpenConns(1)
			var journalMode string
			if err := writer.QueryRow("PRAGMA journal_mode=" + mode).Scan(&journalMode); err != nil {
				t.Fatal(err)
			}
			if !strings.EqualFold(journalMode, mode) {
				t.Fatalf("journal mode: got %q, want %q", journalMode, mode)
			}
			if _, err := writer.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
				t.Fatal(err)
			}
			reader, err := openCalendarReadDB(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				calendarReadDBCache.Delete(calendarSQLiteDSN(dbPath))
				reader.Close()
			})
			b := NewOsaScriptBackend()
			filter := EventFilter{From: time.Unix(cocoaEpochOffset+1, 0), To: time.Unix(cocoaEpochOffset+10, 0)}
			for _, title := range []string{"event-1", "updated", "updated again"} {
				if title != "event-1" {
					// Each Exec commits independently while the cached reader stays open.
					if _, err := writer.Exec("UPDATE CalendarItem SET summary = ? WHERE ROWID = 1", title); err != nil {
						t.Fatal(err)
					}
					if mode == "WAL" {
						info, err := os.Stat(dbPath + "-wal")
						if err != nil || info.Size() <= 32 {
							t.Fatalf("expected uncheckpointed WAL frames: info=%v, error=%v", info, err)
						}
					}
				}
				items, err := b.listEventsFromDB(context.Background(), dbPath, filter)
				if err != nil || len(items) != 1 || items[0].Title != title {
					t.Fatalf("reread: got %v, error=%v; want title %q", items, err, title)
				}
				cached, err := openCalendarReadDB(dbPath)
				if err != nil || cached != reader {
					t.Fatalf("reader was not reused: %v", err)
				}
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("unexpected AppleScript invocation: %v", err)
			}
			if _, err := reader.Exec("UPDATE CalendarItem SET summary = 'forbidden'"); err == nil {
				t.Fatal("read-only handle accepted a write")
			}
		})
	}
}

func TestListEventsFromDBFallsBackOnDeniedSQLiteAccess(t *testing.T) {
	dbPath := buildSQLiteFixture(t, 1)
	if err := os.Chmod(dbPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dbPath, 0600) })
	if file, err := os.Open(dbPath); err == nil {
		file.Close()
		t.Skip("current user can read files without permission bits")
	}
	marker := stubLookupAppleScript(t, "uid-1\tcal-1\tWork\tfallback\t978307201\t978307202\tfalse\troom\tnotes\turl\n", false)
	b := NewOsaScriptBackend()
	items, err := b.listEventsFromDB(context.Background(), dbPath, EventFilter{
		From: time.Unix(cocoaEpochOffset+1, 0), To: time.Unix(cocoaEpochOffset+10, 0),
	})
	if err != nil || len(items) != 1 || items[0].Title != "fallback" {
		t.Fatalf("access-denied fallback: items=%v, error=%v", items, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("AppleScript stub was not called: %v", err)
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
