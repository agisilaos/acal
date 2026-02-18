package app

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestParseQuickAddInputBasic(t *testing.T) {
	now := time.Date(2026, 2, 16, 8, 0, 0, 0, time.UTC)
	in, err := parseQuickAddInput("tomorrow 10:00 Standup @Work 30m", now, time.UTC, "", time.Hour, false)
	if err != nil {
		t.Fatalf("parseQuickAddInput error: %v", err)
	}
	if in.Calendar != "Work" {
		t.Fatalf("calendar mismatch: %q", in.Calendar)
	}
	if in.Title != "Standup" {
		t.Fatalf("title mismatch: %q", in.Title)
	}
	if got, want := in.Start.Format(time.RFC3339), "2026-02-17T10:00:00Z"; got != want {
		t.Fatalf("start mismatch: got %s want %s", got, want)
	}
	if got, want := in.End.Format(time.RFC3339), "2026-02-17T10:30:00Z"; got != want {
		t.Fatalf("end mismatch: got %s want %s", got, want)
	}
}

func TestParseQuickAddInputDefaultCalendar(t *testing.T) {
	now := time.Date(2026, 2, 16, 8, 0, 0, 0, time.UTC)
	in, err := parseQuickAddInput("2026-02-18 09:15 Deep Work 45m", now, time.UTC, "Personal", time.Hour, false)
	if err != nil {
		t.Fatalf("parseQuickAddInput error: %v", err)
	}
	if in.Calendar != "Personal" {
		t.Fatalf("calendar mismatch: %q", in.Calendar)
	}
	if in.Title != "Deep Work" {
		t.Fatalf("title mismatch: %q", in.Title)
	}
	if got, want := in.End.Format(time.RFC3339), "2026-02-18T10:00:00Z"; got != want {
		t.Fatalf("end mismatch: got %s want %s", got, want)
	}
}

func TestParseQuickAddInputAllDay(t *testing.T) {
	now := time.Date(2026, 2, 16, 8, 0, 0, 0, time.UTC)
	in, err := parseQuickAddInput("tomorrow Offsite @Work", now, time.UTC, "", time.Hour, true)
	if err != nil {
		t.Fatalf("parseQuickAddInput error: %v", err)
	}
	if !in.AllDay {
		t.Fatalf("expected all-day event")
	}
	if got, want := in.Start.Format(time.RFC3339), "2026-02-17T00:00:00Z"; got != want {
		t.Fatalf("start mismatch: got %s want %s", got, want)
	}
	if got, want := in.End.Format(time.RFC3339), "2026-02-18T00:00:00Z"; got != want {
		t.Fatalf("end mismatch: got %s want %s", got, want)
	}
}

func TestParseQuickAddInputMissingCalendar(t *testing.T) {
	now := time.Date(2026, 2, 16, 8, 0, 0, 0, time.UTC)
	if _, err := parseQuickAddInput("tomorrow 10:00 Standup", now, time.UTC, "", time.Hour, false); err == nil {
		t.Fatalf("expected missing calendar error")
	}
}

func TestParseQuickAddInputMissingTime(t *testing.T) {
	now := time.Date(2026, 2, 16, 8, 0, 0, 0, time.UTC)
	if _, err := parseQuickAddInput("tomorrow Standup @Work", now, time.UTC, "", time.Hour, false); err == nil {
		t.Fatalf("expected missing time error")
	}
}

func TestQuickAddDryRunPlainOutput(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"quick-add", "tomorrow 10:00 Standup @Work 30m", "--dry-run", "--plain"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("quick-add failed: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "dry-run\t") || !strings.Contains(got, "\tWork\tStandup") {
		t.Fatalf("expected readable plain quick-add output, got: %q", got)
	}
}
