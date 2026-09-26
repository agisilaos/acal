package app

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestReminderMinuteParsing(t *testing.T) {
	for _, input := range []string{"15m", "-15m", "+15m", "900s", " 0.25h "} {
		got, err := normalizeReminderOffset(input)
		if err != nil || got != -15*time.Minute {
			t.Errorf("%q: offset=%v error=%v", input, got, err)
		}
	}
	for _, input := range []string{"0", "0m", "30s", "-30s", "90s", "-90s", "1m1ns"} {
		if _, err := normalizeReminderOffset(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestFractionalReminderPreservesHistoryBeforeBackendAccess(t *testing.T) {
	for _, offset := range []string{"30s", "-30s", "90s", "-90s"} {
		t.Run(offset, func(t *testing.T) {
			setupReminderHistory(t)
			if err := writeHistory([]historyEntry{{Type: "add", EventID: "older"}}); err != nil {
				t.Fatal(err)
			}
			if err := writeRedoHistory([]historyEntry{{Type: "add", EventID: "redo"}}); err != nil {
				t.Fatal(err)
			}
			historyBefore, err := os.ReadFile(historyFilePath())
			if err != nil {
				t.Fatal(err)
			}
			redoBefore, err := os.ReadFile(redoFilePath())
			if err != nil {
				t.Fatal(err)
			}
			output, err := runReminderHistoryCommand(t, &strictNoCallBackend{}, "events", "remind", "evt", "--at="+offset, "--json")
			if ExitCode(err) != 2 || !strings.Contains(err.Error(), "whole number of minutes") {
				t.Fatalf("output=%s error=%v", output, err)
			}
			assertHistoryFiles(t, historyBefore, redoBefore)
		})
	}
}
