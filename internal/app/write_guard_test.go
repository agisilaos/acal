package app

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/agis/acal/internal/backend"
	"github.com/agis/acal/internal/contract"
)

type guardedTestBackend struct {
	scopeCaptureBackend
	reason string
}

func (b *guardedTestBackend) CheckEventWrite(context.Context, string) error {
	return &backend.WriteRejectedError{Reason: b.reason}
}

func runGuardCommand(t *testing.T, be backend.Backend, args ...string) (string, error) {
	t.Helper()
	old := backendFactory
	backendFactory = func(string) (backend.Backend, error) { return be, nil }
	defer func() { backendFactory = old }()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestWriteRejectionCLI(t *testing.T) {
	for _, reason := range []string{"recurring", "permission", "unclassified"} {
		for _, args := range [][]string{{"events", "update", "evt@12345", "--title", "new"}, {"events", "move", "evt@12345", "--by", "1h"}, {"events", "delete", "evt@12345", "--force"}, {"events", "remind", "evt@12345", "--clear"}} {
			t.Run(reason+"/"+args[1], func(t *testing.T) {
				setupReminderHistory(t)
				if err := writeHistory(nil); err != nil {
					t.Fatal(err)
				}
				if err := writeRedoHistory(nil); err != nil {
					t.Fatal(err)
				}
				before, _ := os.ReadFile(historyFilePath())
				redoBefore, _ := os.ReadFile(redoFilePath())
				be := &guardedTestBackend{reason: reason}
				out, err := runGuardCommand(t, be, append(args, "--json")...)
				wantExit := 6
				code := "BACKEND_UNAVAILABLE"
				if reason == "recurring" {
					wantExit = 2
					code = "UNSUPPORTED_OPERATION"
				}
				if reason == "permission" {
					code = "PERMISSION_DENIED"
				}
				if ExitCode(err) != wantExit || !strings.Contains(out, code) || !strings.Contains(out, `"applied": false`) || strings.Contains(out, "UPDATE_OUTCOME_UNKNOWN") {
					t.Fatalf("err=%v out=%s", err, out)
				}
				if be.getCalls+be.updateCalls+be.deleteCalls+be.remindCalls != 0 {
					t.Fatalf("preflight touched event: %+v", be)
				}
				assertHistoryFiles(t, before, redoBefore)
			})
		}
	}
}

func TestRejectedReplayPreservesBothStacks(t *testing.T) {
	for _, operation := range []string{"undo", "redo"} {
		for _, kind := range []string{"update", "reminder", "delete"} {
			for _, reason := range []string{"recurring", "permission", "unclassified"} {
				t.Run(operation+"/"+kind+"/"+reason, func(t *testing.T) {
					setupReminderHistory(t)
					e := &contract.Event{ID: "evt@12345"}
					entry := historyEntry{Independent: true, Type: kind, EventID: e.ID, Prev: e, Next: e, ReminderBefore: &reminderSnapshot{}, ReminderAfter: &reminderSnapshot{}}
					// Undo-add and redo-delete both invoke the native delete guard.
					if kind == "delete" && operation == "undo" {
						entry.Type = "add"
					}
					if err := writeHistory([]historyEntry{entry}); err != nil {
						t.Fatal(err)
					}
					if err := writeRedoHistory([]historyEntry{entry}); err != nil {
						t.Fatal(err)
					}
					before, _ := os.ReadFile(historyFilePath())
					redoBefore, _ := os.ReadFile(redoFilePath())
					rejected := &backend.WriteRejectedError{Reason: reason}
					be := &scopeCaptureBackend{updateErr: rejected, deleteErr: rejected}
					out, err := runGuardCommand(t, be, "history", operation, "--json")
					if err == nil || !strings.Contains(out, "write_rejected") || !strings.Contains(out, `"applied": false`) {
						t.Fatalf("err=%v out=%s", err, out)
					}
					assertHistoryFiles(t, before, redoBefore)
				})
			}
		}
	}
}

func TestReadAndPreviewDoNotRequireWriteClassification(t *testing.T) {
	for _, args := range [][]string{{"events", "show", "evt@12345"}, {"events", "remind", "evt@12345", "--clear", "--dry-run"}} {
		t.Run(args[1], func(t *testing.T) {
			be := &guardedTestBackend{reason: "permission"}
			out, err := runGuardCommand(t, be, append(args, "--json")...)
			if err != nil || be.getCalls != 1 || be.updateCalls != 0 {
				t.Fatalf("err=%v reads=%d writes=%d out=%s", err, be.getCalls, be.updateCalls, out)
			}
		})
	}
}
