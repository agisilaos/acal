package app

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type scopeCaptureBackend struct {
	updateInput backend.EventUpdateInput
	deleteScope backend.RecurrenceScope
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
	return &contract.Event{ID: "evt@792417600", Start: time.Now()}, nil
}

func (b *scopeCaptureBackend) AddEvent(context.Context, backend.EventCreateInput) (*contract.Event, error) {
	return nil, nil
}

func (b *scopeCaptureBackend) UpdateEvent(_ context.Context, id string, in backend.EventUpdateInput) (*contract.Event, error) {
	b.updateInput = in
	return &contract.Event{ID: id}, nil
}

func (b *scopeCaptureBackend) DeleteEvent(_ context.Context, _ string, scope backend.RecurrenceScope) error {
	b.deleteScope = scope
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
