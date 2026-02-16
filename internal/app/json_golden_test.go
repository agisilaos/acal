package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type fakeBackend struct {
	checks []contract.DoctorCheck
	events []contract.Event
}

func (f *fakeBackend) Doctor(context.Context) ([]contract.DoctorCheck, error) {
	return f.checks, nil
}

func (f *fakeBackend) ListCalendars(context.Context) ([]contract.Calendar, error) {
	return []contract.Calendar{}, nil
}

func (f *fakeBackend) ListEvents(_ context.Context, flt backend.EventFilter) ([]contract.Event, error) {
	return filterFakeEvents(f.events, flt), nil
}

func (f *fakeBackend) GetEventByID(context.Context, string) (*contract.Event, error) {
	return nil, nil
}

func (f *fakeBackend) AddEvent(context.Context, backend.EventCreateInput) (*contract.Event, error) {
	return nil, nil
}

func (f *fakeBackend) UpdateEvent(context.Context, string, backend.EventUpdateInput) (*contract.Event, error) {
	return nil, nil
}

func (f *fakeBackend) DeleteEvent(context.Context, string) error {
	return nil
}

func filterFakeEvents(events []contract.Event, flt backend.EventFilter) []contract.Event {
	out := make([]contract.Event, 0, len(events))
	for _, e := range events {
		if !flt.From.IsZero() && e.Start.Before(flt.From) {
			continue
		}
		if !flt.To.IsZero() && e.Start.After(flt.To) {
			continue
		}
		out = append(out, e)
		if flt.Limit > 0 && len(out) >= flt.Limit {
			break
		}
	}
	return out
}

func TestJSONGolden(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	fb := &fakeBackend{
		checks: []contract.DoctorCheck{
			{Name: "osascript", Status: "ok", Message: "osascript found"},
			{Name: "calendar_access", Status: "ok", Message: "Calendar automation reachable"},
			{Name: "calendar_db", Status: "ok", Message: "Calendar database found"},
			{Name: "calendar_db_read", Status: "ok", Message: "Calendar database readable"},
		},
		events: []contract.Event{
			{
				ID:           "evt-1@792417600",
				CalendarID:   "cal-1",
				CalendarName: "Work",
				Title:        "Standup",
				Start:        time.Date(2026, 2, 10, 10, 0, 0, 0, time.UTC),
				End:          time.Date(2026, 2, 10, 10, 30, 0, 0, time.UTC),
				AllDay:       false,
			},
			{
				ID:           "evt-2@792504000",
				CalendarID:   "cal-1",
				CalendarName: "Work",
				Title:        "Planning",
				Start:        time.Date(2026, 2, 11, 10, 0, 0, 0, time.UTC),
				End:          time.Date(2026, 2, 11, 11, 0, 0, 0, time.UTC),
				AllDay:       false,
			},
		},
	}

	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cases := []struct {
		name string
		args []string
	}{
		{name: "setup", args: []string{"setup", "--json"}},
		{name: "today", args: []string{"today", "--day", "2026-02-10", "--tz", "UTC", "--json"}},
		{name: "week_summary", args: []string{"week", "--of", "2026-02-11", "--tz", "UTC", "--week-start", "monday", "--summary", "--json"}},
		{name: "month", args: []string{"month", "--month", "2026-02", "--tz", "UTC", "--json"}},
		{name: "quick_add_dry_run", args: []string{"quick-add", "2026-02-18 09:15 Deep Work @Personal 45m", "--tz", "UTC", "--dry-run", "--json"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runRootForJSONGolden(t, tc.args)
			assertGoldenJSON(t, tc.name, got)
		})
	}
}

func runRootForJSONGolden(t *testing.T, args []string) string {
	t.Helper()
	cmd := NewRootCommand()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	stdout := captureStdout(t, func() error { return cmd.Execute() })
	if stderr.Len() > 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
	return normalizeEnvelopeJSON(t, []byte(stdout))
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe create failed: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	errCh := make(chan error, 1)
	var buf bytes.Buffer
	go func() {
		_, copyErr := io.Copy(&buf, r)
		errCh <- copyErr
	}()

	runErr := fn()
	_ = w.Close()
	copyErr := <-errCh
	_ = r.Close()

	if runErr != nil {
		returnError := runErr
		t.Fatalf("command failed: %v", returnError)
	}
	if copyErr != nil {
		t.Fatalf("stdout capture failed: %v", copyErr)
	}
	return buf.String()
}

func normalizeEnvelopeJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("json unmarshal failed: %v\nraw=%s", err, string(raw))
	}
	if _, ok := obj["generated_at"]; ok {
		obj["generated_at"] = "<generated>"
	}
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}
	return string(out) + "\n"
}

func assertGoldenJSON(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, string(want))
	}
}
