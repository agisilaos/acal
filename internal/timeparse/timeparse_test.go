package timeparse

import (
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
		{"+7d", "2026-02-15T00:00:00Z"},
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
