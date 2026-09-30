package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	got := buildICS(items, time.UTC)
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
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
	raw := buildICS(items, time.UTC)
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

func TestICSDateParameters(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, header, value, want string
		allDay                    bool
	}{
		{"winter", "DTSTART;TZID=America/New_York", "20260220T090000", "2026-02-20T14:00:00Z", false},
		{"summer", "dtstart;tzid=America/New_York", "20260720T090000", "2026-07-20T13:00:00Z", false},
		{"quoted", `DTSTART;TZID="America/New_York"`, "20260220T090000", "2026-02-20T14:00:00Z", false},
		{"floating", "DTSTART", "20260220T090000", "2026-02-20T08:00:00Z", false},
		{"explicit date-time", "DTSTART;VALUE=DATE-TIME", "20260220T090000", "2026-02-20T08:00:00Z", false},
		{"UTC", "DTSTART;VALUE=DATE-TIME", "20260220T090000Z", "2026-02-20T09:00:00Z", false},
		{"date", "DTSTART;value=date", "20260220", "2026-02-19T23:00:00Z", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			end := "DTEND:20260801T100000Z"
			if tt.allDay {
				end = "DTEND;VALUE=DATE:20260221"
			}
			raw := "BEGIN:VEVENT\n" + tt.header + ":" + tt.value + "\n" + end + "\nEND:VEVENT\n"
			items, warnings := parseICS(raw, "Work", berlin)
			if len(items) != 1 || len(warnings) != 0 {
				t.Fatalf("items=%v warnings=%v", items, warnings)
			}
			if got := items[0].Start.UTC().Format(time.RFC3339); got != tt.want || items[0].AllDay != tt.allDay {
				t.Fatalf("start=%s allDay=%t; want %s %t", got, items[0].AllDay, tt.want, tt.allDay)
			}
		})
	}
}

func TestICSUnsupportedDateParameters(t *testing.T) {
	for _, property := range []string{
		"TZID=Mars/Olympus:20260220T090000",
		"TZID=Local:20260220T090000",
		"TZID=/custom/Zone:20260220T090000",
		"TZID=America/New_York:20260220T090000Z",
		"VALUE=DATE;TZID=America/New_York:20260220",
		"VALUE=DATE-TIME:20260220",
		"VALUE=DATE:20260220T090000",
		"VALUE=DATE-OTHER:20260220",
		`VALUE="":20260220T090000`,
		"TZID=:20260220T090000",
		"TZID=UTC;TZID=America/New_York:20260220T090000",
	} {
		t.Run(property, func(t *testing.T) {
			for _, field := range []string{"DTSTART", "DTEND"} {
				raw := "BEGIN:VEVENT\nDTSTART:20260220T080000Z\nDTEND:20260220T100000Z\n" + field + ";" + property + "\nEND:VEVENT\n"
				items, warnings := parseICS(raw, "Work", time.UTC)
				if len(items) != 0 || len(warnings) != 1 {
					t.Fatalf("%s: items=%v warnings=%v", field, items, warnings)
				}
			}
		})
	}
}

func TestEventsImportTimezoneWrites(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, strict := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "strict"}[strict], func(t *testing.T) {
			fb := &scopeCaptureBackend{}
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return fb, nil }
			t.Cleanup(func() { backendFactory = origFactory })
			raw := "BEGIN:VCALENDAR\n" +
				"BEGIN:VEVENT\nDTSTART;TZID=America/New_York:20260220T090000\nDTEND;TZID=America/New_York:20260220T100000\nEND:VEVENT\n" +
				"BEGIN:VTIMEZONE\nTZID:Custom\nEND:VTIMEZONE\n" +
				"BEGIN:VEVENT\nDTSTART;TZID=Custom:20260220T090000\nDTEND;TZID=Custom:20260220T100000\nEND:VEVENT\nEND:VCALENDAR\n"
			path := filepath.Join(t.TempDir(), "zones.ics")
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := NewRootCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			args := []string{"events", "import", "--file", path, "--calendar", "Work", "--tz", "UTC", "--json"}
			if strict {
				args = append(args, "--strict")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if strict {
				if ExitCode(err) != 2 || fb.addCalls != 0 {
					t.Fatalf("strict import: err=%v writes=%d", err, fb.addCalls)
				}
				return
			}
			if err != nil || fb.addCalls != 1 {
				t.Fatalf("import: err=%v writes=%d", err, fb.addCalls)
			}
			if got := fb.addInput.Start.UTC().Format(time.RFC3339); got != "2026-02-20T14:00:00Z" {
				t.Fatalf("backend start=%s", got)
			}
			if got := fb.addInput.End.UTC().Format(time.RFC3339); got != "2026-02-20T15:00:00Z" {
				t.Fatalf("backend end=%s", got)
			}
			var result struct {
				Warnings []string `json:"warnings"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil || len(result.Warnings) != 1 {
				t.Fatalf("expected one warning: %s (%v)", out.String(), err)
			}
		})
	}
}

type importProgressBackend struct {
	scopeCaptureBackend
	failAt  int
	failErr error
}

func (b *importProgressBackend) AddEvent(_ context.Context, in backend.EventCreateInput) (*contract.Event, error) {
	b.addCalls++
	if b.addCalls == b.failAt {
		if b.failErr != nil {
			return nil, b.failErr
		}
		return nil, fmt.Errorf("injected import failure")
	}
	return &contract.Event{ID: fmt.Sprintf("created-%d", b.addCalls), Title: in.Title, Start: in.Start, End: in.End}, nil
}

func TestImportRecordsAndReportsProgress(t *testing.T) {
	for _, mode := range []string{"--json", "--jsonl", "--plain"} {
		for _, tc := range []struct {
			name                string
			failAt, want, code  int
			dry, historyFailure bool
			failErr             error
		}{
			{name: "success", want: 2}, {name: "first fails", failAt: 1, code: 1}, {name: "second fails", failAt: 2, want: 1, code: 1}, {name: "preview", dry: true}, {name: "history fails", want: 1, code: 1, historyFailure: true},
			{name: "native second failure", failAt: 2, want: 1, code: 1, failErr: &backend.CreationOutcomeError{Err: fmt.Errorf("AppleEvent timed out (-1712)")}},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
				fb := &importProgressBackend{failAt: tc.failAt, failErr: tc.failErr}
				orig := backendFactory
				backendFactory = func(string) (backend.Backend, error) { return fb, nil }
				t.Cleanup(func() { backendFactory = orig })
				if err := writeRedoHistory([]historyEntry{{Type: "add", EventID: "old", Created: &contract.Event{ID: "old"}}}); err != nil {
					t.Fatal(err)
				}
				if tc.historyFailure {
					if err := os.Mkdir(historyFilePath(), 0700); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(t.TempDir(), "in.ics")
				event := "BEGIN:VEVENT\nSUMMARY:Import\nDTSTART:20261001T090000Z\nDTEND:20261001T100000Z\nEND:VEVENT\n"
				if err := os.WriteFile(path, []byte("BEGIN:VCALENDAR\n"+event+event+"END:VCALENDAR\n"), 0600); err != nil {
					t.Fatal(err)
				}
				args := []string{"events", "import", "--file", path, "--calendar", "Work", mode}
				if tc.dry {
					args = append(args, "--dry-run")
				}
				cmd := NewRootCommand()
				var out, errOut bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&errOut)
				cmd.SetArgs(args)
				if code := ExitCode(cmd.Execute()); code != tc.code {
					t.Fatalf("exit=%d want=%d: %s", code, tc.code, &errOut)
				}
				if tc.code != 0 {
					if out.Len() != 0 {
						t.Fatalf("failure stdout: %s", &out)
					}
					if !strings.Contains(errOut.String(), "retrying the whole file can duplicate") {
						t.Fatalf("missing recovery hint: %s", &errOut)
					}
					if mode != "--plain" {
						var env struct {
							Meta struct {
								Count   int
								IDs     []string `json:"created_ids"`
								Item    int      `json:"failed_item"`
								Kind    string   `json:"kind"`
								Outcome string   `json:"outcome"`
							}
						}
						if err := json.Unmarshal(errOut.Bytes(), &env); err != nil {
							t.Fatal(err)
						}
						if env.Meta.Count != tc.want || len(env.Meta.IDs) != tc.want || env.Meta.Item < 1 {
							t.Fatalf("progress: %s", &errOut)
						}
						if tc.failErr != nil && (env.Meta.Kind != "creation_outcome_unknown" || env.Meta.Outcome != "unknown") {
							t.Fatalf("lost native uncertainty: %s", &errOut)
						}
					}
				}
				if tc.historyFailure {
					return
				}
				entries, err := readHistory()
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != tc.want {
					t.Fatalf("history=%d want=%d", len(entries), tc.want)
				}
				redo, err := readRedoHistory()
				if err != nil {
					t.Fatal(err)
				}
				if tc.want > 0 && len(redo) != 0 || tc.want == 0 && len(redo) != 1 {
					t.Fatalf("redo=%d", len(redo))
				}
				for i, e := range entries {
					if e.Type != "add" || e.Created == nil || e.EventID != fmt.Sprintf("created-%d", i+1) {
						t.Fatalf("bad history: %+v", e)
					}
				}
				if tc.want > 0 {
					cmd := NewRootCommand()
					cmd.SetOut(io.Discard)
					cmd.SetErr(io.Discard)
					cmd.SetArgs([]string{"history", "undo", "--json"})
					if err := cmd.Execute(); err != nil {
						t.Fatal(err)
					}
					if fb.deleteCalls != 1 {
						t.Fatal("undo did not delete imported event")
					}
				}
				if tc.dry && fb.addCalls != 0 {
					t.Fatal("preview wrote events")
				}
			})
		}
	}
}

func TestAllDayExportRoundTripInSelectedTimezone(t *testing.T) {
	for _, zone := range []string{"Europe/Berlin", "Pacific/Kiritimati", "America/Los_Angeles"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			for _, date := range []string{"2026-10-01", "2026-03-29", "2026-03-08"} {
				start, err := time.ParseInLocation("2006-01-02", date, loc)
				if err != nil {
					t.Fatal(err)
				}
				end := start.AddDate(0, 0, 1)
				// Backend timestamps may be represented in a different location.
				fb := &scopeCaptureBackend{events: []contract.Event{{ID: "all-day", Title: "Holiday", Start: start.UTC(), End: end.UTC(), AllDay: true}, {ID: "timed", Title: "Meeting", Start: start.Add(9 * time.Hour), End: start.Add(10 * time.Hour)}}}
				orig := backendFactory
				backendFactory = func(string) (backend.Backend, error) { return fb, nil }
				cmd := NewRootCommand()
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(io.Discard)
				cmd.SetArgs([]string{"events", "export", "--from", date, "--to", end.Format("2006-01-02"), "--tz", zone, "--json"})
				err = cmd.Execute()
				backendFactory = orig
				if err != nil {
					t.Fatal(err)
				}
				var env struct {
					Data struct {
						ICS string `json:"ics"`
					}
				}
				if err := json.Unmarshal(out.Bytes(), &env); err != nil {
					t.Fatal(err)
				}
				imported, warnings := parseICS(env.Data.ICS, "Work", loc)
				if len(warnings) != 0 || len(imported) != 2 {
					t.Fatalf("parse: %+v %v", imported, warnings)
				}
				if !imported[0].AllDay || !imported[0].Start.Equal(start) || !imported[0].End.Equal(end) {
					t.Fatalf("shifted dates: %+v", imported[0])
				}
				if !imported[1].Start.Equal(fb.events[1].Start) || !imported[1].End.Equal(fb.events[1].End) {
					t.Fatal("timed instants changed")
				}
			}
		})
	}
}
