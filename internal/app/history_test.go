package app

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestHistoryAppendRead(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := appendHistory(historyEntry{At: time.Now().UTC(), Type: "add", EventID: "e1"}); err != nil {
		t.Fatalf("appendHistory failed: %v", err)
	}
	entries, err := readHistory()
	if err != nil {
		t.Fatalf("readHistory failed: %v", err)
	}
	if len(entries) != 1 || entries[0].EventID != "e1" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestUndoLastHistoryAdd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fb := &scopeCaptureBackend{}
	if err := appendHistory(historyEntry{At: time.Now().UTC(), Type: "add", EventID: "e1@1"}); err != nil {
		t.Fatalf("appendHistory failed: %v", err)
	}
	_, _, err := undoLastHistory(context.Background(), fb, false)
	if err != nil {
		t.Fatalf("undoLastHistory failed: %v", err)
	}
	if fb.deleteCalls != 1 {
		t.Fatalf("expected one delete call, got %d", fb.deleteCalls)
	}
}

func TestHistoryUndoCommandDryRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := appendHistory(historyEntry{At: time.Now().UTC(), Type: "add", EventID: "e1@1"}); err != nil {
		t.Fatalf("appendHistory failed: %v", err)
	}
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"history", "undo", "--dry-run", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.deleteCalls != 0 {
		t.Fatalf("expected no delete calls in dry-run")
	}
}

func TestRedoLastHistoryAdd(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ev := &contract.Event{
		ID:           "e1@1",
		CalendarName: "Work",
		Title:        "Standup",
		Start:        time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 2, 20, 9, 30, 0, 0, time.UTC),
	}
	if err := appendHistory(historyEntry{At: time.Now().UTC(), Type: "add", EventID: ev.ID, Created: ev}); err != nil {
		t.Fatalf("appendHistory failed: %v", err)
	}
	fb := &scopeCaptureBackend{}
	if _, _, err := undoLastHistory(context.Background(), fb, false); err != nil {
		t.Fatalf("undoLastHistory failed: %v", err)
	}
	if _, _, err := redoLastHistory(context.Background(), fb, false); err != nil {
		t.Fatalf("redoLastHistory failed: %v", err)
	}
	if fb.addCalls != 1 {
		t.Fatalf("expected one add call on redo, got %d", fb.addCalls)
	}
}

func TestHistoryRedoCommandDryRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ev := &contract.Event{
		ID:           "e1@1",
		CalendarName: "Work",
		Title:        "Standup",
		Start:        time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC),
		End:          time.Date(2026, 2, 20, 9, 30, 0, 0, time.UTC),
	}
	if err := appendHistory(historyEntry{At: time.Now().UTC(), Type: "add", EventID: ev.ID, Created: ev}); err != nil {
		t.Fatalf("appendHistory failed: %v", err)
	}
	fb := &scopeCaptureBackend{}
	if _, _, err := undoLastHistory(context.Background(), fb, false); err != nil {
		t.Fatalf("undoLastHistory failed: %v", err)
	}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"history", "redo", "--dry-run", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if fb.addCalls != 0 {
		t.Fatalf("expected no add calls in dry-run")
	}
}
