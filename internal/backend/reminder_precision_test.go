package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeReminderPrecision(t *testing.T) {
	for _, operation := range []string{"add", "update"} {
		for _, tc := range []struct {
			name   string
			offset *time.Duration
			want   string
		}{
			{"omitted", nil, "__ACAL_KEEP__"},
			{"zero", durationPointer(0), "0"},
			{"before", durationPointer(-15 * time.Minute), "-15"},
			{"after", durationPointer(15 * time.Minute), "15"},
			{"negative30s", durationPointer(-30 * time.Second), ""},
			{"positive30s", durationPointer(30 * time.Second), ""},
			{"negative90s", durationPointer(-90 * time.Second), ""},
			{"positive90s", durationPointer(90 * time.Second), ""},
			{"nanosecond", durationPointer(time.Minute + time.Nanosecond), ""},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				log := filepath.Join(dir, "args")
				t.Setenv("PATH", dir)
				t.Setenv("ACAL_TEST_ARGS", log)
				// Stop at the write boundary, before readback, and never invoke Calendar.
				stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ACAL_TEST_ARGS\"\necho stub-write-boundary >&2\nexit 1\n"
				if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte(stub), 0700); err != nil {
					t.Fatal(err)
				}
				b := &OsaScriptBackend{}
				var err error
				if operation == "add" {
					start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
					_, err = b.AddEvent(context.Background(), EventCreateInput{Calendar: "Work", Title: "Test", Start: start, End: start.Add(time.Hour), ReminderOffset: tc.offset})
				} else {
					_, err = b.UpdateEvent(context.Background(), "uid", EventUpdateInput{ReminderOffset: tc.offset, ClearReminder: tc.offset == nil})
				}
				raw, readErr := os.ReadFile(log)
				if tc.want == "" {
					if err == nil || !strings.Contains(err.Error(), "whole number of minutes") {
						t.Fatalf("error = %v", err)
					}
					var outcome *UpdateOutcomeError
					if errors.As(err, &outcome) {
						t.Fatalf("preflight rejection must not report an uncertain write: %v", err)
					}
					if !os.IsNotExist(readErr) {
						t.Fatalf("invalid offset invoked runner: %s, %v", raw, readErr)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), "stub-write-boundary") || readErr != nil {
					t.Fatalf("write boundary: err=%v read=%v", err, readErr)
				}
				args := strings.Split(strings.TrimSpace(string(raw)), "\n")
				index := len(args) - 1
				if operation == "update" {
					index--
					wantClear := "false"
					if tc.offset == nil {
						wantClear = "true"
					}
					if args[len(args)-1] != wantClear {
						t.Fatalf("clear = %q", args[len(args)-1])
					}
				}
				if args[index] != tc.want {
					t.Fatalf("minutes = %q, want %q", args[index], tc.want)
				}
			})
		}
	}
}

func durationPointer(d time.Duration) *time.Duration { return &d }
