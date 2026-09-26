package backend

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/contract"
)

func TestParseReadEventID(t *testing.T) {
	for _, tc := range []struct {
		id, uid        string
		occurrence     int64
		present, valid bool
	}{
		{"uid@123", "uid", 123, true, true},
		{"uid@domain@-123", "uid@domain", -123, true, true},
		{"uid@0", "uid", 0, true, true},
		{"uid", "uid", 0, false, false},
		{"", "", 0, false, false},
		{"uid@", "uid", 0, true, false},
		{"uid@bad", "uid", 0, true, false},
		{"uid@1.5", "uid", 0, true, false},
		{"@1", "", 1, true, false},
		{"uid@+1", "uid", 1, true, false},
		{"uid@01", "uid", 1, true, false},
		{"uid@-0", "uid", 0, true, false},
		{"uid@1 ", "uid", 0, true, false},
		{"uid@9223372036854775808", "uid", math.MaxInt64, true, false},
	} {
		t.Run(tc.id, func(t *testing.T) {
			uid, occurrence, present, valid := parseReadEventID(tc.id)
			if uid != tc.uid || occurrence != tc.occurrence || present != tc.present || valid != tc.valid {
				t.Fatalf("got (%q, %d, %v, %v), want (%q, %d, %v, %v)", uid, occurrence, present, valid, tc.uid, tc.occurrence, tc.present, tc.valid)
			}
		})
	}
}

func TestGetEventByIDRejectsInvalidIDsWithoutListing(t *testing.T) {
	for _, id := range []string{"", "uid", "uid@", "uid@bad", "uid@1.5", "@1", "uid@+1", "uid@01", "uid@-0", "uid@1 ", "uid@9223372036854775808", "uid@-9223372036854775808", "uid@9223372036854775807"} {
		t.Run(id, func(t *testing.T) {
			event, err := getEventByID(context.Background(), id, func(context.Context, EventFilter) ([]contract.Event, error) {
				t.Fatal("invalid ID must not list events")
				return nil, nil
			})
			if event != nil || err == nil || err.Error() != "event not found" {
				t.Fatalf("got event=%v, error=%v", event, err)
			}
		})
	}
}

func TestGetEventByIDUsesBoundedWindowAndExactMatch(t *testing.T) {
	zeroUnix := time.Time{}.Unix()
	for _, occurrence := range []int64{0, 1, -1, 1234567890, -1234567890, zeroUnix - cocoaEpochOffset + 1, zeroUnix - cocoaEpochOffset - 1, math.MinInt64 + 1, math.MaxInt64 - cocoaEpochOffset - 1} {
		t.Run(strconv.FormatInt(occurrence, 10), func(t *testing.T) {
			id := fmt.Sprintf("uid@domain@%d", occurrence)
			event, err := getEventByID(context.Background(), id, func(ctx context.Context, f EventFilter) ([]contract.Event, error) {
				from := occurrence + cocoaEpochOffset - 1
				if from == zeroUnix {
					from--
				}
				to := occurrence + cocoaEpochOffset + 1
				if to == zeroUnix {
					to++
				}
				if f.From.IsZero() || f.To.IsZero() || f.From.Unix() != from || f.To.Unix() != to || f.Limit != 0 || f.Overlap {
					t.Fatalf("unexpected filter: %+v", f)
				}
				return []contract.Event{
					{ID: fmt.Sprintf("uid@domain@%d", occurrence-1)},
					{ID: fmt.Sprintf("unrelated@%d", occurrence)},
					{ID: id, Title: "target"},
					{ID: fmt.Sprintf("uid@domain@%d", occurrence+1)},
				}, nil
			})
			if err != nil || event == nil || event.ID != id || event.Title != "target" {
				t.Fatalf("got event=%v, error=%v", event, err)
			}
		})
	}
}

func TestGetEventByIDSQLiteOccurrences(t *testing.T) {
	stubLookupAppleScript(t, "unexpected fallback", true)
	now := time.Now()
	for _, tc := range []struct {
		name  string
		start float64
	}{
		{"far past", float64(now.AddDate(-10, 0, 0).Unix() - cocoaEpochOffset)},
		{"far future", float64(now.AddDate(10, 0, 0).Unix() - cocoaEpochOffset)},
		{"positive fraction", 42.75},
		{"negative fraction", -42.75},
		{"zero", 0},
		{"positive fraction at zero", 0.75},
		{"negative fraction at zero", -0.75},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbPath := buildSQLiteFixture(t, 0)
			db, err := sql.Open("sqlite", "file:"+dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			for _, item := range []struct {
				uid   string
				start float64
			}{
				{"uid@domain", float64(int64(tc.start) - 1)},
				{"other", tc.start},
				{"uid@domain", tc.start},
				{"uid@domain", float64(int64(tc.start) + 1)},
			} {
				result, err := db.Exec(`INSERT INTO CalendarItem (unique_identifier, summary) VALUES (?, ?)`, item.uid, fmt.Sprintf("start=%g", item.start))
				if err != nil {
					t.Fatal(err)
				}
				rowID, err := result.LastInsertId()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO OccurrenceCache (event_id, calendar_id, occurrence_start_date, occurrence_end_date) VALUES (?, 1, ?, ?)`, rowID, item.start, item.start+60); err != nil {
					t.Fatal(err)
				}
			}
			// Finish fixture writes before opening the production immutable reader.
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			b := NewOsaScriptBackend()
			list := func(ctx context.Context, f EventFilter) ([]contract.Event, error) {
				return b.listEventsFromDB(ctx, dbPath, f)
			}
			id := fmt.Sprintf("uid@domain@%d", int64(tc.start))
			event, err := getEventByID(context.Background(), id, list)
			if err != nil || event == nil || event.ID != id || event.Title != fmt.Sprintf("start=%g", tc.start) {
				t.Fatalf("got event=%v, error=%v", event, err)
			}
			event, err = getEventByID(context.Background(), fmt.Sprintf("absent@%d", int64(tc.start)), list)
			if event != nil || err == nil || err.Error() != "event not found" {
				t.Fatalf("absent ID: event=%v, error=%v", event, err)
			}
		})
	}
}

func TestGetEventByIDAppleScriptFallback(t *testing.T) {
	for _, occurrence := range []int64{-42, 0, 42, time.Now().AddDate(10, 0, 0).Unix() - cocoaEpochOffset} {
		t.Run(strconv.FormatInt(occurrence, 10), func(t *testing.T) {
			startUnix := occurrence + cocoaEpochOffset
			row := func(uid string, start int64) string {
				return fmt.Sprintf("%s\tcal-1\tWork\ttarget\t%d\t%d\tfalse\tRoom\tnotes\turl\n", uid, start, start+60)
			}
			marker := stubLookupAppleScript(t, row("uid@domain", startUnix-1)+row("other", startUnix)+row("uid@domain", startUnix)+row("uid@domain", startUnix+1), false)
			// An empty file opens as SQLite but its missing tables force fallback.
			dbPath := filepath.Join(t.TempDir(), "Calendar.sqlitedb")
			if err := os.WriteFile(dbPath, nil, 0600); err != nil {
				t.Fatal(err)
			}
			b := NewOsaScriptBackend()
			list := func(ctx context.Context, f EventFilter) ([]contract.Event, error) {
				return b.listEventsFromDB(ctx, dbPath, f)
			}
			id := fmt.Sprintf("uid@domain@%d", occurrence)
			event, err := getEventByID(context.Background(), id, list)
			if err != nil || event == nil || event.ID != id {
				t.Fatalf("got event=%v, error=%v", event, err)
			}
			args, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			wantArgs := fmt.Sprintf("%d\n%d\n", startUnix-1, startUnix+1)
			if string(args) != wantArgs {
				t.Fatalf("fallback window: got %q, want %q", args, wantArgs)
			}
			event, err = getEventByID(context.Background(), fmt.Sprintf("absent@%d", occurrence), list)
			if event != nil || err == nil || err.Error() != "event not found" {
				t.Fatalf("absent ID: event=%v, error=%v", event, err)
			}
		})
	}
}

func TestGetEventByIDDoesNotFallbackOnEmptyOrCanceledSQLite(t *testing.T) {
	marker := stubLookupAppleScript(t, "", true)
	dbPath := buildSQLiteFixture(t, 0)
	b := NewOsaScriptBackend()
	list := func(ctx context.Context, f EventFilter) ([]contract.Event, error) {
		return b.listEventsFromDB(ctx, dbPath, f)
	}
	event, err := getEventByID(context.Background(), "uid@42", list)
	if event != nil || err == nil || err.Error() != "event not found" {
		t.Fatalf("empty SQLite: event=%v, error=%v", event, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := getEventByID(ctx, "uid@42", list); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled SQLite: %v", err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := getEventByID(ctx, "uid@42", list); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired SQLite: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected AppleScript invocation: %v", err)
	}
}

func TestGetEventByIDPropagatesFallbackFailure(t *testing.T) {
	stubLookupAppleScript(t, "fallback unavailable", true)
	b := NewOsaScriptBackend()
	_, err := getEventByID(context.Background(), "uid@42", func(ctx context.Context, f EventFilter) ([]contract.Event, error) {
		return b.listEventsFromDB(ctx, filepath.Join(t.TempDir(), "missing.db"), f)
	})
	if err == nil || !strings.Contains(err.Error(), "fallback failed") || !strings.Contains(err.Error(), "fallback unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func stubLookupAppleScript(t *testing.T, output string, fail bool) string {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "args")
	outputPath := filepath.Join(dir, "output")
	if err := os.WriteFile(outputPath, []byte(output), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACAL_TEST_SCRIPT_ARGS", marker)
	t.Setenv("ACAL_TEST_SCRIPT_OUTPUT", outputPath)
	t.Setenv("ACAL_OSASCRIPT_RETRIES", "0")
	script := "#!/bin/sh\nwhile [ \"$#\" -gt 2 ]; do shift; done\nprintf '%s\\n' \"$@\" > \"$ACAL_TEST_SCRIPT_ARGS\"\n/bin/cat \"$ACAL_TEST_SCRIPT_OUTPUT\"\n"
	if fail {
		script += "exit 1\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return marker
}

func TestGetEventByIDFallbackWithEmptyOptionalFields(t *testing.T) {
	start := time.Unix(cocoaEpochOffset+42, 0)
	stubLookupAppleScript(t, fmt.Sprintf("uid\tcal\tWork\tquote \"literal\"\t%d\t%d\tfalse\t\t\t\n", start.Unix(), start.Add(time.Hour).Unix()), false)
	b := NewOsaScriptBackend()
	event, err := getEventByID(context.Background(), "uid@42", b.listEventsViaAppleScript)
	if err != nil || event == nil || event.Title != `quote "literal"` || event.Location != "" || event.Notes != "" || event.URL != "" {
		t.Fatalf("event=%+v error=%v", event, err)
	}
}
