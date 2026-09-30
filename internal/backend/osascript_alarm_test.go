package backend

import (
	"context"
	"encoding/json"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// Execute the production alarm transformation on actual in-memory EventKit
// objects. There is no store save, Calendar access, or permission request.
func TestNativeAlarmValues(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("EventKit requires macOS")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lines := append(updateResultScriptHandlers(), alarmScriptHandlers()...)
	lines = append(lines,
		`set store to current application's EKEventStore's alloc()'s init()`,
		`set e to current application's EKEvent's eventWithEventStore:store`,
		`e's setTitle:"keep title"`, `e's setNotes:"keep notes"`,
		`set emailAlarm to current application's EKAlarm's alarmWithRelativeOffset:-1200`,
		`emailAlarm's setEmailAddress:"fixture@example.com"`, `e's addAlarm:emailAlarm`,
		`e's addAlarm:(current application's EKAlarm's alarmWithRelativeOffset:-900)`,
		`e's addAlarm:(current application's EKAlarm's alarmWithAbsoluteDate:(current application's NSDate's dateWithTimeIntervalSince1970:1791014400))`,
		`set observations to {}`,
		`repeat with valueText in {"__ACAL_KEEP__", "-30", "15", "0", "__ACAL_KEEP__"}`,
		`my replaceAlarmValues(e, valueText as text)`,
		`my verifyAlarmValues(e, valueText as text)`,
		`set alarms to e's alarms()`,
		`set emailCount to 0`,
		`repeat with entry in (alarms as list)`,
		`set alarmRef to contents of entry`,
		`if (alarmRef's |type|() as integer) is 3 then`,
		`if (alarmRef's emailAddress() as text) is not "fixture@example.com" or (alarmRef's relativeOffset() as integer) is not -1200 then error "email alarm changed"`,
		`set emailCount to emailCount + 1`,
		`end if`,
		`end repeat`,
		`if emailCount is not 1 then error "non-display alarm removed"`,
		`if (emailAlarm's emailAddress() as text) is not "fixture@example.com" then error "email alarm changed"`,
		`if (e's |title|() as text) is not "keep title" or (e's notes() as text) is not "keep notes" then error "event fields changed"`,
		`set end of observations to (alarms's |count|() as integer) as text`,
		`end repeat`, `return my resultJSON(observations)`,
	)
	out, err := runWriteAppleScript(ctx, lines)
	if err != nil {
		t.Fatal(err)
	}
	var counts []string
	if err := json.Unmarshal([]byte(out), &counts); err != nil || !reflect.DeepEqual(counts, []string{"1", "2", "2", "2", "1"}) {
		t.Fatalf("counts=%v err=%v output=%s", counts, err, out)
	}
}
