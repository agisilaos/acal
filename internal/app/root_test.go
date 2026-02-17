package app

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
)

func TestResolveEndDuration(t *testing.T) {
	start := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	end, err := resolveEnd("", "30m", start, time.UTC)
	if err != nil {
		t.Fatalf("resolveEnd error: %v", err)
	}
	want := start.Add(30 * time.Minute)
	if !end.Equal(want) {
		t.Fatalf("expected %s, got %s", want, end)
	}
}

func TestResolveEndBothSet(t *testing.T) {
	start := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	if _, err := resolveEnd("2026-02-10T13:00", "30m", start, time.UTC); err == nil {
		t.Fatalf("expected error when both end and duration are set")
	}
}

func TestDayBounds(t *testing.T) {
	loc := time.FixedZone("UTC+2", 2*3600)
	anchor := time.Date(2026, 2, 10, 14, 30, 0, 0, loc)
	start, end := dayBounds(anchor)
	if got, want := start.Format(time.RFC3339), "2026-02-10T00:00:00+02:00"; got != want {
		t.Fatalf("start=%s want=%s", got, want)
	}
	if got, want := end.Format(time.RFC3339), "2026-02-10T23:59:59+02:00"; got != want {
		t.Fatalf("end=%s want=%s", got, want)
	}
}

func TestWeekBoundsMondayStart(t *testing.T) {
	loc := time.UTC
	anchor := time.Date(2026, 2, 11, 9, 0, 0, 0, loc) // Wednesday
	start, end := weekBounds(anchor, time.Monday)
	if got, want := start.Format(time.RFC3339), "2026-02-09T00:00:00Z"; got != want {
		t.Fatalf("start=%s want=%s", got, want)
	}
	if got, want := end.Format(time.RFC3339), "2026-02-15T23:59:59Z"; got != want {
		t.Fatalf("end=%s want=%s", got, want)
	}
}

func TestWeekBoundsSundayStart(t *testing.T) {
	loc := time.UTC
	anchor := time.Date(2026, 2, 11, 9, 0, 0, 0, loc) // Wednesday
	start, end := weekBounds(anchor, time.Sunday)
	if got, want := start.Format(time.RFC3339), "2026-02-08T00:00:00Z"; got != want {
		t.Fatalf("start=%s want=%s", got, want)
	}
	if got, want := end.Format(time.RFC3339), "2026-02-14T23:59:59Z"; got != want {
		t.Fatalf("end=%s want=%s", got, want)
	}
}

func TestMonthBounds(t *testing.T) {
	loc := time.UTC
	anchor := time.Date(2026, 2, 11, 9, 0, 0, 0, loc)
	start, end := monthBounds(anchor)
	if got, want := start.Format(time.RFC3339), "2026-02-01T00:00:00Z"; got != want {
		t.Fatalf("start=%s want=%s", got, want)
	}
	if got, want := end.Format(time.RFC3339), "2026-02-28T23:59:59Z"; got != want {
		t.Fatalf("end=%s want=%s", got, want)
	}
}

func TestParseWeekStart(t *testing.T) {
	wd, err := parseWeekStart("monday")
	if err != nil || wd != time.Monday {
		t.Fatalf("expected monday, got %v err=%v", wd, err)
	}
	wd, err = parseWeekStart("sun")
	if err != nil || wd != time.Sunday {
		t.Fatalf("expected sunday, got %v err=%v", wd, err)
	}
	if _, err := parseWeekStart("fri"); err == nil {
		t.Fatalf("expected error for invalid week start")
	}
}

func TestParseMonthOrDate(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 2, 11, 9, 0, 0, 0, loc)
	ts, err := parseMonthOrDate("2026-03", now, loc)
	if err != nil {
		t.Fatalf("parseMonthOrDate month error: %v", err)
	}
	if got, want := ts.Format(time.RFC3339), "2026-03-01T00:00:00Z"; got != want {
		t.Fatalf("got=%s want=%s", got, want)
	}
	ts, err = parseMonthOrDate("+7d", now, loc)
	if err != nil {
		t.Fatalf("parseMonthOrDate relative error: %v", err)
	}
	if got, want := ts.Format(time.RFC3339), "2026-02-18T00:00:00Z"; got != want {
		t.Fatalf("got=%s want=%s", got, want)
	}
}

func TestParseRecurrenceScope(t *testing.T) {
	tests := []struct {
		in      string
		want    backend.RecurrenceScope
		wantErr bool
	}{
		{in: "", want: backend.ScopeAuto},
		{in: "auto", want: backend.ScopeAuto},
		{in: "this", want: backend.ScopeThis},
		{in: "future", want: backend.ScopeFuture},
		{in: "series", want: backend.ScopeSeries},
		{in: "bad", wantErr: true},
	}
	for _, tc := range tests {
		got, err := parseRecurrenceScope(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseRecurrenceScope(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseRecurrenceScope(%q): unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseRecurrenceScope(%q): got=%q want=%q", tc.in, got, tc.want)
		}
	}
}

func TestPromptConfirmID(t *testing.T) {
	var out bytes.Buffer
	ok, err := promptConfirmID(bytes.NewBufferString("evt-1\n"), &out, "evt-1")
	if err != nil {
		t.Fatalf("promptConfirmID error: %v", err)
	}
	if !ok {
		t.Fatalf("expected confirmation to match")
	}
	if got := out.String(); got != "Type event ID to confirm delete: " {
		t.Fatalf("unexpected prompt output: %q", got)
	}
}

func TestPromptConfirmIDMismatch(t *testing.T) {
	var out bytes.Buffer
	ok, err := promptConfirmID(bytes.NewBufferString("evt-2\n"), &out, "evt-1")
	if err != nil {
		t.Fatalf("promptConfirmID error: %v", err)
	}
	if ok {
		t.Fatalf("expected mismatch")
	}
}

func TestReadTextInputFileAndStdin(t *testing.T) {
	tmp := t.TempDir() + "/note.txt"
	if err := os.WriteFile(tmp, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	got, err := readTextInput(tmp)
	if err != nil {
		t.Fatalf("readTextInput(file) failed: %v", err)
	}
	if got != "hello" {
		t.Fatalf("file content mismatch: %q", got)
	}

	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	_, _ = w.WriteString("stdin text")
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})

	got, err = readTextInput("-")
	if err != nil {
		t.Fatalf("readTextInput(stdin) failed: %v", err)
	}
	if got != "stdin text" {
		t.Fatalf("stdin content mismatch: %q", got)
	}
}

func TestStdinInteractiveFalseForPipe(t *testing.T) {
	orig := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	_ = w.Close()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
	if stdinInteractive() {
		t.Fatalf("expected non-interactive for pipe stdin")
	}
}
