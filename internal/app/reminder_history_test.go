package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

func setupReminderHistory(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(historyFilePath()), 0700); err != nil {
		t.Fatal(err)
	}
}

func runReminderHistoryCommand(t *testing.T, be backend.Backend, args ...string) (string, error) {
	t.Helper()
	original := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return be, nil }
	defer func() { backendFactory = original }()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func assertReminderOffset(t *testing.T, got, want *time.Duration) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("offset = %v, want %v", got, want)
	}
}

func TestReminderHistoryTransitions(t *testing.T) {
	a, b, zero, positive := -15*time.Minute, -30*time.Minute, time.Duration(0), 10*time.Minute
	for _, tc := range []struct {
		name          string
		before, after *time.Duration
		args          []string
	}{
		{"none to offset", nil, &a, []string{"--at", "15m"}},
		{"offset to offset", &a, &b, []string{"--at", "30m"}},
		{"offset to none", &a, nil, []string{"--clear"}},
		{"zero to none", &zero, nil, []string{"--clear"}},
		{"positive to offset", &positive, &a, []string{"--at", "15m"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupReminderHistory(t)
			fb := &scopeCaptureBackend{reminder: tc.before}
			args := append([]string{"events", "remind", "evt@792417600", "--json"}, tc.args...)
			if _, err := runReminderHistoryCommand(t, fb, args...); err != nil {
				t.Fatal(err)
			}
			if fb.remindCalls != 2 {
				t.Fatalf("reminder reads = %d, want prior + verification", fb.remindCalls)
			}
			entries, err := readHistory()
			if err != nil || len(entries) != 1 {
				t.Fatalf("history = %+v, err=%v", entries, err)
			}
			entry := entries[0]
			if entry.Type != "reminder" || entry.EventID != "evt@792417600" || entry.Prev != nil || entry.Next != nil || entry.ReminderBefore == nil || entry.ReminderAfter == nil {
				t.Fatalf("bad reminder history: %+v", entry)
			}
			assertReminderOffset(t, entry.ReminderBefore.Offset, tc.before)
			assertReminderOffset(t, entry.ReminderAfter.Offset, tc.after)
			for _, step := range []struct {
				command string
				want    *time.Duration
			}{{"undo", tc.before}, {"redo", tc.after}} {
				output, err := runReminderHistoryCommand(t, fb, "history", step.command, "--json")
				if err != nil {
					t.Fatal(err)
				}
				assertReminderOffset(t, fb.reminder, step.want)
				wantPatch := backend.EventUpdateInput{Scope: backend.ScopeAuto, ReminderOffset: step.want, ClearReminder: step.want == nil}
				if !reflect.DeepEqual(fb.updateInput, wantPatch) {
					t.Fatalf("%s sent unrelated fields: %+v", step.command, fb.updateInput)
				}
				var envelope struct {
					Schema string       `json:"schema_version"`
					Data   historyEntry `json:"data"`
				}
				if err := json.Unmarshal([]byte(output), &envelope); err != nil {
					t.Fatal(err)
				}
				if envelope.Schema != "v1" || envelope.Data.Type != "reminder" || envelope.Data.ReminderBefore == nil || envelope.Data.ReminderAfter == nil {
					t.Fatalf("bad public output: %s", output)
				}
				assertReminderOffset(t, envelope.Data.ReminderBefore.Offset, tc.before)
				assertReminderOffset(t, envelope.Data.ReminderAfter.Offset, tc.after)
			}
			if fb.updateCalls != 3 || fb.remindCalls != 4 {
				t.Fatalf("calls: updates=%d, reads=%d", fb.updateCalls, fb.remindCalls)
			}
			entries, err = readHistory()
			redo, redoErr := readRedoHistory()
			if err != nil || redoErr != nil || len(entries) != 1 || len(redo) != 0 {
				t.Fatalf("stacks after redo: history=%+v redo=%+v errors=%v %v", entries, redo, err, redoErr)
			}
		})
	}
}

func TestReminderProducerFailurePreservesHistory(t *testing.T) {
	for _, tc := range []struct {
		name        string
		readErrorAt int
		updateError bool
		wantUpdates int
	}{
		{"prior read", 1, false, 0}, {"update", 0, true, 1}, {"verification read", 2, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupReminderHistory(t)
			if err := writeHistory([]historyEntry{{Type: "add", EventID: "older"}}); err != nil {
				t.Fatal(err)
			}
			if err := writeRedoHistory([]historyEntry{{Type: "add", EventID: "redo"}}); err != nil {
				t.Fatal(err)
			}
			historyBefore, _ := os.ReadFile(historyFilePath())
			redoBefore, _ := os.ReadFile(redoFilePath())
			fb := &scopeCaptureBackend{}
			if tc.readErrorAt > 0 {
				fb.remindErr = errors.New("read failed")
				fb.remindErrAt = tc.readErrorAt
			}
			if tc.updateError {
				fb.updateErr = errors.New("write failed")
			}
			if _, err := runReminderHistoryCommand(t, fb, "events", "remind", "evt", "--at", "15m", "--json"); ExitCode(err) != 1 {
				t.Fatalf("want failure, got %v", err)
			}
			if fb.updateCalls != tc.wantUpdates {
				t.Fatalf("updates=%d, want %d", fb.updateCalls, tc.wantUpdates)
			}
			assertHistoryFiles(t, historyBefore, redoBefore)
		})
	}
}

func assertHistoryFiles(t *testing.T, historyWant, redoWant []byte) {
	t.Helper()
	for _, pair := range []struct {
		path string
		want []byte
	}{{historyFilePath(), historyWant}, {redoFilePath(), redoWant}} {
		got, err := os.ReadFile(pair.path)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if !bytes.Equal(got, pair.want) {
			t.Fatalf("%s changed: %s", pair.path, got)
		}
	}
}

// Simulates a backend accepting a patch without changing the alarm.
type ignoredReminderBackend struct{ scopeCaptureBackend }

func (b *ignoredReminderBackend) UpdateEvent(ctx context.Context, id string, in backend.EventUpdateInput) (*contract.Event, error) {
	before := b.reminder
	event, err := b.scopeCaptureBackend.UpdateEvent(ctx, id, in)
	b.reminder = before
	return event, err
}

func TestReminderReplayFailuresPreserveStacks(t *testing.T) {
	offset := -15 * time.Minute
	entry := historyEntry{Type: "reminder", EventID: "evt", ReminderBefore: &reminderSnapshot{Offset: &offset}, ReminderAfter: &reminderSnapshot{Offset: &offset}}
	for _, operation := range []string{"undo", "redo"} {
		for _, failure := range []string{"update", "readback", "mismatch"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				setupReminderHistory(t)
				if err := writeHistory([]historyEntry{entry}); err != nil {
					t.Fatal(err)
				}
				if err := writeRedoHistory([]historyEntry{entry}); err != nil {
					t.Fatal(err)
				}
				historyBefore, _ := os.ReadFile(historyFilePath())
				redoBefore, _ := os.ReadFile(redoFilePath())
				fb := &ignoredReminderBackend{}
				if failure == "update" {
					fb.updateErr = errors.New("write failed")
				}
				if failure == "readback" {
					fb.remindErr = errors.New("read failed")
				}
				if _, err := runReminderHistoryCommand(t, fb, "history", operation, "--json"); ExitCode(err) != 1 {
					t.Fatalf("want failure, got %v", err)
				}
				if fb.updateCalls != 1 {
					t.Fatalf("updates=%d", fb.updateCalls)
				}
				assertHistoryFiles(t, historyBefore, redoBefore)
			})
		}
	}
}

func TestMalformedReminderHistory(t *testing.T) {
	for _, payload := range []string{
		`{"type":"reminder","event_id":"evt","reminder_after":{}}`,
		`{"type":"reminder","event_id":"evt","reminder_before":{}}`,
		`{"type":"reminder","event_id":"evt","reminder_before":null,"reminder_after":{}}`,
		`{"type":"reminder","event_id":"  ","reminder_before":{},"reminder_after":{}}`,
		`{"type":"reminder","event_id":"evt","reminder_before":{"offset_ns":"bad"},"reminder_after":{}}`,
		`{"type":"reminder","event_id":"evt","reminder_before":{},"reminder_after":{"offset_ns":1.5}}`,
		`{"type":"reminder","event_id":"evt","reminder_before":{},"reminder_after":{"offset_ns":9223372036854775808}}`,
		`{"type":"reminder","event_id":"evt","reminder_before":[],"reminder_after":{}}`,
	} {
		for _, operation := range []string{"undo", "redo", "list"} {
			for _, dryRun := range []bool{false, true} {
				if operation == "list" && dryRun {
					continue
				}
				t.Run(operation+"/"+payload+"/dry="+fmt.Sprint(dryRun), func(t *testing.T) {
					setupReminderHistory(t)
					raw := []byte(`{"type":"add","event_id":"older"}` + "\n" + payload + "\n")
					if err := os.WriteFile(historyFilePath(), raw, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(redoFilePath(), raw, 0600); err != nil {
						t.Fatal(err)
					}
					fb := &scopeCaptureBackend{}
					args := []string{"history", operation, "--json"}
					if dryRun {
						args = append(args, "--dry-run")
					}
					if _, err := runReminderHistoryCommand(t, fb, args...); err == nil {
						t.Fatal("malformed reminder accepted")
					}
					if fb.updateCalls != 0 || fb.deleteCalls != 0 || fb.addCalls != 0 || fb.remindCalls != 0 {
						t.Fatal("malformed payload reached backend")
					}
					assertHistoryFiles(t, raw, raw)
				})
			}
		}
	}
}

func TestReminderHistoryDryRunAndJSON(t *testing.T) {
	setupReminderHistory(t)
	// Absent offset and explicit null both mean known-none; zero is an actual alarm.
	raw := []byte(`{"at":"2026-02-20T09:00:00Z","type":"reminder","event_id":"evt","reminder_before":{},"reminder_after":{"offset_ns":0}}` + "\n")
	if err := os.WriteFile(historyFilePath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(redoFilePath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	fb := &strictNoCallBackend{}
	for _, args := range [][]string{{"list", "--json"}, {"list", "--jsonl"}, {"undo", "--dry-run", "--json"}, {"redo", "--dry-run", "--json"}} {
		output, err := runReminderHistoryCommand(t, fb, append([]string{"history"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		compact := new(bytes.Buffer)
		if err := json.Compact(compact, []byte(output)); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"type":"reminder"`, `"reminder_before":{"offset_ns":null}`, `"reminder_after":{"offset_ns":0}`} {
			if !strings.Contains(compact.String(), want) {
				t.Fatalf("missing %s in %s", want, output)
			}
		}
	}
	assertHistoryFiles(t, raw, raw)
}

func TestLegacyHistoryFixtures(t *testing.T) {
	for _, operation := range []string{"undo", "redo"} {
		t.Run(operation, func(t *testing.T) {
			setupReminderHistory(t)
			name := "history_legacy.jsonl"
			path := historyFilePath()
			if operation == "redo" {
				name = "redo_legacy.jsonl"
				path = redoFilePath()
			}
			raw, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			offset := -10 * time.Minute
			fb := &scopeCaptureBackend{reminder: &offset}
			// Legacy update records (even old remind writes) retain ordinary-field replay.
			for range 3 {
				if _, err := runReminderHistoryCommand(t, fb, "history", operation, "--json"); err != nil {
					t.Fatal(err)
				}
			}
			if fb.updateCalls != 1 || fb.addCalls != 1 || fb.deleteCalls != 1 || fb.remindCalls != 0 {
				t.Fatalf("unexpected legacy replay calls: %+v", fb)
			}
			wantTitle := "Original"
			if operation == "redo" {
				wantTitle = "Updated"
			}
			if fb.updateInput.Title == nil || *fb.updateInput.Title != wantTitle {
				t.Fatalf("legacy %s title = %v", operation, fb.updateInput.Title)
			}
			if fb.updateInput.ReminderOffset != nil || fb.updateInput.ClearReminder {
				t.Fatal("legacy update tried to infer an alarm")
			}
			assertReminderOffset(t, fb.reminder, &offset)
		})
	}
}

func TestReminderReplayRejectsMalformedDestination(t *testing.T) {
	valid := []byte(`{"type":"reminder","event_id":"evt","reminder_before":{},"reminder_after":{"offset_ns":-900000000000}}` + "\n")
	invalid := []byte(`{"type":"reminder","event_id":"evt"}` + "\n")
	for _, operation := range []string{"undo", "redo"} {
		for _, dryRun := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%t", operation, dryRun), func(t *testing.T) {
				setupReminderHistory(t)
				history, redo := valid, invalid
				if operation == "redo" {
					history, redo = invalid, valid
				}
				if err := os.WriteFile(historyFilePath(), history, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(redoFilePath(), redo, 0600); err != nil {
					t.Fatal(err)
				}
				args := []string{"history", operation, "--json"}
				if dryRun {
					args = append(args, "--dry-run")
				}
				if _, err := runReminderHistoryCommand(t, &strictNoCallBackend{}, args...); err == nil {
					t.Fatal("malformed destination accepted")
				}
				assertHistoryFiles(t, history, redo)
			})
		}
	}
}

func TestReminderProducerMismatchPreservesHistory(t *testing.T) {
	offset := -15 * time.Minute
	for _, tc := range []struct {
		name   string
		before *time.Duration
		args   []string
	}{
		{"set ignored", nil, []string{"--at", "15m"}},
		{"clear ignored", &offset, []string{"--clear"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupReminderHistory(t)
			fb := &ignoredReminderBackend{scopeCaptureBackend: scopeCaptureBackend{reminder: tc.before}}
			args := append([]string{"events", "remind", "evt", "--json"}, tc.args...)
			if _, err := runReminderHistoryCommand(t, fb, args...); ExitCode(err) != 1 {
				t.Fatalf("want mismatch failure, got %v", err)
			}
			if fb.updateCalls != 1 || fb.remindCalls != 2 {
				t.Fatalf("unexpected calls: %+v", fb)
			}
			assertHistoryFiles(t, nil, nil)
		})
	}
}
