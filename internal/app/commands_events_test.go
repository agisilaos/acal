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
	getEvent    *contract.Event
	getErr      error
	updateErr   error
	deleteErr   error
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

func (b *scopeCaptureBackend) AddEvent(context.Context, backend.EventCreateInput) (*contract.Event, error) {
	return nil, nil
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
