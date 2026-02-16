package app

import (
	"testing"
	"time"

	"github.com/agis/acal/internal/contract"
)

func TestSummarizeEventsByDay(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 2, 9, 0, 0, 0, 0, loc)
	to := time.Date(2026, 2, 11, 23, 59, 59, 0, loc)
	events := []contract.Event{
		{Start: time.Date(2026, 2, 9, 10, 0, 0, 0, loc), AllDay: false},
		{Start: time.Date(2026, 2, 9, 12, 0, 0, 0, loc), AllDay: true},
		{Start: time.Date(2026, 2, 11, 9, 0, 0, 0, loc), AllDay: false},
	}
	rows := summarizeEventsByDay(events, from, to, loc)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Date != "2026-02-09" || rows[0].Total != 2 || rows[0].AllDay != 1 || rows[0].Timed != 1 {
		t.Fatalf("unexpected day 1 summary: %+v", rows[0])
	}
	if rows[1].Date != "2026-02-10" || rows[1].Total != 0 {
		t.Fatalf("unexpected day 2 summary: %+v", rows[1])
	}
	if rows[2].Date != "2026-02-11" || rows[2].Total != 1 {
		t.Fatalf("unexpected day 3 summary: %+v", rows[2])
	}
}
