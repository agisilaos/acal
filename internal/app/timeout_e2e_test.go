package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type blockingBackend struct{}

func (b *blockingBackend) Doctor(context.Context) ([]contract.DoctorCheck, error) {
	return []contract.DoctorCheck{{Name: "ok", Status: "ok"}}, nil
}

func (b *blockingBackend) ListCalendars(context.Context) ([]contract.Calendar, error) {
	return nil, nil
}

func (b *blockingBackend) ListEvents(ctx context.Context, _ backend.EventFilter) ([]contract.Event, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b *blockingBackend) GetEventByID(context.Context, string) (*contract.Event, error) {
	return nil, nil
}

func (b *blockingBackend) GetReminderOffset(context.Context, string) (*time.Duration, error) {
	return nil, nil
}

func (b *blockingBackend) AddEvent(ctx context.Context, _ backend.EventCreateInput) (*contract.Event, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b *blockingBackend) UpdateEvent(context.Context, string, backend.EventUpdateInput) (*contract.Event, error) {
	return nil, nil
}

func (b *blockingBackend) DeleteEvent(context.Context, string, backend.RecurrenceScope) error {
	return nil
}

func TestEventsListTimeoutIncludesBackendPhase(t *testing.T) {
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &blockingBackend{}, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"events", "list", "--from", "today", "--to", "+1d", "--timeout", "1ns", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if code := ExitCode(err); code != 6 {
		t.Fatalf("exit code mismatch: got=%d want=6", code)
	}
	got := out.String() + errOut.String()
	if !strings.Contains(got, "backend.list_events timed out") {
		t.Fatalf("expected backend phase timeout in output, got: %q", got)
	}
}

func TestEventsAddTimeoutIncludesBackendPhase(t *testing.T) {
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &blockingBackend{}, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{
		"events", "add",
		"--calendar", "Work",
		"--title", "Load test",
		"--start", "2026-02-20T09:00:00Z",
		"--duration", "30m",
		"--timeout", "1ns",
		"--json",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if code := ExitCode(err); code != 1 {
		t.Fatalf("exit code mismatch: got=%d want=1 err=%v out=%q errOut=%q", code, err, out.String(), errOut.String())
	}
	got := out.String() + errOut.String()
	if !strings.Contains(got, "backend.add_event timed out") {
		t.Fatalf("expected backend phase timeout in output, got: %q", got)
	}
}
