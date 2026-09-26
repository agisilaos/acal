package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func denseConflictEvents(n int, allDay bool) []contract.Event {
	base := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	items := make([]contract.Event, n)
	for i := range items {
		items[i] = contract.Event{ID: fmt.Sprintf("e%05d", n-i-1), Start: base, End: base.Add(time.Hour), AllDay: allDay}
	}
	return items
}

func runConflicts(t *testing.T, items []contract.Event, flags ...string) (string, string) {
	t.Helper()
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &scopeCaptureBackend{events: items}, nil }
	defer func() { backendFactory = origFactory }()

	cmd := NewRootCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	args := []string{"events", "conflicts", "--from", "2026-02-20", "--to", "2026-02-21", "--tz", "UTC"}
	cmd.SetArgs(append(args, flags...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v: %s", err, stderr.String())
	}
	return stdout.String(), stderr.String()
}

type conflictsResult struct {
	Data []struct {
		LeftID  string `json:"left_id"`
		RightID string `json:"right_id"`
	} `json:"data"`
	Meta struct {
		Count         int  `json:"count"`
		EventsScanned int  `json:"events_scanned"`
		MaxConflicts  int  `json:"max_conflicts"`
		Truncated     bool `json:"truncated"`
	} `json:"meta"`
	Warnings []string `json:"warnings"`
}

func TestEventsConflictsCaps(t *testing.T) {
	for _, tc := range []struct {
		name      string
		n         int
		allDay    bool
		flags     []string
		wantCount int
		wantMax   int
		truncated bool
	}{
		{name: "10K dense default", n: 10000, wantCount: 1000, wantMax: 1000, truncated: true},
		{name: "10K all day", n: 10000, allDay: true, flags: []string{"--include-all-day"}, wantCount: 1000, wantMax: 1000, truncated: true},
		{name: "all day excluded", n: 10000, allDay: true, wantCount: 0, wantMax: 1000},
		{name: "custom ceiling", n: 150, flags: []string{"--max-conflicts", "10000"}, wantCount: 10000, wantMax: 10000, truncated: true},
		{name: "below cap", n: 2, flags: []string{"--max-conflicts", "3"}, wantCount: 1, wantMax: 3},
		{name: "single event", n: 1, flags: []string{"--max-conflicts", "1"}, wantCount: 0, wantMax: 1},
		{name: "no events", n: 0, wantCount: 0, wantMax: 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr := runConflicts(t, denseConflictEvents(tc.n, tc.allDay), append(tc.flags, "--json")...)
			var got conflictsResult
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Data) != tc.wantCount || got.Meta.Count != tc.wantCount {
				t.Fatalf("got %d rows, count=%d; want %d", len(got.Data), got.Meta.Count, tc.wantCount)
			}
			if got.Meta.MaxConflicts != tc.wantMax || got.Meta.Truncated != tc.truncated {
				t.Fatalf("got meta %+v; want max=%d, truncated=%v", got.Meta, tc.wantMax, tc.truncated)
			}
			if got.Meta.EventsScanned != tc.n {
				t.Fatalf("got %d events scanned; want %d", got.Meta.EventsScanned, tc.n)
			}
			if tc.truncated {
				if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "truncated") {
					t.Fatalf("missing truncation warning: %v", got.Warnings)
				}
			} else if len(got.Warnings) != 0 {
				t.Fatalf("unexpected warnings: %v", got.Warnings)
			}
			if stderr != "" {
				t.Fatalf("JSON warning should be in envelope: %s", stderr)
			}
		})
	}
}

func TestEventsConflictsCappedOrderAndValidOverlaps(t *testing.T) {
	items := denseConflictEvents(3, false)
	base := items[0].Start
	// Zero/negative durations and touching endpoints must not consume the cap
	// or cause a false truncation warning after the final valid pair.
	items = append(items,
		contract.Event{ID: "zero", Start: base.Add(30 * time.Minute), End: base.Add(30 * time.Minute)},
		contract.Event{ID: "negative", Start: base.Add(45 * time.Minute), End: base},
		contract.Event{ID: "touching", Start: base.Add(time.Hour), End: base.Add(2 * time.Hour)},
	)
	for _, limit := range []string{"2", "3"} {
		stdout, _ := runConflicts(t, items, "--max-conflicts", limit, "--json")
		var got conflictsResult
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatal(err)
		}
		want := [][2]string{{"e00000", "e00001"}, {"e00000", "e00002"}, {"e00001", "e00002"}}
		if limit == "2" {
			want = want[:2]
		}
		if len(got.Data) != len(want) || got.Meta.Truncated != (limit == "2") {
			t.Fatalf("unexpected capped overlaps: %+v", got)
		}
		for i, pair := range got.Data {
			if pair.LeftID != want[i][0] || pair.RightID != want[i][1] {
				t.Fatalf("pair %d = %+v; want %v", i, pair, want[i])
			}
		}
	}
}

func TestEventsConflictsLargeSparseInput(t *testing.T) {
	items := denseConflictEvents(10000, false)
	for i := range items {
		items[i].Start = items[i].Start.Add(time.Duration(i) * time.Hour)
		items[i].End = items[i].Start.Add(time.Hour)
	}
	stdout, _ := runConflicts(t, items, "--max-conflicts", "1", "--json")
	var got conflictsResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Data) != 0 || got.Meta.Truncated || got.Meta.EventsScanned != 10000 {
		t.Fatalf("unexpected sparse result: %+v", got)
	}
}

func TestEventsConflictsTruncationWarningWithoutEnvelope(t *testing.T) {
	for _, mode := range []string{"--plain", "--jsonl"} {
		t.Run(mode, func(t *testing.T) {
			stdout, stderr := runConflicts(t, denseConflictEvents(3, false), mode, "--max-conflicts", "1", "--quiet")
			if !strings.Contains(stderr, "truncated at --max-conflicts=1") {
				t.Fatalf("missing truncation warning: %s", stderr)
			}
			if strings.Contains(strings.TrimSpace(stdout), "\n") || !json.Valid([]byte(stdout)) {
				t.Fatalf("expected exactly one intact row: %s", stdout)
			}
			_, stderr = runConflicts(t, denseConflictEvents(2, false), mode, "--max-conflicts", "1")
			if stderr != "" {
				t.Fatalf("unexpected warning for exact cap: %s", stderr)
			}
		})
	}
}

func TestEventsConflictsInvalidCapBeforeBackendRead(t *testing.T) {
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = origFactory })
	for _, limit := range []string{"0", "-1", "10001", "2147483647"} {
		t.Run(limit, func(t *testing.T) {
			cmd := NewRootCommand()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"events", "conflicts", "--max-conflicts", limit, "--json"})
			err := cmd.Execute()
			if err == nil || ExitCode(err) != 2 {
				t.Fatalf("expected usage error, got %v", err)
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), "--max-conflicts must be between 1 and 10000") || strings.Contains(stderr.String(), unexpectedBackendCall) {
				t.Fatalf("unexpected output: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
		})
	}
}
