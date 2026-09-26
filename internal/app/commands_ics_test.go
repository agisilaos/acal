package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestBuildICSContainsCalendarAndEvent(t *testing.T) {
	items := []contract.Event{{ID: "e1", Title: "Standup", Start: time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 20, 9, 30, 0, 0, time.UTC)}}
	got := buildICS(items)
	if !strings.Contains(got, "BEGIN:VCALENDAR") || !strings.Contains(got, "BEGIN:VEVENT") {
		t.Fatalf("invalid ICS output: %q", got)
	}
}

func TestEventsExportWritesFile(t *testing.T) {
	fb := &scopeCaptureBackend{events: []contract.Event{{ID: "e1", Title: "Standup", Start: time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 20, 9, 30, 0, 0, time.UTC)}}}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	out := filepath.Join(t.TempDir(), "out.ics")
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "export", "--from", "2026-02-20", "--to", "2026-02-21", "--tz", "UTC", "--out", out, "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	if !strings.Contains(string(raw), "BEGIN:VCALENDAR") {
		t.Fatalf("expected ICS content")
	}
}

func TestEventsExportJSONContainsICS(t *testing.T) {
	fb := &scopeCaptureBackend{events: []contract.Event{{ID: "e1", Title: "Standup", Start: time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 2, 20, 9, 30, 0, 0, time.UTC)}}}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "export", "--from", "2026-02-20", "--to", "2026-02-21", "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var got struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	ics, _ := got.Data["ics"].(string)
	if !strings.Contains(ics, "BEGIN:VCALENDAR") {
		t.Fatalf("expected ICS in json output")
	}
}

func TestParseICS(t *testing.T) {
	raw := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Plan\r\nDTSTART:20260220T090000Z\r\nDTEND:20260220T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	items, warnings := parseICS(raw, "Work", time.UTC)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}
	if items[0].Title != "Plan" || items[0].Calendar != "Work" {
		t.Fatalf("unexpected item: %+v", items[0])
	}
}

func TestEventsImportDryRun(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "in.ics")
	raw := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Plan\r\nDTSTART:20260220T090000Z\r\nDTEND:20260220T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if err := os.WriteFile(f, []byte(raw), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "import", "--file", f, "--calendar", "Work", "--dry-run", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.addCalls != 0 {
		t.Fatalf("expected no add calls in dry-run")
	}
}

func TestEventsImportDryRunControlCharacters(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	const value = "Café 東京\x1b]52;c;payload\a\r\t\x00\b\x7f\u009b2J\u009dtext\u009c end"
	const escaped = `Café 東京\u001b]52;c;payload\u0007\r\t\u0000\u0008\u007f\u009b2J\u009dtext\u009c end`
	raw := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\n" +
		"SUMMARY:" + value + "\r\nLOCATION:" + value + "\r\n" +
		"DESCRIPTION:" + value + "\r\nURL:" + value + "\r\n" +
		"DTSTART:20260220T090000Z\r\nDTEND:20260220T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	f := filepath.Join(t.TempDir(), "controls.ics")
	if err := os.WriteFile(f, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"--plain", "--json", "--jsonl"} {
		t.Run(mode, func(t *testing.T) {
			cmd := NewRootCommand()
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"events", "import", "--file", f, "--calendar", "Work", "--dry-run", mode, "--fields", "title,location,notes,url"})
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if mode == "--plain" {
				want := strings.Join([]string{escaped, escaped, escaped, escaped}, "\t") + "\n"
				if stdout.String() != want {
					t.Fatalf("got %q, want %q", stdout.String(), want)
				}
			} else {
				var item backend.EventCreateInput
				if mode == "--json" {
					var env struct{ Data []backend.EventCreateInput }
					if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
						t.Fatal(err)
					}
					if len(env.Data) != 1 {
						t.Fatalf("expected one event, got %d", len(env.Data))
					}
					item = env.Data[0]
				} else if err := json.Unmarshal(stdout.Bytes(), &item); err != nil {
					t.Fatal(err)
				}
				if item.Title != value || item.Location != value || item.Notes != value || item.URL != value {
					t.Fatalf("structured output changed event data: %+v", item)
				}
			}
			if fb.addCalls != 0 {
				t.Fatalf("expected no add calls in dry-run, got %d", fb.addCalls)
			}
		})
	}
}

func TestEventsImportMalformedICS(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "bad.ics")
	raw := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nSUMMARY:Broken\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	if err := os.WriteFile(f, []byte(raw), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "import", "--file", f, "--calendar", "Work", "--json"})
	err := cmd.Execute()
	if code := ExitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d err=%v", code, err)
	}
}

func TestEventsImportStrictRejectsWarnings(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "warn.ics")
	raw := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Good\r\nDTSTART:20260220T090000Z\r\nDTEND:20260220T100000Z\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nSUMMARY:Bad\r\nDTSTART:bad\r\nDTEND:bad\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	if err := os.WriteFile(f, []byte(raw), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "import", "--file", f, "--calendar", "Work", "--strict", "--dry-run", "--json"})
	err := cmd.Execute()
	if code := ExitCode(err); code != 2 {
		t.Fatalf("expected exit code 2, got %d err=%v", code, err)
	}
}

func TestParseICSRejectsRecurrenceProperties(t *testing.T) {
	for _, property := range []string{
		"RRULE:FREQ=WEEKLY", "RDATE:20260227T090000Z",
		"EXDATE:20260227T090000Z", "RECURRENCE-ID:20260220T090000Z",
		"rdate;VALUE=DATE:20260227", "exdate;TZID=Europe/Berlin:20260227T090000",
		"recurrence-id;RANGE=THISANDFUTURE:20260220T090000Z", "RRULE:",
		"RECURRENCE-ID;\r\n RANGE=THISANDFUTURE:20260220T090000Z",
		"EXDATE;\n\tVALUE=DATE:20260227",
	} {
		t.Run(property, func(t *testing.T) {
			raw := "BEGIN:VEVENT\nSUMMARY:Series\nDTSTART:20260220T090000Z\nDTEND:20260220T100000Z\n" + property + "\nEND:VEVENT\n"
			items, warnings := parseICS(raw, "Work", time.UTC)
			name := strings.ToUpper(strings.SplitN(strings.SplitN(property, ":", 2)[0], ";", 2)[0])
			if len(items) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "unsupported recurrence property "+name) {
				t.Fatalf("expected recurrence warning and no events, got items=%+v warnings=%v", items, warnings)
			}
		})
	}
}

func TestEventsImportRecurrenceBoundary(t *testing.T) {
	const independent = "BEGIN:VEVENT\nSUMMARY:Independent\nDTSTART:20260220T090000Z\nDTEND:20260220T100000Z\nEND:VEVENT\n"
	const recurring = "BEGIN:VEVENT\nSUMMARY:Series\nDTSTART:20260220T090000Z\nDTEND:20260220T100000Z\nRRULE:FREQ=WEEKLY\nEND:VEVENT\n"
	for _, tc := range []struct {
		name      string
		events    string
		flags     []string
		wantCalls int
		wantCode  int
	}{
		{"mixed", independent + recurring + independent, nil, 2, 0},
		{"strict", independent + recurring, []string{"--strict"}, 0, 2},
		{"strict-folded", independent + strings.ReplaceAll(recurring, "RRULE:FREQ=WEEKLY", "RECURRENCE-ID;\r\n RANGE=THISANDFUTURE:20260220T090000Z"), []string{"--strict"}, 0, 2},
		{"dry-run", recurring + independent, []string{"--dry-run"}, 0, 0},
		{"recurrence-only", recurring, nil, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fb := &scopeCaptureBackend{}
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return fb, nil }
			t.Cleanup(func() { backendFactory = origFactory })
			path := filepath.Join(t.TempDir(), "recurrence.ics")
			if err := os.WriteFile(path, []byte("BEGIN:VCALENDAR\n"+tc.events+"END:VCALENDAR\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := NewRootCommand()
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stdout)
			cmd.SetArgs(append([]string{"events", "import", "--file", path, "--calendar", "Work", "--json"}, tc.flags...))
			err := cmd.Execute()
			if ExitCode(err) != tc.wantCode || fb.addCalls != tc.wantCalls {
				t.Fatalf("err=%v calls=%d; want code=%d calls=%d", err, fb.addCalls, tc.wantCode, tc.wantCalls)
			}
			if tc.wantCalls > 0 && (fb.addInput.Title != "Independent" || fb.addInput.RepeatRule != "") {
				t.Fatalf("unexpected imported event: %+v", fb.addInput)
			}
			if tc.wantCode == 0 {
				var got struct {
					Warnings []string `json:"warnings"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "RRULE") {
					t.Fatalf("missing recurrence warning: %s", stdout.String())
				}
			} else if tc.name == "recurrence-only" && !strings.Contains(stdout.String(), "RRULE") {
				t.Fatalf("missing recurrence diagnostic: %s", stdout.String())
			}
		})
	}
}

func TestBuildICSExportsSeparateOccurrences(t *testing.T) {
	start := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	items := []contract.Event{
		{ID: "series/first", Title: "Weekly", Start: start, End: start.Add(time.Hour)},
		{ID: "series/second", Title: "Weekly", Start: start.AddDate(0, 0, 7), End: start.AddDate(0, 0, 7).Add(time.Hour)},
	}
	raw := buildICS(items)
	if strings.Count(raw, "BEGIN:VEVENT") != 2 || strings.Count(raw, "END:VEVENT") != 2 {
		t.Fatalf("expected separate occurrences: %s", raw)
	}
	parsed, warnings := parseICS(raw, "Work", time.UTC)
	if len(parsed) != 2 || len(warnings) != 0 {
		t.Fatalf("expected independent events: items=%+v warnings=%v", parsed, warnings)
	}
	for i, event := range parsed {
		if !event.Start.Equal(items[i].Start) || !event.End.Equal(items[i].End) || event.RepeatRule != "" {
			t.Fatalf("occurrence %d changed: %+v", i, event)
		}
	}
}
