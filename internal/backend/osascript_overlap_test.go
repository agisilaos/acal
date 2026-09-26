package backend

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteRangeSelection(t *testing.T) {
	cases := []struct {
		name                    string
		start, end              int64
		allDay, overlap, listed bool
	}{
		{"cross lower", 5, 15, false, true, false},
		{"contains range", 1, 30, false, true, false},
		{"ends at from", 5, 10, false, false, false},
		{"starts at to", 20, 25, false, false, true},
		{"starts at from", 10, 15, false, true, true},
		{"cross upper", 15, 25, false, true, true},
		{"all day", 1, 30, true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := buildSQLiteFixture(t, 1)
			db, err := sql.Open("sqlite", "file:"+path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("UPDATE OccurrenceCache SET occurrence_start_date=?, occurrence_end_date=?", tc.start, tc.end); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("UPDATE CalendarItem SET all_day=?", tc.allDay); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			for _, overlap := range []bool{false, true} {
				items, err := listEventsViaSQLite(context.Background(), path, buildListEventsQuery(10, 20, EventFilter{Overlap: overlap}), 0)
				if err != nil {
					t.Fatal(err)
				}
				want := tc.listed
				if overlap {
					want = tc.overlap
				}
				if (len(items) == 1) != want {
					t.Fatalf("overlap=%v got %d items, want included=%v", overlap, len(items), want)
				}
				if len(items) > 0 && (items[0].Start.Unix() != tc.start+cocoaEpochOffset || items[0].End.Unix() != tc.end+cocoaEpochOffset || items[0].AllDay != tc.allDay) {
					t.Fatalf("event changed: %+v", items[0])
				}
			}
			items, err := listEventsViaSQLite(context.Background(), path, buildListEventsQuery(12, 12, EventFilter{Overlap: true}), 0)
			if err != nil || len(items) != 0 {
				t.Fatalf("empty range: %v, %v", items, err)
			}
		})
	}
}

func TestAppleScriptRangeSelection(t *testing.T) {
	// The runner is stubbed: verify native predicate source and returned full bounds,
	// without reading or mutating the user's Calendar data.
	output := "ongoing\tcal-1\tWork\tOngoing\t978307205\t978307225\ttrue\troom\tnote\turl\n"
	marker := stubLookupAppleScript(t, output, false)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ACAL_TEST_SCRIPT_ARGS\"\n/bin/cat \"$ACAL_TEST_SCRIPT_OUTPUT\"\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(marker), "osascript"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, overlap := range []bool{false, true} {
		f := EventFilter{From: time.Unix(cocoaEpochOffset+10, 0), To: time.Unix(cocoaEpochOffset+20, 0), Overlap: overlap, Limit: 1}
		items, err := NewOsaScriptBackend().listEventsViaAppleScript(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		source, err := os.ReadFile(marker)
		if err != nil {
			t.Fatal(err)
		}
		predicate := "start date >= fromDate and start date <= toDate"
		if overlap {
			predicate = "start date < toDate and end date > fromDate"
		}
		if !strings.Contains(string(source), "every event of c whose "+predicate+")") {
			t.Fatalf("wrong predicate: %s", source)
		}
		if len(items) != 1 || items[0].Start.Unix() != cocoaEpochOffset+5 || items[0].End.Unix() != cocoaEpochOffset+25 || !items[0].AllDay {
			t.Fatalf("event changed: %+v", items)
		}
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	bound := time.Unix(cocoaEpochOffset+10, 0)
	items, err := NewOsaScriptBackend().listEventsViaAppleScript(context.Background(), EventFilter{From: bound, To: bound, Overlap: true})
	if err != nil || len(items) != 0 {
		t.Fatalf("empty range: %v, %v", items, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("empty range invoked AppleScript")
	}
}

func TestSQLiteOverlapLimitAfterSelection(t *testing.T) {
	path := buildSQLiteFixture(t, 4)
	items, err := listEventsViaSQLite(context.Background(), path, buildListEventsQuery(2, 5, EventFilter{Overlap: true, Limit: 1}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "event-2" {
		t.Fatalf("limit must apply after overlap selection: %+v", items)
	}
}
