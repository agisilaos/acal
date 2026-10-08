package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
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

func TestQuickAddInlineCalendarOverridesFallback(t *testing.T) {
	for _, alias := range []string{"quick-add", "events quick-add"} {
		t.Run(alias, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			args := append(strings.Fields(alias), "2026-10-09 10:00 Probe @Work", "--calendar", "Personal", "--dry-run", "--json")
			out, err := runReminderHistoryCommand(t, &scopeCaptureBackend{}, args...)
			if err != nil {
				t.Fatalf("quick-add failed: %v\n%s", err, out)
			}
			var response struct{ Data backend.EventCreateInput }
			if err := json.Unmarshal([]byte(out), &response); err != nil {
				t.Fatal(err)
			}
			if response.Data.Calendar != "Work" || response.Data.Title != "Probe" {
				t.Fatalf("calendar=%q title=%q; want Work and Probe", response.Data.Calendar, response.Data.Title)
			}
		})
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

func TestParseQuickAddInputAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, now, input, start, end string
		allDay                       bool
		duration                     time.Duration
	}{
		{"spring all-day", "2026-03-28T12:00:00Z", "tomorrow Offsite @Work", "2026-03-29T00:00:00+01:00", "2026-03-30T00:00:00+02:00", true, 23 * time.Hour},
		{"fall all-day", "2026-10-24T12:00:00Z", "tomorrow Offsite @Work", "2026-10-25T00:00:00+02:00", "2026-10-26T00:00:00+01:00", true, 25 * time.Hour},
		{"spring tomorrow clock", "2026-03-29T12:00:00Z", "tomorrow 10:00 Standup @Work 30m", "2026-03-30T10:00:00+02:00", "2026-03-30T10:30:00+02:00", false, 30 * time.Minute},
		{"fall tomorrow clock", "2026-10-25T12:00:00Z", "tomorrow 10:00 Standup @Work 30m", "2026-10-26T10:00:00+01:00", "2026-10-26T10:30:00+01:00", false, 30 * time.Minute},
		{"spring elapsed duration", "2026-03-28T12:00:00Z", "tomorrow 01:30 Work @Work 2h", "2026-03-29T01:30:00+01:00", "2026-03-29T04:30:00+02:00", false, 2 * time.Hour},
		{"fall elapsed duration", "2026-10-24T12:00:00Z", "tomorrow 01:30 Work @Work 2h", "2026-10-25T01:30:00+02:00", "2026-10-25T02:30:00+01:00", false, 2 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			in, err := parseQuickAddInput(tc.input, now, loc, "", time.Hour, tc.allDay)
			if err != nil {
				t.Fatal(err)
			}
			if in.Start.Format(time.RFC3339) != tc.start || in.End.Format(time.RFC3339) != tc.end {
				t.Fatalf("got %s to %s, want %s to %s", in.Start, in.End, tc.start, tc.end)
			}
			if in.AllDay != tc.allDay || in.End.Sub(in.Start) != tc.duration {
				t.Fatalf("allDay=%t duration=%s, want %t and %s", in.AllDay, in.End.Sub(in.Start), tc.allDay, tc.duration)
			}
		})
	}
}

func TestQuickAddHistoryAcrossAliasesAndModes(t *testing.T) {
	for _, alias := range []string{"quick-add", "events quick-add"} {
		for _, mode := range []string{"--plain", "--json", "--jsonl"} {
			for _, scenario := range []string{"success", "dry-run", "backend failure", "history failure"} {
				t.Run(alias+"/"+mode+"/"+scenario, func(t *testing.T) {
					t.Setenv("XDG_CONFIG_HOME", t.TempDir())
					seed := []historyEntry{{Type: "add", EventID: "prior-event", Created: &contract.Event{ID: "prior-event"}}}
					if err := writeRedoHistory(seed); err != nil {
						t.Fatal(err)
					}
					if scenario == "history failure" {
						// A directory at the history path deterministically prevents appending.
						if err := os.Mkdir(historyFilePath(), 0o700); err != nil {
							t.Fatal(err)
						}
					}
					fb := &scopeCaptureBackend{}
					if scenario == "backend failure" {
						fb.addErr = errors.New("add failed")
					}
					originalFactory := backendFactory
					backendFactory = func(string) (backend.Backend, error) { return fb, nil }
					t.Cleanup(func() { backendFactory = originalFactory })
					cmd := NewRootCommand()
					var out bytes.Buffer
					cmd.SetOut(&out)
					cmd.SetErr(&bytes.Buffer{})
					args := append(strings.Fields(alias), "2026-10-01 10:00 Standup @Work 30m", mode, "--tz", "UTC")
					if scenario == "dry-run" {
						args = append(args, "--dry-run")
					}
					cmd.SetArgs(args)
					err := cmd.Execute()
					if scenario == "backend failure" {
						if err == nil || err.Error() != fb.addErr.Error() || ExitCode(err) != 1 {
							t.Fatalf("got %v, want backend failure", err)
						}
					} else if err != nil {
						t.Fatalf("quick-add failed: %v", err)
					}
					wantCalls := 1
					if scenario == "dry-run" {
						wantCalls = 0
					}
					if fb.addCalls != wantCalls {
						t.Fatalf("add calls = %d, want %d", fb.addCalls, wantCalls)
					}
					if scenario == "success" || scenario == "history failure" {
						if !strings.Contains(out.String(), "new-evt@792417600") {
							t.Fatalf("missing created event output: %q", out.String())
						}
					}
					if scenario != "history failure" {
						entries, err := readHistory()
						if err != nil {
							t.Fatal(err)
						}
						if scenario == "success" {
							if len(entries) != 1 || entries[0].Type != "add" || entries[0].EventID != "new-evt@792417600" || entries[0].Created == nil || *entries[0].Created != (contract.Event{ID: "new-evt@792417600"}) {
								t.Fatalf("expected exactly one created-event snapshot, got %+v", entries)
							}
						} else if len(entries) != 0 {
							t.Fatalf("unexpected history: %+v", entries)
						}
					}
					redo, err := readRedoHistory()
					if err != nil {
						t.Fatal(err)
					}
					if scenario == "success" {
						if len(redo) != 0 {
							t.Fatalf("redo not invalidated: %+v", redo)
						}
					} else if !reflect.DeepEqual(redo, seed) {
						t.Fatalf("redo changed: got %+v, want %+v", redo, seed)
					}
				})
			}
		}
	}
}

// Echo the parsed request without touching Calendar; return backend-only fields
// as well so projection is checked against the created snapshot.
type quickAddOutputBackend struct {
	scopeCaptureBackend
	created *contract.Event
}

func (b *quickAddOutputBackend) AddEvent(_ context.Context, in backend.EventCreateInput) (*contract.Event, error) {
	b.addCalls++
	b.addInput = in
	b.created = &contract.Event{ID: "created-id", Title: in.Title, Start: in.Start, End: in.End, CalendarID: "work-id", CalendarName: in.Calendar, Notes: "line\nnext\tcell\r\x1b[31m\x7f", AllDay: in.AllDay}
	return b.created, nil
}

func TestQuickAddOutputProjection(t *testing.T) {
	for _, alias := range []string{"quick-add", "events quick-add"} {
		for _, dryRun := range []bool{true, false} {
			for _, fields := range []string{"", "title,start", "end,title,calendar,id", "notes,calendar_id,all_day", "missing,title"} {
				t.Run(fmt.Sprintf("%s/dry=%t/fields=%s", alias, dryRun, fields), func(t *testing.T) {
					t.Setenv("XDG_CONFIG_HOME", t.TempDir())
					b := &quickAddOutputBackend{}
					original := backendFactory
					backendFactory = func(string) (backend.Backend, error) { return b, nil }
					t.Cleanup(func() { backendFactory = original })
					cmd := NewRootCommand()
					var out bytes.Buffer
					cmd.SetOut(&out)
					cmd.SetErr(&bytes.Buffer{})
					args := append(strings.Fields(alias), "2026-10-01 09:00 \x1b[31mReview @Work\x07 30m", "--plain", "--tz", "UTC")
					if fields != "" {
						args = append(args, "--fields", fields)
					}
					if dryRun {
						args = append(args, "--dry-run")
					}
					cmd.SetArgs(args)
					if err := cmd.Execute(); err != nil {
						t.Fatal(err)
					}
					id := "created-id"
					if dryRun {
						id = "dry-run"
					}
					want := id + "\t2026-10-01T09:00:00Z\t2026-10-01T09:30:00Z\tWork\\u0007\t\\u001b[31mReview\n"
					switch fields {
					case "title,start":
						want = "\\u001b[31mReview\t2026-10-01T09:00:00Z\n"
					case "end,title,calendar,id":
						want = "2026-10-01T09:30:00Z\t\\u001b[31mReview\tWork\\u0007\t" + id + "\n"
					case "notes,calendar_id,all_day":
						want = "line\\nnext\\tcell\\r\\u001b[31m\\u007f\twork-id\tfalse\n"
						if dryRun {
							want = "\t\tfalse\n"
						}
					case "missing,title":
						want = "\t\\u001b[31mReview\n"
					}
					if out.String() != want {
						t.Fatalf("got %q, want %q", out.String(), want)
					}
					entries, err := readHistory()
					if err != nil {
						t.Fatal(err)
					}
					if dryRun {
						if b.addCalls != 0 || len(entries) != 0 {
							t.Fatal("preview mutated backend or history")
						}
					} else if b.addCalls != 1 || len(entries) != 1 || !reflect.DeepEqual(entries[0].Created, b.created) {
						t.Fatalf("created snapshot was changed by rendering: %+v", entries)
					}
				})
			}
		}
	}
}

func TestQuickAddStructuredOutputSchema(t *testing.T) {
	for _, alias := range []string{"quick-add", "events quick-add"} {
		for _, mode := range []string{"--json", "--jsonl"} {
			for _, dryRun := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/dry=%t", alias, mode, dryRun), func(t *testing.T) {
					t.Setenv("XDG_CONFIG_HOME", t.TempDir())
					b := &quickAddOutputBackend{}
					original := backendFactory
					backendFactory = func(string) (backend.Backend, error) { return b, nil }
					t.Cleanup(func() { backendFactory = original })
					cmd := NewRootCommand()
					var out bytes.Buffer
					cmd.SetOut(&out)
					cmd.SetErr(&bytes.Buffer{})
					input := "2026-10-01 09:00 Review @Work 30m"
					args := append(strings.Fields(alias), input, mode, "--fields", "title,start", "--tz", "UTC")
					if dryRun {
						args = append(args, "--dry-run")
					}
					cmd.SetArgs(args)
					if err := cmd.Execute(); err != nil {
						t.Fatal(err)
					}
					var data any
					if err := json.Unmarshal(out.Bytes(), &data); err != nil {
						t.Fatal(err)
					}
					if mode == "--json" {
						env := data.(map[string]any)
						if env["command"] != strings.ReplaceAll(alias, " ", ".") || env["schema_version"] != contract.SchemaVersion {
							t.Fatalf("unexpected envelope: %v", env)
						}
						meta := map[string]any{"count": float64(1)}
						if dryRun {
							meta = map[string]any{"dry_run": true}
						}
						if !reflect.DeepEqual(env["meta"], meta) {
							t.Fatalf("unexpected metadata: %v", env["meta"])
						}
						data = env["data"]
					}
					var expected any = b.created
					if dryRun {
						in, err := parseQuickAddInput(input, time.Now(), time.UTC, "", time.Hour, false)
						if err != nil {
							t.Fatal(err)
						}
						expected = in
					}
					encoded, err := json.Marshal(expected)
					if err != nil {
						t.Fatal(err)
					}
					var want any
					if err := json.Unmarshal(encoded, &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(data, want) {
						t.Fatalf("wire data changed: got %v want %v", data, want)
					}
				})
			}
		}
	}
}

func TestQuickAddNonpositiveInlineDuration(t *testing.T) {
	for _, duration := range []string{"0m", "-30m", "0h 30m", "-1h 1h"} {
		if _, err := parseQuickAddInput("2026-10-01 09:00 Review @Work "+duration, time.Now(), time.UTC, "", time.Hour, false); err == nil {
			t.Fatalf("accepted %q", duration)
		}
	}
	in, err := parseQuickAddInput("2026-10-01 09:00 Review 0 milestones @Work 30m", time.Now(), time.UTC, "", time.Hour, false)
	if err != nil || in.Title != "Review 0 milestones" || in.End.Sub(in.Start) != 30*time.Minute {
		t.Fatalf("valid input: %+v err=%v", in, err)
	}
}
