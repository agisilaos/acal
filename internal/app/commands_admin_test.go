package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type adminBackend struct {
	checks    []contract.DoctorCheck
	doctorErr error
	calendars []contract.Calendar
	listErr   error
}

func (b *adminBackend) Doctor(context.Context) ([]contract.DoctorCheck, error) {
	return b.checks, b.doctorErr
}

func (b *adminBackend) ListCalendars(context.Context) ([]contract.Calendar, error) {
	return b.calendars, b.listErr
}

func (b *adminBackend) ListEvents(context.Context, backend.EventFilter) ([]contract.Event, error) {
	return nil, nil
}

func (b *adminBackend) GetEventByID(context.Context, string) (*contract.Event, error) {
	return nil, nil
}

func (b *adminBackend) GetReminderOffset(context.Context, string) (*time.Duration, error) {
	return nil, nil
}

func (b *adminBackend) AddEvent(context.Context, backend.EventCreateInput) (*contract.Event, error) {
	return nil, nil
}

func (b *adminBackend) UpdateEvent(context.Context, string, backend.EventUpdateInput) (*contract.Event, error) {
	return nil, nil
}

func (b *adminBackend) DeleteEvent(context.Context, string, backend.RecurrenceScope) error {
	return nil
}

func TestVersionCommand(t *testing.T) {
	SetBuildInfo("v9.9.9", "abc", "2026-02-17T00:00:00Z")
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "acal v9.9.9 (abc) 2026-02-17T00:00:00Z") {
		t.Fatalf("unexpected version output: %q", got)
	}
}

func TestCompletionInvalidShellExitCode(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"completion", "tcsh"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit code mismatch: got=%d want=2", code)
	}
}

func TestDoctorAndCalendarsCommands(t *testing.T) {
	fb := &adminBackend{
		checks: []contract.DoctorCheck{{Name: "osascript", Status: "ok"}},
		calendars: []contract.Calendar{
			{ID: "cal-1", Name: "Work", Writable: true},
		},
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"doctor", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("doctor failed: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "\"command\": \"doctor\"") {
		t.Fatalf("unexpected doctor output: %q", got)
	}

	out.Reset()
	cmd = NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"calendars", "list", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("calendars list failed: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "\"name\": \"Work\"") {
		t.Fatalf("unexpected calendars output: %q", got)
	}
}

func TestDoctorFailureProducesSinglePayload(t *testing.T) {
	fb := &adminBackend{
		checks: []contract.DoctorCheck{{Name: "osascript", Status: "fail"}},
		doctorErr: errors.New("osascript missing"),
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"doctor", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected doctor error")
	}
	if code := ExitCode(err); code != 6 {
		t.Fatalf("exit code mismatch: got=%d want=6", code)
	}
	if got := errOut.String(); got != "" {
		t.Fatalf("expected no stderr payload, got: %q", got)
	}
	if got := out.String(); !strings.Contains(got, "\"warnings\": [") {
		t.Fatalf("expected warnings in doctor payload: %q", got)
	}
}

func TestStatusCommand(t *testing.T) {
	fb := &adminBackend{
		checks: []contract.DoctorCheck{
			{Name: "osascript", Status: "ok"},
			{Name: "calendar_access", Status: "ok"},
			{Name: "calendar_db", Status: "ok"},
			{Name: "calendar_db_read", Status: "ok"},
		},
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "\"command\": \"status\"") {
		t.Fatalf("expected status command in output: %q", got)
	}
	if !strings.Contains(got, "\"ready\": true") {
		t.Fatalf("expected ready=true in output: %q", got)
	}
}

func TestStatusCommandNotReadyExitCode(t *testing.T) {
	fb := &adminBackend{
		checks: []contract.DoctorCheck{
			{Name: "osascript", Status: "fail"},
		},
		doctorErr: errors.New("osascript missing"),
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected status error")
	}
	if code := ExitCode(err); code != 6 {
		t.Fatalf("exit code mismatch: got=%d want=6", code)
	}
}

func TestStatusCommandReportsEffectiveOutputMode(t *testing.T) {
	fb := &adminBackend{
		checks: []contract.DoctorCheck{
			{Name: "osascript", Status: "ok"},
			{Name: "calendar_access", Status: "ok"},
			{Name: "calendar_db", Status: "ok"},
			{Name: "calendar_db_read", Status: "ok"},
		},
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"output_mode": "json"`) {
		t.Fatalf("expected effective output_mode json in non-tty, got: %q", got)
	}
}
