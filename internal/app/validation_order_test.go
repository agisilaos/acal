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

const unexpectedBackendCall = "unexpected backend call"

type strictNoCallBackend struct{}

func (b *strictNoCallBackend) Doctor(context.Context) ([]contract.DoctorCheck, error) {
	return []contract.DoctorCheck{{Name: "ok", Status: "ok"}}, nil
}

func (b *strictNoCallBackend) ListCalendars(context.Context) ([]contract.Calendar, error) {
	return nil, errors.New(unexpectedBackendCall)
}

func (b *strictNoCallBackend) ListEvents(context.Context, backend.EventFilter) ([]contract.Event, error) {
	return nil, errors.New(unexpectedBackendCall)
}

func (b *strictNoCallBackend) GetEventByID(context.Context, string) (*contract.Event, error) {
	return nil, errors.New(unexpectedBackendCall)
}

func (b *strictNoCallBackend) GetReminderOffset(context.Context, string) (*time.Duration, error) {
	return nil, errors.New(unexpectedBackendCall)
}

func (b *strictNoCallBackend) AddEvent(context.Context, backend.EventCreateInput) (*contract.Event, error) {
	return nil, errors.New(unexpectedBackendCall)
}

func (b *strictNoCallBackend) UpdateEvent(context.Context, string, backend.EventUpdateInput) (*contract.Event, error) {
	return nil, errors.New(unexpectedBackendCall)
}

func (b *strictNoCallBackend) DeleteEvent(context.Context, string, backend.RecurrenceScope) error {
	return errors.New(unexpectedBackendCall)
}

func TestUpdateInvalidScopeFailsBeforeBackendLookup(t *testing.T) {
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"events", "update", "evt-1", "--scope", "bad", "--title", "x", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit code mismatch: got=%d want=2", code)
	}
	if got := out.String() + errOut.String(); strings.Contains(got, unexpectedBackendCall) {
		t.Fatalf("did not expect backend to be called: %q", got)
	}
}

func TestMoveInvalidFlagsFailBeforeBackendLookup(t *testing.T) {
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"events", "move", "evt-1", "--to", "2026-02-20T10:00:00Z", "--by", "30m", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit code mismatch: got=%d want=2", code)
	}
	if got := out.String() + errOut.String(); strings.Contains(got, unexpectedBackendCall) {
		t.Fatalf("did not expect backend to be called: %q", got)
	}
}

func TestCopyInvalidToFailsBeforeBackendLookup(t *testing.T) {
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"events", "copy", "evt-1", "--to", "not-a-time", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit code mismatch: got=%d want=2", code)
	}
	if got := out.String() + errOut.String(); strings.Contains(got, unexpectedBackendCall) {
		t.Fatalf("did not expect backend to be called: %q", got)
	}
}

func TestRemindInvalidOffsetFailsBeforeBackendLookup(t *testing.T) {
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"events", "remind", "evt-1", "--at", "zzz", "--json"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error")
	}
	if code := ExitCode(err); code != 2 {
		t.Fatalf("exit code mismatch: got=%d want=2", code)
	}
	if got := out.String() + errOut.String(); strings.Contains(got, unexpectedBackendCall) {
		t.Fatalf("did not expect backend to be called: %q", got)
	}
}

func TestDSTGapRejectedBeforeCreation(t *testing.T) {
	orig := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = orig })
	for _, args := range [][]string{
		{"events", "add", "--calendar", "Work", "--title", "Gap", "--start", "2026-03-29T02:30", "--duration", "30m"},
		{"quick-add", "2026-03-29 02:30 Gap @Work 30m"},
		{"events", "quick-add", "2026-03-29 02:30 Gap @Work 30m"},
	} {
		for _, preview := range []bool{false, true} {
			cmd := NewRootCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			argv := append(append([]string{}, args...), "--tz", "Europe/Berlin", "--json")
			if preview {
				argv = append(argv, "--dry-run")
			}
			cmd.SetArgs(argv)
			if code := ExitCode(cmd.Execute()); code != 2 || !strings.Contains(out.String(), "nonexistent local time") || strings.Contains(out.String(), unexpectedBackendCall) {
				t.Fatalf("%v: code=%d output=%s", argv, code, &out)
			}
		}
	}
}

func TestQuickAddInvalidDurationDoesNotWrite(t *testing.T) {
	orig := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return &strictNoCallBackend{}, nil }
	t.Cleanup(func() { backendFactory = orig })
	for _, prefix := range [][]string{{"quick-add"}, {"events", "quick-add"}} {
		for _, duration := range []string{"0m", "-30m"} {
			cmd := NewRootCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(append(append([]string{}, prefix...), "2026-10-01 09:00 Review @Work "+duration, "--json"))
			if code := ExitCode(cmd.Execute()); code != 2 || !strings.Contains(out.String(), "duration must be positive") || strings.Contains(out.String(), unexpectedBackendCall) {
				t.Fatalf("exit=%d output=%s", code, &out)
			}
		}
	}
}
