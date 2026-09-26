package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func TestHistoryStoragePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	entry := historyEntry{Type: "add", EventID: "private", Created: &contract.Event{Title: "Private meeting", Notes: "Sensitive notes"}}
	cases := []struct {
		name string
		path func() string
		run  func() error
		read func() ([]historyEntry, error)
	}{
		{"append", historyFilePath, func() error { return appendHistory(entry) }, readHistory},
		{"rewrite", historyFilePath, func() error { return writeHistory([]historyEntry{entry}) }, readHistory},
		{"redo", redoFilePath, func() error { return writeRedoHistory([]historyEntry{entry}) }, readRedoHistory},
	}
	for _, tc := range cases {
		for _, existing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%t", tc.name, existing), func(t *testing.T) {
				root := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", root)
				if err := os.Chmod(root, 0o755); err != nil {
					t.Fatal(err)
				}
				path := tc.path()
				if existing {
					seedLegacyHistory(t, path)
					if tc.name == "append" {
						seedLegacyHistory(t, redoFilePath())
					}
				}
				if err := tc.run(); err != nil {
					t.Fatal(err)
				}
				assertHistoryMode(t, root, 0o755)
				assertHistoryMode(t, filepath.Dir(path), 0o700)
				assertHistoryMode(t, path, 0o600)
				if tc.name == "append" {
					assertHistoryMode(t, redoFilePath(), 0o600)
					if raw, err := os.ReadFile(redoFilePath()); err != nil || len(raw) != 0 {
						t.Fatalf("append did not clear redo history: %q, %v", raw, err)
					}
				}
				entries, err := tc.read()
				if err != nil {
					t.Fatal(err)
				}
				wantCount := 1
				if existing && tc.name == "append" {
					wantCount = 2
				}
				if len(entries) != wantCount || entries[len(entries)-1].Created == nil || *entries[len(entries)-1].Created != *entry.Created {
					t.Fatalf("snapshot did not round trip: %+v", entries)
				}
			})
		}
	}
}

func TestHistoryReadsTightenLegacyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	for _, tc := range []struct {
		name string
		path func() string
		read func() ([]historyEntry, error)
	}{
		{"history", historyFilePath, readHistory},
		{"page", historyFilePath, func() ([]historyEntry, error) {
			entries, _, err := readHistoryPage(1, 0)
			return entries, err
		}},
		{"redo", redoFilePath, readRedoHistory},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path := tc.path()
			// Reading missing history must remain a no-op, without creating storage.
			if entries, err := tc.read(); err != nil || len(entries) != 0 {
				t.Fatalf("missing history: %v, %v", entries, err)
			}
			if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
				t.Fatalf("read created storage: %v", err)
			}
			original := seedLegacyHistory(t, path)
			entries, err := tc.read()
			if err != nil || len(entries) != 1 || entries[0].EventID != "legacy" {
				t.Fatalf("legacy history: %v, %v", entries, err)
			}
			assertHistoryMode(t, filepath.Dir(path), 0o700)
			assertHistoryMode(t, path, 0o600)
			raw, err := os.ReadFile(path)
			if err != nil || string(raw) != original {
				t.Fatalf("read changed snapshot contents: %q, %v", raw, err)
			}
		})
	}
}

func TestHistoryRejectsDirectoryWithoutChangingItsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement Unix permission bits")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := historyFilePath()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readHistory(); err == nil {
		t.Fatal("expected error reading a directory as history")
	}
	if err := writeHistory(nil); err == nil {
		t.Fatal("expected error writing a directory as history")
	}
	assertHistoryMode(t, path, 0o755)
}

func seedLegacyHistory(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const raw = "{\"type\":\"add\",\"event_id\":\"legacy\",\"created\":{\"title\":\"Old private meeting\"}}\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertHistoryMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s permissions = %04o, want %04o", path, got, want)
	}
}

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

func TestHistoryListPagination(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for i := 1; i <= 3; i++ {
		if err := appendHistory(historyEntry{
			At:      time.Date(2026, 2, 18, 9, i, 0, 0, time.UTC),
			Type:    "add",
			EventID: fmt.Sprintf("e%d", i),
		}); err != nil {
			t.Fatalf("appendHistory failed: %v", err)
		}
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"history", "list", "--json", "--limit", "1", "--offset", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("history list failed: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "\"event_id\": \"e2\"") {
		t.Fatalf("expected second-most-recent event in paged output, got: %q", got)
	}
	if !strings.Contains(got, "\"has_more\": true") || !strings.Contains(got, "\"next_offset\": 2") {
		t.Fatalf("expected pagination metadata, got: %q", got)
	}
}
