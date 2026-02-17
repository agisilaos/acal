package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type scopeCaptureBackend struct {
	updateInput backend.EventUpdateInput
	deleteScope backend.RecurrenceScope
	addInput    backend.EventCreateInput
	getEvent    *contract.Event
	getErr      error
	addErr      error
	updateErr   error
	deleteErr   error
	addCalls    int
	updateCalls int
	deleteCalls int
}

func (b *scopeCaptureBackend) Doctor(context.Context) ([]contract.DoctorCheck, error) {
	return nil, nil
}

func (b *scopeCaptureBackend) ListCalendars(context.Context) ([]contract.Calendar, error) {
	return nil, nil
}

func (b *scopeCaptureBackend) ListEvents(context.Context, backend.EventFilter) ([]contract.Event, error) {
	return nil, nil
}

func (b *scopeCaptureBackend) GetEventByID(context.Context, string) (*contract.Event, error) {
	if b.getEvent != nil || b.getErr != nil {
		return b.getEvent, b.getErr
	}
	return &contract.Event{ID: "evt@792417600", Start: time.Now(), Sequence: 1}, nil
}

func (b *scopeCaptureBackend) AddEvent(_ context.Context, in backend.EventCreateInput) (*contract.Event, error) {
	b.addCalls++
	b.addInput = in
	if b.addErr != nil {
		return nil, b.addErr
	}
	return &contract.Event{ID: "new-evt@792417600"}, nil
}

func (b *scopeCaptureBackend) UpdateEvent(_ context.Context, id string, in backend.EventUpdateInput) (*contract.Event, error) {
	b.updateCalls++
	b.updateInput = in
	if b.updateErr != nil {
		return nil, b.updateErr
	}
	return &contract.Event{ID: id}, nil
}

func (b *scopeCaptureBackend) DeleteEvent(_ context.Context, _ string, scope backend.RecurrenceScope) error {
	b.deleteCalls++
	b.deleteScope = scope
	if b.deleteErr != nil {
		return b.deleteErr
	}
	return nil
}

func TestEventsUpdatePassesScope(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "update", "evt@792417600", "--title", "new", "--scope", "this", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.updateInput.Scope != backend.ScopeThis {
		t.Fatalf("scope mismatch: got=%q want=%q", fb.updateInput.Scope, backend.ScopeThis)
	}
}

func TestEventsDeletePassesScope(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "delete", "evt@792417600", "--force", "--scope", "series", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.deleteScope != backend.ScopeSeries {
		t.Fatalf("scope mismatch: got=%q want=%q", fb.deleteScope, backend.ScopeSeries)
	}
}

func TestEventsVerboseEmitsDiagnostics(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	var stderr bytes.Buffer
	cmd.SetOut(io.Discard)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"events", "list", "--from", "today", "--to", "+1d", "--verbose", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := stderr.String(); !strings.Contains(got, "acal: command=events.list") {
		t.Fatalf("expected verbose diagnostics, got %q", got)
	}
}

func TestEventsUpdateValidationMatrix(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		backend         *scopeCaptureBackend
		wantExitCode    int
		wantUpdateCalls int
	}{
		{
			name:            "invalid scope",
			args:            []string{"events", "update", "evt@792417600", "--scope", "bogus", "--title", "x", "--json"},
			backend:         &scopeCaptureBackend{},
			wantExitCode:    2,
			wantUpdateCalls: 0,
		},
		{
			name:            "if-match mismatch",
			args:            []string{"events", "update", "evt@792417600", "--if-match-seq", "2", "--title", "x", "--json"},
			backend:         &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: time.Now(), Sequence: 1}},
			wantExitCode:    7,
			wantUpdateCalls: 0,
		},
		{
			name:            "if-match not-found",
			args:            []string{"events", "update", "evt@792417600", "--if-match-seq", "2", "--title", "x", "--json"},
			backend:         &scopeCaptureBackend{getErr: errors.New("missing")},
			wantExitCode:    4,
			wantUpdateCalls: 0,
		},
		{
			name:            "dry-run skips backend update",
			args:            []string{"events", "update", "evt@792417600", "--scope", "this", "--title", "x", "--dry-run", "--json"},
			backend:         &scopeCaptureBackend{},
			wantExitCode:    0,
			wantUpdateCalls: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return tc.backend, nil }
			t.Cleanup(func() { backendFactory = origFactory })

			cmd := NewRootCommand()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if got := ExitCode(err); got != tc.wantExitCode {
				t.Fatalf("exit code mismatch: got=%d want=%d err=%v", got, tc.wantExitCode, err)
			}
			if tc.backend.updateCalls != tc.wantUpdateCalls {
				t.Fatalf("update calls mismatch: got=%d want=%d", tc.backend.updateCalls, tc.wantUpdateCalls)
			}
		})
	}
}

func TestEventsDeleteValidationMatrix(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		backend         *scopeCaptureBackend
		wantExitCode    int
		wantDeleteCalls int
	}{
		{
			name:            "non-interactive requires confirmation",
			args:            []string{"events", "delete", "evt@792417600", "--no-input", "--json"},
			backend:         &scopeCaptureBackend{},
			wantExitCode:    2,
			wantDeleteCalls: 0,
		},
		{
			name:            "invalid scope",
			args:            []string{"events", "delete", "evt@792417600", "--force", "--scope", "bogus", "--json"},
			backend:         &scopeCaptureBackend{},
			wantExitCode:    2,
			wantDeleteCalls: 0,
		},
		{
			name:            "if-match mismatch",
			args:            []string{"events", "delete", "evt@792417600", "--force", "--if-match-seq", "2", "--json"},
			backend:         &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: time.Now(), Sequence: 1}},
			wantExitCode:    7,
			wantDeleteCalls: 0,
		},
		{
			name:            "if-match not-found",
			args:            []string{"events", "delete", "evt@792417600", "--force", "--if-match-seq", "2", "--json"},
			backend:         &scopeCaptureBackend{getErr: errors.New("missing")},
			wantExitCode:    4,
			wantDeleteCalls: 0,
		},
		{
			name:            "dry-run skips backend delete",
			args:            []string{"events", "delete", "evt@792417600", "--force", "--dry-run", "--json"},
			backend:         &scopeCaptureBackend{},
			wantExitCode:    0,
			wantDeleteCalls: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return tc.backend, nil }
			t.Cleanup(func() { backendFactory = origFactory })

			cmd := NewRootCommand()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if got := ExitCode(err); got != tc.wantExitCode {
				t.Fatalf("exit code mismatch: got=%d want=%d err=%v", got, tc.wantExitCode, err)
			}
			if tc.backend.deleteCalls != tc.wantDeleteCalls {
				t.Fatalf("delete calls mismatch: got=%d want=%d", tc.backend.deleteCalls, tc.wantDeleteCalls)
			}
		})
	}
}

func TestEventsMovePassesPatch(t *testing.T) {
	base := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	fb := &scopeCaptureBackend{
		getEvent: &contract.Event{
			ID:       "evt@792417600",
			Start:    base,
			End:      base.Add(30 * time.Minute),
			Sequence: 4,
		},
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "move", "evt@792417600", "--by", "1h", "--scope", "this", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.updateCalls != 1 {
		t.Fatalf("expected one update call, got %d", fb.updateCalls)
	}
	if fb.updateInput.Start == nil || fb.updateInput.End == nil {
		t.Fatalf("expected start/end patch")
	}
	if got, want := fb.updateInput.Start.Format(time.RFC3339), "2026-02-20T11:00:00Z"; got != want {
		t.Fatalf("start mismatch: got=%s want=%s", got, want)
	}
	if got, want := fb.updateInput.End.Format(time.RFC3339), "2026-02-20T11:30:00Z"; got != want {
		t.Fatalf("end mismatch: got=%s want=%s", got, want)
	}
	if fb.updateInput.Scope != backend.ScopeThis {
		t.Fatalf("scope mismatch: got=%q want=%q", fb.updateInput.Scope, backend.ScopeThis)
	}
}

func TestEventsMoveValidationMatrix(t *testing.T) {
	base := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		args            []string
		backend         *scopeCaptureBackend
		wantExitCode    int
		wantUpdateCalls int
	}{
		{
			name:            "missing to and by",
			args:            []string{"events", "move", "evt@792417600", "--json"},
			backend:         &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: base, End: base.Add(30 * time.Minute)}},
			wantExitCode:    2,
			wantUpdateCalls: 0,
		},
		{
			name:            "to and by conflict",
			args:            []string{"events", "move", "evt@792417600", "--to", "today 10:00", "--by", "1h", "--json"},
			backend:         &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: base, End: base.Add(30 * time.Minute)}},
			wantExitCode:    2,
			wantUpdateCalls: 0,
		},
		{
			name:            "not found",
			args:            []string{"events", "move", "evt@792417600", "--by", "1h", "--json"},
			backend:         &scopeCaptureBackend{getErr: errors.New("missing")},
			wantExitCode:    4,
			wantUpdateCalls: 0,
		},
		{
			name:            "if-match mismatch",
			args:            []string{"events", "move", "evt@792417600", "--by", "1h", "--if-match-seq", "2", "--json"},
			backend:         &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: base, End: base.Add(30 * time.Minute), Sequence: 1}},
			wantExitCode:    7,
			wantUpdateCalls: 0,
		},
		{
			name:            "dry-run",
			args:            []string{"events", "move", "evt@792417600", "--to", "2026-02-21T09:00", "--duration", "45m", "--dry-run", "--json", "--tz", "UTC"},
			backend:         &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: base, End: base.Add(30 * time.Minute)}},
			wantExitCode:    0,
			wantUpdateCalls: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return tc.backend, nil }
			t.Cleanup(func() { backendFactory = origFactory })

			cmd := NewRootCommand()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if got := ExitCode(err); got != tc.wantExitCode {
				t.Fatalf("exit code mismatch: got=%d want=%d err=%v", got, tc.wantExitCode, err)
			}
			if tc.backend.updateCalls != tc.wantUpdateCalls {
				t.Fatalf("update calls mismatch: got=%d want=%d", tc.backend.updateCalls, tc.wantUpdateCalls)
			}
		})
	}
}

func TestEventsCopyPassesAddInput(t *testing.T) {
	base := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	fb := &scopeCaptureBackend{
		getEvent: &contract.Event{
			ID:           "evt@792417600",
			CalendarName: "Work",
			Title:        "Planning",
			Start:        base,
			End:          base.Add(30 * time.Minute),
			Location:     "Room 4A",
			Notes:        "Bring notes",
			URL:          "https://example.com",
		},
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "copy", "evt@792417600", "--to", "2026-02-21T09:00", "--tz", "UTC", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.addCalls != 1 {
		t.Fatalf("expected one add call, got %d", fb.addCalls)
	}
	if got, want := fb.addInput.Start.Format(time.RFC3339), "2026-02-21T09:00:00Z"; got != want {
		t.Fatalf("start mismatch: got=%s want=%s", got, want)
	}
	if got, want := fb.addInput.End.Format(time.RFC3339), "2026-02-21T09:30:00Z"; got != want {
		t.Fatalf("end mismatch: got=%s want=%s", got, want)
	}
	if fb.addInput.Calendar != "Work" {
		t.Fatalf("calendar mismatch: %q", fb.addInput.Calendar)
	}
}

func TestEventsCopyValidationMatrix(t *testing.T) {
	base := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		args         []string
		backend      *scopeCaptureBackend
		wantExitCode int
		wantAddCalls int
	}{
		{
			name:         "missing to",
			args:         []string{"events", "copy", "evt@792417600", "--json"},
			backend:      &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: base, End: base.Add(30 * time.Minute), CalendarName: "Work"}},
			wantExitCode: 2,
			wantAddCalls: 0,
		},
		{
			name:         "source not found",
			args:         []string{"events", "copy", "evt@792417600", "--to", "2026-02-21T09:00", "--json", "--tz", "UTC"},
			backend:      &scopeCaptureBackend{getErr: errors.New("missing")},
			wantExitCode: 4,
			wantAddCalls: 0,
		},
		{
			name:         "invalid duration",
			args:         []string{"events", "copy", "evt@792417600", "--to", "2026-02-21T09:00", "--duration", "0m", "--json", "--tz", "UTC"},
			backend:      &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: base, End: base.Add(30 * time.Minute), CalendarName: "Work"}},
			wantExitCode: 2,
			wantAddCalls: 0,
		},
		{
			name:         "dry-run",
			args:         []string{"events", "copy", "evt@792417600", "--to", "2026-02-21T09:00", "--dry-run", "--json", "--tz", "UTC"},
			backend:      &scopeCaptureBackend{getEvent: &contract.Event{ID: "evt@792417600", Start: base, End: base.Add(30 * time.Minute), CalendarName: "Work"}},
			wantExitCode: 0,
			wantAddCalls: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origFactory := backendFactory
			backendFactory = func(string) (backend.Backend, error) { return tc.backend, nil }
			t.Cleanup(func() { backendFactory = origFactory })

			cmd := NewRootCommand()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if got := ExitCode(err); got != tc.wantExitCode {
				t.Fatalf("exit code mismatch: got=%d want=%d err=%v", got, tc.wantExitCode, err)
			}
			if tc.backend.addCalls != tc.wantAddCalls {
				t.Fatalf("add calls mismatch: got=%d want=%d", tc.backend.addCalls, tc.wantAddCalls)
			}
		})
	}
}
