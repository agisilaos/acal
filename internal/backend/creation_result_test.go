package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreationDoesNotRetryNativeMutation(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "calls")
	t.Setenv("ACAL_CREATE_TEST_COUNT", count)
	t.Setenv("ACAL_OSASCRIPT_RETRIES", "3")
	t.Setenv("ACAL_OSASCRIPT_RETRY_BACKOFF", "1ms")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Each invocation represents a creation followed by failed native readback.
	// The stub never launches Calendar.
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte("#!/bin/sh\nprintf x >> \"$ACAL_CREATE_TEST_COUNT\"\necho 'AppleEvent timed out (-1712)'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	item, err := NewOsaScriptBackend().AddEvent(context.Background(), EventCreateInput{Calendar: "Work", Title: "Review", Start: start, End: start.Add(time.Hour)})
	if item != nil || err == nil {
		t.Fatalf("item=%v error=%v", item, err)
	}
	raw, err := os.ReadFile(count)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "x" {
		t.Fatalf("creation attempts=%q, want one", raw)
	}
}
