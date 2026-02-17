package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
)

func TestExecuteBatchLineAddDryRun(t *testing.T) {
	title := "Standup"
	start := "2026-02-20T09:00"
	dur := "30m"
	res, err := executeBatchLine(context.Background(), &scopeCaptureBackend{}, batchLine{Op: "add", Calendar: "Work", Title: &title, Start: &start, Duration: &dur}, time.UTC, true)
	if err != nil {
		t.Fatalf("executeBatchLine failed: %v", err)
	}
	if res["op"] != "add" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestEventsBatchDryRun(t *testing.T) {
	fb := &scopeCaptureBackend{}
	origFactory := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return fb, nil }
	t.Cleanup(func() { backendFactory = origFactory })

	f := filepath.Join(t.TempDir(), "ops.jsonl")
	content := "{\"op\":\"add\",\"calendar\":\"Work\",\"title\":\"Plan\",\"start\":\"2026-02-20T09:00\",\"duration\":\"30m\"}\n" +
		"{\"op\":\"delete\",\"id\":\"evt@792417600\"}\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"events", "batch", "--file", f, "--dry-run", "--tz", "UTC", "--json"})
	err := cmd.Execute()
	if code := ExitCode(err); code != 0 {
		t.Fatalf("expected exit code 0, got %d err=%v", code, err)
	}
	if fb.addCalls != 0 || fb.deleteCalls != 0 || fb.updateCalls != 0 {
		t.Fatalf("expected no backend write calls in dry-run")
	}
}
