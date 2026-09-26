package timeparse

import (
	"strings"
	"testing"
	"time"
)

func TestParseDateTime(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 2, 8, 15, 0, 0, 0, loc)

	cases := []struct {
		in   string
		want string
	}{
		{"today", "2026-02-08T00:00:00Z"},
		{"tomorrow", "2026-02-09T00:00:00Z"},
		{"yesterday", "2026-02-07T00:00:00Z"},
		{"+7d", "2026-02-15T00:00:00Z"},
		{"-7d", "2026-02-01T00:00:00Z"},
		{"--1d", "2026-02-09T00:00:00Z"},
		{"2026-02-20T10:30:00Z", "2026-02-20T10:30:00Z"},
		{"2026-02-20T10:30", "2026-02-20T10:30:00Z"},
		{"2026-02-20 10:30", "2026-02-20T10:30:00Z"},
		{"2026-02-20", "2026-02-20T00:00:00Z"},
	}

	for _, tc := range cases {
		got, err := ParseDateTime(tc.in, now, loc)
		if err != nil {
			t.Fatalf("ParseDateTime(%q) error: %v", tc.in, err)
		}
		if got.UTC().Format(time.RFC3339) != tc.want {
			t.Fatalf("ParseDateTime(%q) = %s, want %s", tc.in, got.UTC().Format(time.RFC3339), tc.want)
		}
	}
}

func TestParseDateTimeCalendarDaysAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		now, input, want string
	}{
		{"2026-03-29T12:00:00Z", "today", "2026-03-29T00:00:00+01:00"},
		{"2026-03-29T12:00:00Z", "tomorrow", "2026-03-30T00:00:00+02:00"},
		{"2026-03-29T12:00:00Z", "+1d", "2026-03-30T00:00:00+02:00"},
		{"2026-03-28T12:00:00Z", "+2d", "2026-03-30T00:00:00+02:00"},
		{"2026-03-30T12:00:00Z", "yesterday", "2026-03-29T00:00:00+01:00"},
		{"2026-03-30T12:00:00Z", "-1d", "2026-03-29T00:00:00+01:00"},
		{"2026-03-30T12:00:00Z", "-2d", "2026-03-28T00:00:00+01:00"},
		{"2026-10-25T12:00:00Z", "today", "2026-10-25T00:00:00+02:00"},
		{"2026-10-25T12:00:00Z", "tomorrow", "2026-10-26T00:00:00+01:00"},
		{"2026-10-25T12:00:00Z", "+1d", "2026-10-26T00:00:00+01:00"},
		{"2026-10-24T12:00:00Z", "+2d", "2026-10-26T00:00:00+01:00"},
		{"2026-10-26T12:00:00Z", "yesterday", "2026-10-25T00:00:00+02:00"},
		{"2026-10-26T12:00:00Z", "-1d", "2026-10-25T00:00:00+02:00"},
		{"2026-10-26T12:00:00Z", "-2d", "2026-10-24T00:00:00+02:00"},
		{"2026-03-28T23:30:00Z", "+0d", "2026-03-29T00:00:00+01:00"},
		{"2026-10-24T22:30:00Z", "-0d", "2026-10-25T00:00:00+02:00"},
	}
	for _, tc := range cases {
		t.Run(tc.now+"/"+tc.input, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseDateTime(tc.input, now, loc)
			if err != nil {
				t.Fatal(err)
			}
			if got.Format(time.RFC3339) != tc.want || got.Location() != loc {
				t.Fatalf("got %s (%s), want %s in %s", got.Format(time.RFC3339), got.Location(), tc.want, loc)
			}
		})
	}
}

func TestParseRejectsNonexistentWallTimes(t *testing.T) {
	for _, tc := range []struct{ zone, gap, before, after, offset string }{
		{"Europe/Berlin", "2026-03-29T02:30", "2026-03-29T01:30", "2026-03-29T03:30", "2026-03-29T02:30:00+01:00"},
		{"America/New_York", "2026-03-08T02:30", "2026-03-08T01:30", "2026-03-08T03:30", "2026-03-08T02:30:00-05:00"},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range []string{tc.gap, strings.ReplaceAll(tc.gap, "T", " ")} {
				if _, err := ParseDateTime(input, time.Now(), loc); err == nil || !strings.Contains(err.Error(), "nonexistent local time") {
					t.Fatalf("%s: %v", input, err)
				}
			}
			for _, input := range []string{tc.before, tc.after, tc.offset} {
				if _, err := ParseDateTime(input, time.Now(), loc); err != nil {
					t.Fatalf("%s: %v", input, err)
				}
			}
		})
	}
}
