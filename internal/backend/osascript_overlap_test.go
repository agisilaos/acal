package backend

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
				items, err := listEventsViaSQLite(context.Background(), path, buildListEventsQuery(EventFilter{From: time.Unix(cocoaEpochOffset+10, 0), To: time.Unix(cocoaEpochOffset+20, 0), Overlap: overlap}), 0)
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
			stubLookupAppleScript(t, fixtureReadRows(t, fmt.Sprintf("uid\tcal\tWork\tEvent\t%d\t%d\t%t\troom\tnote\turl\n", tc.start+cocoaEpochOffset, tc.end+cocoaEpochOffset, tc.allDay)), false)
			fallback, err := NewOsaScriptBackend().listEventsViaAppleScript(context.Background(), EventFilter{From: time.Unix(cocoaEpochOffset+10, 0), To: time.Unix(cocoaEpochOffset+20, 0), Overlap: true})
			if err != nil || (len(fallback) == 1) != tc.overlap {
				t.Fatalf("AppleScript overlap: %+v, %v; want included=%v", fallback, err, tc.overlap)
			}
			items, err := listEventsViaSQLite(context.Background(), path, buildListEventsQuery(EventFilter{From: time.Unix(cocoaEpochOffset+12, 0), To: time.Unix(cocoaEpochOffset+12, 0), Overlap: true}), 0)
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
	marker := stubLookupAppleScript(t, fixtureReadRows(t, output), false)
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
		if !overlap {
			if len(items) != 0 {
				t.Fatalf("start-range selection retained an earlier start: %+v", items)
			}
			continue
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
	items, err := listEventsViaSQLite(context.Background(), path, buildListEventsQuery(EventFilter{From: time.Unix(cocoaEpochOffset+2, 0), To: time.Unix(cocoaEpochOffset+5, 0), Overlap: true, Limit: 1}), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "event-2" {
		t.Fatalf("limit must apply after overlap selection: %+v", items)
	}
}

func TestOverlapFractionalBounds(t *testing.T) {
	path := buildSQLiteFixture(t, 3)
	// event-1=[1,2), event-2=[2,3), event-3=[3,4), relative to Cocoa epoch.
	for _, tc := range []struct {
		name     string
		from, to time.Time
		want     string
	}{
		{"within one second", time.Unix(cocoaEpochOffset+2, 100000000), time.Unix(cocoaEpochOffset+2, 900000000), "event-2"},
		{"fractional upper", time.Unix(cocoaEpochOffset+2, 0), time.Unix(cocoaEpochOffset+3, 100000000), "event-2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := EventFilter{From: tc.from, To: tc.to, Overlap: true, Limit: 1}
			items, err := NewOsaScriptBackend().listEventsFromDB(context.Background(), path, f)
			if err != nil || len(items) != 1 || items[0].Title != tc.want {
				t.Fatalf("SQLite: %+v %v", items, err)
			}
			// Include an out-of-range earlier event to prove filtering precedes limit.
			marker := stubLookupAppleScript(t, fixtureReadRows(t, "one\tcal\tWork\tevent-1\t978307201\t978307202\tfalse\troom\tnote\turl\ntwo\tcal\tWork\tevent-2\t978307202\t978307203\tfalse\troom\tnote\turl\nthree\tcal\tWork\tevent-3\t978307203\t978307204\tfalse\troom\tnote\turl\n"), false)
			items, err = NewOsaScriptBackend().listEventsViaAppleScript(context.Background(), f)
			if err != nil || len(items) != 1 || items[0].Title != tc.want {
				t.Fatalf("AppleScript: %+v %v", items, err)
			}
			args, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(args), strconv.FormatInt(tc.to.Unix()+1, 10)) {
				t.Fatalf("upper bound not rounded outward: %s", args)
			}
			f.Limit = 0
			items, err = NewOsaScriptBackend().listEventsFromDB(context.Background(), path, f)
			want := 1
			if tc.name == "fractional upper" {
				want = 2
			}
			if err != nil || len(items) != want {
				t.Fatalf("SQLite upper edge: %+v %v", items, err)
			}
			items, err = NewOsaScriptBackend().listEventsViaAppleScript(context.Background(), f)
			if err != nil || len(items) != want {
				t.Fatalf("AppleScript upper edge: %+v %v", items, err)
			}
		})
	}
}

func TestOverlapModernNanosecondBounds(t *testing.T) {
	start := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	path := buildSQLiteFixture(t, 1)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE OccurrenceCache SET occurrence_start_date=?, occurrence_end_date=?", start.Unix()-cocoaEpochOffset, start.Unix()-cocoaEpochOffset+60); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	stubLookupAppleScript(t, fixtureReadRows(t, fmt.Sprintf("uid\tcal\tWork\tEvent\t%d\t%d\tfalse\troom\tnote\turl\n", start.Unix(), start.Unix()+60)), false)
	for _, f := range []EventFilter{
		{From: start.Add(-time.Second), To: start.Add(time.Nanosecond), Overlap: true},
		{From: start.Add(time.Nanosecond), To: start.Add(2 * time.Nanosecond), Overlap: true},
	} {
		items, err := NewOsaScriptBackend().listEventsFromDB(context.Background(), path, f)
		if err != nil || len(items) != 1 {
			t.Fatalf("SQLite dropped nanosecond overlap: %+v %v", items, err)
		}
		items, err = NewOsaScriptBackend().listEventsViaAppleScript(context.Background(), f)
		if err != nil || len(items) != 1 {
			t.Fatalf("AppleScript dropped nanosecond overlap: %+v %v", items, err)
		}
	}
}

func TestOverlapSkipsNonpositiveIntervalsBeforeLimit(t *testing.T) {
	for _, end := range []int64{2, 1} {
		t.Run(fmt.Sprintf("end=%d", end), func(t *testing.T) {
			path := buildSQLiteFixture(t, 3)
			db, err := sql.Open("sqlite", "file:"+path)
			if err != nil {
				t.Fatal(err)
			}
			// The invalid event starts inside the window before the real busy event.
			if _, err = db.Exec("DELETE FROM OccurrenceCache WHERE event_id=1"); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("UPDATE OccurrenceCache SET occurrence_end_date=? WHERE event_id=2", end); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			stubLookupAppleScript(t, fixtureReadRows(t, fmt.Sprintf("invalid\tcal\tWork\tevent-2\t%d\t%d\tfalse\troom\tnote\turl\nvalid\tcal\tWork\tevent-3\t%d\t%d\tfalse\troom\tnote\turl\n", cocoaEpochOffset+2, cocoaEpochOffset+end, cocoaEpochOffset+3, cocoaEpochOffset+4)), false)
			for _, overlap := range []bool{false, true} {
				f := EventFilter{From: time.Unix(cocoaEpochOffset, 0), To: time.Unix(cocoaEpochOffset+5, 0), Overlap: overlap, Limit: 1}
				want := "event-2"
				if overlap {
					want = "event-3"
				}
				items, err := NewOsaScriptBackend().listEventsFromDB(context.Background(), path, f)
				if err != nil || len(items) != 1 || items[0].Title != want {
					t.Errorf("SQLite overlap=%v: %+v %v; want %s", overlap, items, err, want)
				}
				items, err = NewOsaScriptBackend().listEventsViaAppleScript(context.Background(), f)
				if err != nil || len(items) != 1 || items[0].Title != want {
					t.Errorf("AppleScript overlap=%v: %+v %v; want %s", overlap, items, err, want)
				}
			}
		})
	}
}
