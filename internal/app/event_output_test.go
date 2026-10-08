package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestEventCommandsRenderSelectedTimezone(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, name := range []string{"ACAL_CONFIG", "ACAL_PROFILE", "ACAL_TIMEZONE", "ACAL_FAIL_ON_DEGRADED"} {
		t.Setenv(name, "")
	}
	start, err := time.Parse(time.RFC3339, "2026-09-25T14:00:00+02:00")
	if err != nil {
		t.Fatal(err)
	}
	event := contract.Event{ID: "fixture@812030400", Start: start, End: start.Add(time.Hour), UpdatedAt: start}
	fb := &scopeCaptureBackend{events: []contract.Event{event}, getEvent: &event}
	original := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = original })
	for _, command := range [][]string{
		{"events", "list", "--from", "2026-09-25", "--to", "2026-09-26"},
		{"events", "show", event.ID},
	} {
		for _, mode := range []string{"json", "jsonl", "plain"} {
			t.Run(command[1]+"/"+mode, func(t *testing.T) {
				cmd := NewRootCommand()
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&out)
				args := append(append([]string{}, command...), "--tz", "UTC", "--"+mode)
				if mode == "plain" {
					args = append(args, "--fields", "start,end,updated_at")
				}
				cmd.SetArgs(args)
				if err := cmd.Execute(); err != nil {
					t.Fatalf("command: %v: %s", err, &out)
				}
				if mode == "plain" {
					const want = "2026-09-25 12:00:00 +0000 UTC\t2026-09-25 13:00:00 +0000 UTC\t2026-09-25 12:00:00 +0000 UTC\n"
					if out.String() != want {
						t.Fatalf("wrong timezone: %q", out.String())
					}
					return
				}
				var records []map[string]any
				raw := out.Bytes()
				if mode == "json" {
					var envelope struct{ Data json.RawMessage }
					if err := json.Unmarshal(raw, &envelope); err != nil {
						t.Fatal(err)
					}
					raw = envelope.Data
				}
				if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
					if err := json.Unmarshal(raw, &records); err != nil {
						t.Fatal(err)
					}
				} else {
					var record map[string]any
					if err := json.Unmarshal(raw, &record); err != nil {
						t.Fatal(err)
					}
					records = append(records, record)
				}
				if len(records) != 1 {
					t.Fatalf("expected one event: %s", &out)
				}
				for field, want := range map[string]string{"start": "2026-09-25T12:00:00Z", "end": "2026-09-25T13:00:00Z", "updated_at": "2026-09-25T12:00:00Z"} {
					if records[0][field] != want {
						t.Errorf("%s = %v, want %s", field, records[0][field], want)
					}
				}
			})
		}
	}
}

func TestPlanningCommandsRenderSelectedTimezone(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, name := range []string{"ACAL_CONFIG", "ACAL_PROFILE", "ACAL_TIMEZONE", "ACAL_FAIL_ON_DEGRADED"} {
		t.Setenv(name, "")
	}
	start, err := time.Parse(time.RFC3339, "2026-09-25T14:00:00+02:00")
	if err != nil {
		t.Fatal(err)
	}
	original := backendFactory
	t.Cleanup(func() { backendFactory = original })
	for _, tc := range []struct {
		name, fields string
		args         []string
		events       []contract.Event
	}{
		{"freebusy", "start,end", []string{"freebusy"}, []contract.Event{{ID: "one", Start: start, End: start.Add(time.Hour)}}},
		{"conflicts", "overlap_start,overlap_end", []string{"events", "conflicts"}, []contract.Event{{ID: "one", Start: start, End: start.Add(time.Hour)}, {ID: "two", Start: start, End: start.Add(time.Hour)}}},
		{"slots", "start,end", []string{"slots", "--between", "14:00-15:00", "--duration", "1h", "--step", "1h"}, nil},
	} {
		for _, mode := range []string{"json", "jsonl", "plain"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				backendFactory = func(string) (backend.Backend, error) { return &scopeCaptureBackend{events: tc.events}, nil }
				cmd := NewRootCommand()
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&out)
				args := append(append([]string{}, tc.args...), "--from", "2026-09-25T14:00:00+02:00", "--to", "2026-09-25T15:00:00+02:00", "--tz", "UTC", "--"+mode)
				if mode == "plain" {
					args = append(args, "--fields", tc.fields)
				}
				cmd.SetArgs(args)
				if err := cmd.Execute(); err != nil {
					t.Fatalf("command: %v: %s", err, &out)
				}
				if mode == "plain" {
					if want := "2026-09-25 12:00:00 +0000 UTC\t2026-09-25 13:00:00 +0000 UTC\n"; out.String() != want {
						t.Fatalf("wrong timezone: %q", out.String())
					}
					return
				}
				var records []map[string]any
				if mode == "json" {
					var envelope struct{ Data []map[string]any }
					if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
						t.Fatal(err)
					}
					records = envelope.Data
				} else {
					var record map[string]any
					if err := json.Unmarshal(out.Bytes(), &record); err != nil {
						t.Fatal(err)
					}
					records = append(records, record)
				}
				if len(records) != 1 {
					t.Fatalf("expected one interval: %s", &out)
				}
				fields := strings.Split(tc.fields, ",")
				if records[0][fields[0]] != "2026-09-25T12:00:00Z" || records[0][fields[1]] != "2026-09-25T13:00:00Z" {
					t.Fatalf("wrong timezone: %s", &out)
				}
			})
		}
	}
}
