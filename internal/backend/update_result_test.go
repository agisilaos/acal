package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func nativeUpdateFields(start time.Time) []string {
	return []string{"shared-uid", "calendar-id", "Work", "Changed", strconv.FormatInt(start.Unix(), 10), strconv.FormatInt(start.Add(time.Hour).Unix(), 10), "false", " room\t\n", " notes\nwith \"quotes\" and \\ 日本語 📅 ", "https://example.com/", "7", strconv.FormatInt(start.Unix(), 10), "Europe/Berlin"}
}

func TestUpdateNativeResults(t *testing.T) {
	start := time.Date(2040, 1, 2, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		scope       RecurrenceScope
		moved       bool
		mutate      func([]string) []string
		fail        bool
		wantApplied bool
		wantOK      bool
	}{
		{name: "far future exact", wantOK: true},
		{name: "moved start", moved: true, wantOK: true},
		{name: "series representative", scope: ScopeSeries, wantOK: true},
		{name: "future representative", scope: ScopeFuture, wantOK: true},
		{name: "same UID wrong occurrence", mutate: func(f []string) []string {
			f[4] = strconv.FormatInt(start.Add(24*time.Hour).Unix(), 10)
			f[5] = strconv.FormatInt(start.Add(25*time.Hour).Unix(), 10)
			return f
		}, wantApplied: true},
		{name: "exact identity stale state", mutate: func(f []string) []string { f[3] = "Old title"; return f }, wantApplied: true},
		{name: "partial", mutate: func(f []string) []string { return f[:8] }, wantApplied: true},
		{name: "absent", mutate: func(f []string) []string { return nil }, wantApplied: true},
		{name: "write transport failure", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fields := nativeUpdateFields(start)
			if tc.mutate != nil {
				fields = tc.mutate(fields)
			}
			raw, _ := json.Marshal(fields)
			stubLookupAppleScript(t, string(raw), tc.fail)
			title := "Changed"
			in := EventUpdateInput{Title: &title, Scope: tc.scope}
			oldStart := start
			if tc.moved {
				oldStart = start.Add(-24 * time.Hour)
				in.Start = &start
			}
			event, err := NewOsaScriptBackend().UpdateEvent(context.Background(), fmt.Sprintf("shared-uid@%d", oldStart.Unix()-cocoaEpochOffset), in)
			if tc.wantOK {
				if err != nil || event == nil {
					t.Fatalf("event=%+v err=%v", event, err)
				}
				if event.ID != fmt.Sprintf("shared-uid@%d", start.Unix()-cocoaEpochOffset) || event.Notes != fields[8] || event.Location != fields[7] || event.URL != fields[9] {
					t.Fatalf("lossy result: %+v", event)
				}
			} else {
				var outcome *UpdateOutcomeError
				if event != nil || !errors.As(err, &outcome) || outcome.Applied != tc.wantApplied {
					t.Fatalf("event=%+v err=%v", event, err)
				}
			}
		})
	}
}

func TestUpdateDoesNotRetryNativeMutation(t *testing.T) {
	dir := t.TempDir()
	count := filepath.Join(dir, "calls")
	t.Setenv("ACAL_UPDATE_TEST_COUNT", count)
	t.Setenv("ACAL_OSASCRIPT_RETRIES", "3")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(dir, "osascript"), []byte("#!/bin/sh\nprintf x >> \"$ACAL_UPDATE_TEST_COUNT\"\necho 'AppleEvent timed out (-1712)'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := NewOsaScriptBackend().UpdateEvent(context.Background(), "uid@792417600", EventUpdateInput{})
	var outcome *UpdateOutcomeError
	if !errors.As(err, &outcome) || outcome.Applied {
		t.Fatalf("error=%v", err)
	}
	raw, _ := os.ReadFile(count)
	if string(raw) != "x" {
		t.Fatalf("write attempts=%q", raw)
	}
}

// Execute only the serialization handlers, with no Calendar application access.
func TestUpdateJSONSerializationAppleScript(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("AppleScript interpreter requires macOS")
	}
	fields := []string{"", "  tabs\tnewlines\ncarriage\rquotes\"slash\\", "日本語 Καλημέρα 📅", "\x01\x1f"}
	lines := append(updateResultScriptHandlers(), `on run argv`, `return my resultJSON(argv)`, `end run`)
	args := []string{"-s", "h"}
	for _, line := range lines {
		args = append(args, "-e", line)
	}
	out, err := exec.Command("/usr/bin/osascript", append(args, fields...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil || !reflect.DeepEqual(got, fields) {
		t.Fatalf("got=%q err=%v output=%s", got, err, out)
	}
}

func TestUpdateScriptCapturesActualTargetAfterWrites(t *testing.T) {
	lines, _, err := buildUpdateEventScript("uid", 792417600, ScopeFuture, EventUpdateInput{})
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Join(lines, "\n")
	if strings.Index(script, "set representativeRef to targetRef") < strings.Index(script, "my replaceDisplayAlarm(targetRef") || strings.Index(script, "set resultFields") < strings.Index(script, `if foundAny is false`) {
		t.Fatal("snapshot does not follow writes")
	}
	for _, field := range []string{"description of representativeRef", "url of representativeRef", "start date of representativeRef", "location of representativeRef"} {
		if !strings.Contains(script, field) {
			t.Fatalf("missing %s", field)
		}
	}
}

// Compilation loads Calendar's terminology but never executes the write script.
func TestUpdateScriptCompiles(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("AppleScript compiler requires macOS")
	}
	title := "Changed"
	lines, _, err := buildUpdateEventScript("uid", 792417600, ScopeFuture, EventUpdateInput{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "update.applescript")
	if err := os.WriteFile(source, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("/usr/bin/osacompile", "-o", filepath.Join(dir, "update.scpt"), source).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s\n%s", err, out, strings.Join(lines, "\n"))
	}
}

func TestNativeDateConversionAppleScript(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("AppleScript requires macOS")
	}
	lines := append(updateResultScriptHandlers(),
		`on run argv`,
		`set outputValues to {}`,
		`repeat with stamp in argv`,
		`set nativeValue to my nativeDate(stamp as integer)`,
		`copy my unixSeconds(nativeValue) to end of outputValues`,
		`end repeat`,
		`return my resultJSON(outputValues)`,
		`end run`)
	args := []string{"-s", "h"}
	for _, line := range lines {
		args = append(args, "-e", line)
	}
	stamps := []string{"-86400", "0", "1768474800", "1784113200", "2209118400"}
	out, err := exec.Command("/usr/bin/osascript", append(append(args, "--"), stamps...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []string
	if err := json.Unmarshal(out, &got); err != nil || !reflect.DeepEqual(got, stamps) {
		t.Fatalf("got=%v want=%v err=%v output=%s", got, stamps, err, out)
	}
}

func TestNativeAmbiguousDates(t *testing.T) {
	for _, tc := range []struct {
		zone, stamp string
		ambiguous   bool
	}{
		{"Europe/Berlin", "2026-10-25T00:30:00Z", true},
		{"Europe/Berlin", "2026-10-25T01:30:00Z", true},
		{"Europe/Berlin", "2026-10-25T02:30:00Z", false},
		{"Europe/Berlin", "2026-01-15T11:00:00Z", false},
		{"Europe/Berlin", "2026-07-15T11:00:00Z", false},
		{"Australia/Lord_Howe", "2026-04-04T14:45:00Z", true},
		{"Australia/Lord_Howe", "2026-04-04T15:15:00Z", true},
		{"UTC", "2026-10-25T01:30:00Z", false},
	} {
		t.Run(tc.zone+tc.stamp, func(t *testing.T) {
			instant, err := time.Parse(time.RFC3339, tc.stamp)
			if err != nil {
				t.Fatal(err)
			}
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			if got := ambiguousNativeDate(instant, loc); got != tc.ambiguous {
				t.Fatalf("ambiguous=%v", got)
			}
			fields := nativeUpdateFields(instant)
			fields[12] = tc.zone
			raw, _ := json.Marshal(fields)
			_, err = decodeUpdateResult(string(raw), "shared-uid", instant.Unix()-cocoaEpochOffset, ScopeThis, EventUpdateInput{})
			if tc.ambiguous && err == nil {
				t.Fatal("ambiguous native date certified as snapshot")
			}
		})
	}
}
