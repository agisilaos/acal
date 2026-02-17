package app

import (
	"io"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
)

func TestParseRepeatSpecWeekly(t *testing.T) {
	anchor := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	spec, err := parseRepeatSpec("weekly:wed,mon*4", anchor)
	if err != nil {
		t.Fatalf("parseRepeatSpec failed: %v", err)
	}
	if spec.Frequency != "weekly" || spec.Count != 4 {
		t.Fatalf("unexpected spec: %+v", spec)
	}
	if len(spec.Weekdays) != 2 {
		t.Fatalf("expected two weekdays")
	}
	if got := canonicalRepeatRule(spec); got != "weekly:mon,wed*4" {
		t.Fatalf("unexpected canonical rule: %q", got)
	}
}

func TestExpandRepeatDaily(t *testing.T) {
	start := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	spec := repeatSpec{Frequency: "daily", Count: 3}
	rows := expandRepeat(start, spec)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	if rows[2].Format("2006-01-02") != "2026-02-22" {
		t.Fatalf("unexpected third date: %s", rows[2].Format("2006-01-02"))
	}
}

func TestEventsAddRepeatCreatesSeries(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "add", "--calendar", "Work", "--title", "Standup", "--start", "2026-02-20T09:00", "--duration", "30m", "--repeat", "weekly:wed,mon*3", "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.addCalls != 1 {
		t.Fatalf("expected 1 add call, got %d", fb.addCalls)
	}
	if got := fb.addInput.RepeatRule; got != "weekly:mon,wed*3" {
		t.Fatalf("expected repeat rule to be passed, got %q", got)
	}
}

func TestParseRepeatSpecValidationMatrix(t *testing.T) {
	anchor := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		in      string
		wantErr bool
	}{
		{in: "daily*5", wantErr: false},
		{in: "weekly:mon,wed*6", wantErr: false},
		{in: "monthly*3", wantErr: false},
		{in: "yearly*2", wantErr: false},
		{in: "weekly:mon:wed*2", wantErr: true},
		{in: "daily**2", wantErr: true},
		{in: "weekly:mon,tue*0", wantErr: true},
		{in: "weekly:mon,tue*367", wantErr: true},
		{in: "monthly:mon*2", wantErr: true},
		{in: "hourly*2", wantErr: true},
		{in: "*2", wantErr: true},
	}
	for _, tc := range tests {
		_, err := parseRepeatSpec(tc.in, anchor)
		if tc.wantErr && err == nil {
			t.Fatalf("expected error for %q", tc.in)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.in, err)
		}
	}
}
