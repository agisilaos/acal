package backend

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt-in: this test creates and removes its own Calendar calendar. Routine CI
// must never mutate the invoking user's Calendar or request Calendar permission.
func TestNativeReminderRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("ACAL_CALENDAR_INTEGRATION") != "1" {
		t.Skip("set ACAL_CALENDAR_INTEGRATION=1 to use a disposable Calendar calendar")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	name := fmt.Sprintf("acal-reminder-test-%d", time.Now().UnixNano())
	// Register cleanup before creation in case a native command partially succeeds.
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		_, err := runWriteAppleScript(cleanupCtx, []string{
			`on run argv`, `tell application "Calendar"`,
			`if exists (first calendar whose name is item 1 of argv) then delete (first calendar whose name is item 1 of argv)`,
			`end tell`, `end run`,
		}, name)
		if err != nil {
			t.Errorf("disposable calendar cleanup: %v", err)
		}
	})
	out, err := runWriteAppleScript(ctx, append(append(updateResultScriptHandlers(), `use framework "EventKit"`),
		`on run argv`, `tell application "Calendar"`,
		`set c to make new calendar with properties {name:item 1 of argv}`,
		`set d to (current date) + 604800`,
		`set e to make new event at end of events of c with properties {summary:"alarm regression", start date:d, end date:d + 1800, location:"keep location", description:"keep notes", url:"https://example.com/keep"}`,
		`make new display alarm at end of display alarms of e with properties {trigger interval:-15}`,
		`make new display alarm at end of display alarms of e with properties {trigger interval:-20}`,
		`set eventUID to uid of e as text`,
		`set eventStart to my unixSeconds(start date of e)`,
		`end tell`,
		`return my resultJSON({eventUID, eventStart})`, `end run`), name)
	if err != nil {
		t.Fatal(err)
	}
	var identity []string
	if err := json.Unmarshal([]byte(out), &identity); err != nil || len(identity) != 2 {
		t.Fatalf("identity=%q err=%v", out, err)
	}
	unix, err := strconv.ParseInt(identity[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("%s@%d", identity[0], unix-cocoaEpochOffset)
	be := NewOsaScriptBackend()
	for _, minutes := range []string{"clear", "-30", "15", "0", "clear"} {
		in := EventUpdateInput{Scope: ScopeThis, ClearReminder: minutes == "clear"}
		var wanted *time.Duration
		if minutes != "clear" {
			n, _ := strconv.Atoi(minutes)
			d := time.Duration(n) * time.Minute
			wanted = &d
			in.ReminderOffset = wanted
		}
		result, err := be.UpdateEvent(ctx, id, in)
		if err != nil {
			t.Fatalf("%s: %v", minutes, err)
		}
		if result.ID != id || result.Title != "alarm regression" || result.Notes != "keep notes" || result.Location != "keep location" || result.URL != "https://example.com/keep" || result.Start.Unix() != unix || result.End.Unix() != unix+1800 {
			t.Fatalf("event changed beyond its alarm: %+v", result)
		}
		got, err := be.GetReminderOffset(ctx, id)
		if err != nil || (got == nil) != (wanted == nil) || (got != nil && *got != *wanted) {
			t.Fatalf("%s: reminder=%v want=%v err=%v", minutes, got, wanted, err)
		}
		counts, err := runWriteAppleScript(ctx, []string{
			`use framework "EventKit"`, `use scripting additions`, `on run argv`,
			`tell application "Calendar"`, `set c to first calendar whose name is item 1 of argv`,
			`set eventCount to count of events of c`, `end tell`,
			`set store to current application's EKEventStore's alloc()'s init()`,
			`set e to store's calendarItemWithIdentifier:(item 2 of argv)`,
			`set displayCount to 0`,
			`repeat with entry in (e's alarms() as list)`, `set a to contents of entry`,
			`if (a's |type|() as integer) is 0 then set displayCount to displayCount + 1`,
			`end repeat`, `return {eventCount, displayCount}`, `end run`,
		}, name, identity[0])
		wantCount := "1, 1\n"
		if wanted == nil {
			wantCount = "1, 0\n"
		}
		if err != nil || counts != wantCount {
			t.Fatalf("%s: event/display counts=%q want=%q err=%v", minutes, counts, wantCount, err)
		}
	}
	// History replay can also create an event with its reminder already set.
	offset := -5 * time.Minute
	created, err := be.AddEvent(ctx, EventCreateInput{
		Calendar: name, Title: "created with reminder",
		Start: time.Unix(unix+7200, 0), End: time.Unix(unix+9000, 0),
		ReminderOffset: &offset,
	})
	if err != nil {
		t.Fatalf("create with reminder: %v", err)
	}
	got, err := be.GetReminderOffset(ctx, created.ID)
	if err != nil || got == nil || *got != offset {
		t.Fatalf("created reminder=%v want=%v err=%v", got, offset, err)
	}
}

// Recurring writes are rejected before mutation, including detached instances.
func TestNativeReminderRecurrenceRejection(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("ACAL_CALENDAR_INTEGRATION") != "1" {
		t.Skip("set ACAL_CALENDAR_INTEGRATION=1 to use a disposable Calendar calendar")
	}
	for _, scenario := range []string{"anchor", "middle", "detached"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			name := fmt.Sprintf("acal-reminder-test-%d", time.Now().UnixNano())
			t.Cleanup(func() {
				ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
				defer stop()
				_, err := runWriteAppleScript(ctx, []string{`on run argv`, `tell application "Calendar"`, `if exists (first calendar whose name is item 1 of argv) then delete (first calendar whose name is item 1 of argv)`, `end tell`, `end run`}, name)
				if err != nil {
					t.Errorf("cleanup: %v", err)
				}
			})
			out, err := runWriteAppleScript(ctx, append(updateResultScriptHandlers(),
				`on run argv`, `tell application "Calendar"`,
				`set c to make new calendar with properties {name:item 1 of argv}`,
				`set d to (current date) + 604800`,
				`set e to make new event at end of events of c with properties {summary:"recurring alarm fixture", start date:d, end date:d + 1800, recurrence:"FREQ=DAILY;COUNT=3"}`,
				`make new display alarm at end of display alarms of e with properties {trigger interval:-15}`,
				`return my resultJSON({uid of e as text, my unixSeconds(start date of e)})`, `end tell`, `end run`), name)
			if err != nil {
				t.Fatal(err)
			}
			var identity []string
			if err := json.Unmarshal([]byte(out), &identity); err != nil || len(identity) != 2 {
				t.Fatalf("identity=%s err=%v", out, err)
			}
			unix, err := strconv.ParseInt(identity[1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}

			targetStart := unix
			if scenario != "anchor" {
				targetStart += 86400
			}
			targetUID := identity[0]
			if scenario == "detached" {
				targetUID, err = runWriteAppleScript(ctx, []string{
					`use framework "EventKit"`, `use scripting additions`, `on run argv`,
					`set store to current application's EKEventStore's alloc()'s init()`,
					`set anchorEvent to store's calendarItemWithIdentifier:(item 1 of argv)`,
					`set targetSeconds to item 2 of argv as real`,
					`set fromDate to current application's NSDate's dateWithTimeIntervalSince1970:(targetSeconds - 1)`,
					`set toDate to current application's NSDate's dateWithTimeIntervalSince1970:(targetSeconds + 1)`,
					`set predicate to store's predicateForEventsWithStartDate:fromDate endDate:toDate calendars:{anchorEvent's |calendar|()}`,
					`set occurrences to store's eventsMatchingPredicate:predicate`,
					`if (occurrences's |count|() as integer) is not 1 then error "unexpected fixture occurrences"`,
					`set e to occurrences's objectAtIndex:0`, `e's setTitle:"detached fixture"`,
					`set {saved, saveError} to store's saveEvent:e span:0 |error|:(reference)`,
					`if not saved then error "fixture detach failed"`,
					`return e's calendarItemIdentifier() as text`, `end run`,
				}, identity[0], strconv.FormatInt(targetStart, 10))
				if err != nil {
					t.Fatal(err)
				}
				targetUID = strings.TrimSpace(targetUID)
			}
			id := fmt.Sprintf("%s@%d", targetUID, targetStart-cocoaEpochOffset)
			be := NewOsaScriptBackend()
			checkRejected := func(err error) {
				t.Helper()
				var rejected *WriteRejectedError
				if !errors.As(err, &rejected) || rejected.Reason != "recurring" {
					t.Fatalf("expected recurring rejection, got %v", err)
				}
			}
			checkRejected(be.CheckEventWrite(ctx, id))
			title := "must not be saved"
			for _, scope := range []RecurrenceScope{ScopeThis, ScopeFuture, ScopeSeries} {
				_, err = be.UpdateEvent(ctx, id, EventUpdateInput{Scope: scope, Title: &title, ClearReminder: true})
				checkRejected(err)
				checkRejected(be.DeleteEvent(ctx, id, scope))
			}

			out, err = runWriteAppleScript(ctx, append(append(updateResultScriptHandlers(), `use framework "EventKit"`),
				`on run argv`, `set store to current application's EKEventStore's alloc()'s init()`,
				`set calendars to {}`, `repeat with c in (store's calendarsForEntityType:0)`, `if (c's |title|() as text) is item 1 of argv then set end of calendars to contents of c`, `end repeat`,
				`set startValue to item 2 of argv as real`,
				`set fromDate to current application's NSDate's dateWithTimeIntervalSince1970:(startValue - 1)`,
				`set toDate to current application's NSDate's dateWithTimeIntervalSince1970:(startValue + 259200)`,
				`set predicate to store's predicateForEventsWithStartDate:fromDate endDate:toDate calendars:calendars`,
				`set rows to {}`, `repeat with entry in (store's eventsMatchingPredicate:predicate)`, `set e to contents of entry`,
				`set d to e's startDate()`, `set startValue to my unixSeconds(d)`,
				`set alarmCount to 0`, `set alarms to e's alarms()`, `if alarms is not missing value then set alarmCount to alarms's |count|() as integer`,
				`if (e's |title|() as text) is "must not be saved" then error "rejected update mutated title"`, `set end of rows to {(startValue as text), (alarmCount as text)}`, `end repeat`, `return my resultJSON(rows)`, `end run`), name, identity[1])
			if err != nil {
				t.Fatal(err)
			}
			var rows [][]string
			if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 3 {
				t.Fatalf("occurrences=%s err=%v", out, err)
			}
			for _, row := range rows {
				want := "1"
				if row[1] != want {
					t.Fatalf("%s: occurrence alarms=%v want=%s", scenario, row, want)
				}
			}
		})
	}
}
